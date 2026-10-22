package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
	"github.com/booondenz/kada/internal/infra/urlcheck"
)

// dispatchQuota remembers how many visitors each target took today.
//
// It is in memory because the number is a routing decision - "spread people across these groups, and move on
// when one is full" - not a bill. One API instance per deployment makes it exact; several would make it
// approximately right, bounded by the day's quota per instance, and the failure is over-serving a target
// rather than losing a visitor. The alternative, a count over click_logs on every redirect, is a group-by on
// the hottest path in the system for a number that only needs to be close.
type dispatchQuota struct {
	mu   sync.Mutex
	day  int
	used map[int64]int
}

// today is a comparable date, in UTC so the window changes at one moment for every caller.
func (q *dispatchQuota) today(now time.Time) int {
	return now.Year()*10000 + int(now.Month())*100 + now.Day()
}

// available reports whether a target with this quota has room left today. A quota of zero means unlimited,
// which is what every target without one carries.
func (q *dispatchQuota) available(id int64, quota int, now time.Time) bool {
	if quota <= 0 {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.day != q.today(now) {
		return true
	}
	return q.used[id] < quota
}

// consume records that one visitor was sent to the target.
func (q *dispatchQuota) consume(id int64, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()

	day := q.today(now)
	if q.day != day {
		q.day = day
		q.used = make(map[int64]int)
	}
	q.used[id]++
}

// weightedPick chooses one candidate by weight. `roll` is in [0, sum of weights).
//
// Split out from the random draw so the distribution is testable without controlling the random source: a
// target weighted 3 against 1 owns rolls 0, 1 and 2, and that is a fact about the arithmetic rather than
// about luck.
func weightedPick(candidates []domain.LinkTargetInfo, roll int) *domain.LinkTargetInfo {
	for i := range candidates {
		if roll < candidates[i].Weight {
			return &candidates[i]
		}
		roll -= candidates[i].Weight
	}
	return nil
}

// PickTarget chooses the destination for one visitor, or nil when the link has no pool or none of it can
// take them right now.
//
// The caller must treat nil-due-to-exhaustion as "do not serve", not as "serve the original URL": a link
// whose targets are all full or all disabled has said where its traffic should go, and falling back would
// send it somewhere the owner did not choose.
func (s *LinkService) PickTarget(link *domain.LinkInfo) *domain.LinkTargetInfo {
	if link == nil || len(link.Targets) == 0 {
		return nil
	}

	now := time.Now().UTC()
	candidates := make([]domain.LinkTargetInfo, 0, len(link.Targets))
	total := 0
	for _, t := range link.Targets {
		// A weight of zero is "never", which is how a target is kept in the pool without receiving traffic.
		if !t.Enabled || t.Weight <= 0 {
			continue
		}
		// An expired target is skipped rather than deleted: the row is still the record of what that code
		// was, and the owner replaces it by editing the row.
		if t.ExpiresAt != nil && t.ExpiresAt.Before(now) {
			continue
		}
		if !s.quota.available(t.ID, t.DailyQuota, now) {
			continue
		}
		candidates = append(candidates, t)
		total += t.Weight
	}
	if len(candidates) == 0 {
		return nil
	}

	// A routing draw, not a secret: an adversary who could predict it would learn which of the owner's own
	// destinations comes next, and the hot path cannot afford a syscall per request.
	chosen := weightedPick(candidates, rand.IntN(total)) //nolint:gosec // load balancing, not cryptography
	if chosen == nil {
		return nil
	}
	// Only a target that caps its day is worth counting: the counter's map would otherwise grow with every
	// target that was ever served, to record a number nobody reads.
	if chosen.DailyQuota > 0 {
		s.quota.consume(chosen.ID, now)
	}
	return chosen
}

// ReplaceTargets replaces a link's pool and records the change.
//
// The audit row is written in the same transaction as the replacement, because a change that happened and
// was not recorded is precisely the thing the record exists to prevent. The cache is dropped afterwards: the
// pool travels with the link, so a replacement that did not invalidate would keep serving the old split
// until the TTL ran out.
func (s *LinkService) ReplaceTargets(ctx context.Context, linkID, userID int64, operator string, inputs []domain.LinkTargetInput) (*domain.LinkInfo, error) {
	var link entity.Link
	if err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", linkID, userID).First(&link).Error; err != nil {
		return nil, domain.ErrLinkNotFound
	}

	targets := make([]entity.LinkTarget, 0, len(inputs))
	for _, in := range inputs {
		url := in.URL
		if !urlcheck.IsSafeTarget(url) {
			return nil, errors.New("target URL must use http or https and must not point into a private network")
		}
		if category, blocked := s.Blocked(url); blocked {
			return nil, &domain.BlockedTargetError{Category: category}
		}

		weight := in.Weight
		if weight < 0 {
			weight = 0
		}
		quota := in.DailyQuota
		if quota < 0 {
			quota = 0
		}
		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}

		kind := domain.TargetKind(in.Kind)
		if in.Kind == "" {
			kind = domain.TargetURL
		}
		if !kind.Known() {
			return nil, errors.New("unknown target kind")
		}

		var expiresAt *time.Time
		if in.ExpiresAt != nil && *in.ExpiresAt != "" {
			parsed, parseErr := time.Parse(time.RFC3339, *in.ExpiresAt)
			if parseErr != nil {
				return nil, errors.New("invalid expiry format, use RFC 3339 (for example 2025-01-01T00:00:00Z)")
			}
			expiresAt = &parsed
		}

		targets = append(targets, entity.LinkTarget{
			LinkID:     link.ID,
			URL:        url,
			Label:      in.Label,
			Weight:     weight,
			DailyQuota: quota,
			Enabled:    enabled,
			Kind:       string(kind),
			ExpiresAt:  expiresAt,
		})
	}

	before, err := jsonTargets(ctx, s.db, link.ID)
	if err != nil {
		return nil, errors.New("failed to read the current targets")
	}

	now := time.Now().UTC()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if deleteErr := tx.Where("link_id = ?", link.ID).Delete(&entity.LinkTarget{}).Error; deleteErr != nil {
			return deleteErr
		}
		for i := range targets {
			targets[i].CreatedAt = now
			targets[i].UpdatedAt = now
			if createErr := tx.Create(&targets[i]).Error; createErr != nil {
				return createErr
			}
		}
		after := targetsToJSON(targets)
		return tx.Create(&entity.LinkTargetAudit{
			LinkID:    link.ID,
			Operator:  operator,
			Before:    before,
			After:     after,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, errors.New("failed to save the targets")
	}

	if s.cache != nil {
		s.cache.InvalidateLink(ctx, link.ShortCode)
	}

	return s.GetByID(ctx, link.ID, userID)
}

// Targets returns a link's pool with the clicks each target took, newest first.
//
// The count is a group-by over click_logs, which is why it is a request of its own rather than part of the
// link: the redirect path carries the pool and must not carry a statistic.
func (s *LinkService) Targets(ctx context.Context, linkID, userID int64) ([]domain.LinkTargetInfo, error) {
	var link struct{ ID int64 }
	if err := s.db.WithContext(ctx).Model(&entity.Link{}).
		Select("id").Where("id = ? AND user_id = ?", linkID, userID).Scan(&link).Error; err != nil || link.ID == 0 {
		return nil, domain.ErrLinkNotFound
	}

	targets := s.targetsOf(ctx, linkID)

	clicks := make(map[int64]int64, len(targets))
	var rows []struct {
		TargetID int64
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&entity.ClickLog{}).
		Select("target_id, count(*) AS count").
		Where("link_id = ? AND target_id IS NOT NULL AND kind = ?", linkID, string(domain.ClickVisit)).
		Group("target_id").Scan(&rows).Error; err == nil {
		for _, r := range rows {
			clicks[r.TargetID] = r.Count
		}
	}

	for i := range targets {
		targets[i].Clicks = clicks[targets[i].ID]
	}
	return targets, nil
}

// targetsOf loads a link's pool, in the order the owner built it.
func (s *LinkService) targetsOf(ctx context.Context, linkID int64) []domain.LinkTargetInfo {
	var rows []entity.LinkTarget
	if err := s.db.WithContext(ctx).Where("link_id = ?", linkID).
		Order("id ASC").Find(&rows).Error; err != nil {
		log.Printf("load targets of link %d failed: %v", linkID, err)
		return nil
	}
	return targetInfos(rows)
}

func targetInfos(rows []entity.LinkTarget) []domain.LinkTargetInfo {
	out := make([]domain.LinkTargetInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.LinkTargetInfo{
			ID:         r.ID,
			URL:        r.URL,
			Label:      r.Label,
			Weight:     r.Weight,
			DailyQuota: r.DailyQuota,
			Enabled:    r.Enabled,
			Kind:       domain.TargetKind(r.Kind),
			ExpiresAt:  r.ExpiresAt,
		})
	}
	return out
}

// attachTargets fills in the pool on a link the caller is about to return or cache.
func (s *LinkService) attachTargets(ctx context.Context, info *domain.LinkInfo) *domain.LinkInfo {
	if info == nil {
		return nil
	}
	info.Targets = s.targetsOf(ctx, info.ID)
	return info
}

// jsonTargets is the "before" half of an audit row.
func jsonTargets(ctx context.Context, db *gorm.DB, linkID int64) (string, error) {
	var rows []entity.LinkTarget
	if err := db.WithContext(ctx).Where("link_id = ?", linkID).Order("id ASC").Find(&rows).Error; err != nil {
		return "", err
	}
	return targetsToJSON(rows), nil
}

// targetsToJSON renders a pool for the audit. It is a value, not a pointer: a JSON null would say "no
// change", which is the one thing this row must never say.
func targetsToJSON(rows []entity.LinkTarget) string {
	infos := targetInfos(rows)
	if infos == nil {
		infos = []domain.LinkTargetInfo{}
	}
	out, err := json.Marshal(infos)
	if err != nil {
		return fmt.Sprintf("[unserialisable targets: %v]", err)
	}
	return string(out)
}
