package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
	"github.com/booondenz/kada/internal/infra/urlcheck"
	"github.com/booondenz/kada/internal/metrics"
	"github.com/booondenz/kada/internal/mq"
)

// shortCodePattern allows only letters, digits, underscores and hyphens in a short code
var shortCodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,20}$`)

// TargetBlocker reports whether a target URL is in a blocked category, and which one.
//
// It is an interface so the service does not know whether the list came from a table, a file or a test, and
// nil so a service without one blocks nothing. The same instance answers the create path, the update path
// and the redirect path, which is the point: a link already created is refused by the redirect that would
// have served it, not only by the write that created it.
type TargetBlocker interface {
	Blocked(targetURL string) (category string, blocked bool)
}

type LinkService struct {
	db          *gorm.DB
	baseURL     string
	cache       LinkCache
	kafka       mq.ClickPublisher // Kafka publisher; nil means disabled
	clickWriter ClickWriter       // direct write (used by the degraded fallback)
	blocker     TargetBlocker     // nil means nothing is blocked

	// loads collapses concurrent cache misses for one short code into a single database read. A key that
	// is absent is exactly when a burst of requests for it arrives together.
	loads singleflight.Group
}

func NewLinkService(db *gorm.DB, baseURL string, cache LinkCache, kafka mq.ClickPublisher, clickWriter ClickWriter, blocker TargetBlocker) *LinkService {
	return &LinkService{db: db, baseURL: baseURL, cache: cache, kafka: kafka, clickWriter: clickWriter, blocker: blocker}
}

// Blocked reports whether the target is in a blocked category, through the list the create and update paths
// consult. The redirect handler calls it per request rather than trusting a decision cached with the link,
// because the list changes after the link was cached.
func (s *LinkService) Blocked(targetURL string) (string, bool) {
	if s.blocker == nil {
		return "", false
	}
	return s.blocker.Blocked(targetURL)
}

// Create creates a short link
func (s *LinkService) Create(ctx context.Context, userID int64, req domain.CreateLinkRequest) (*domain.LinkInfo, error) {
	if !urlcheck.IsSafeTarget(req.OriginalURL) {
		return nil, errors.New("target URL must use http or https")
	}

	// The list is consulted before anything is written or allocated, so a refused target leaves no short
	// code behind.
	if category, blocked := s.Blocked(req.OriginalURL); blocked {
		return nil, &domain.BlockedTargetError{Category: category}
	}

	// validate folder ownership (previously any ID could be attached, leaking names across users)
	if err := s.validateOwnedFolder(ctx, userID, req.FolderID); err != nil {
		return nil, err
	}

	var shortCode string
	customCode := req.ShortCode != nil && *req.ShortCode != ""

	if customCode {
		if !shortCodePattern.MatchString(*req.ShortCode) {
			return nil, errors.New("invalid short code format: only letters, digits, underscores and hyphens are allowed, length 4-20")
		}
		// fast-path check: on failure make no decision, the unique constraint is the final arbiter
		if s.shortCodeTaken(ctx, *req.ShortCode, 0) {
			return nil, errors.New("that short code is already taken, please choose another")
		}
		shortCode = *req.ShortCode
	} else {
		shortCode = generateShortCode()
	}

	domainName := "kada.click"
	if req.Domain != nil && *req.Domain != "" {
		domainName = *req.Domain
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return nil, errors.New("invalid expiry format, use RFC 3339 (for example 2025-01-01T00:00:00Z)")
		}
		expiresAt = &t
	}

	var passwordHash *string
	if req.Password != nil && *req.Password != "" {
		hash := hashPassword(*req.Password)
		passwordHash = &hash
	}

	var created entity.Link
	// the unique constraint is the final arbiter for short-code conflicts: under a check-then-insert race the INSERT reports 23505,
	// a custom short code returns a friendly error while a random short code is regenerated and retried
	for attempt := 0; ; attempt++ {
		row := entity.Link{
			ShortCode:    shortCode,
			OriginalURL:  req.OriginalURL,
			Title:        req.Title,
			Description:  req.Description,
			ImageURL:     req.ImageURL,
			Domain:       domainName,
			PasswordHash: passwordHash,
			ExpiresAt:    expiresAt,
			UserID:       &userID,
			FolderID:     req.FolderID,
			UTMSource:    req.UTMSource,
			UTMMedium:    req.UTMMedium,
			UTMCampaign:  req.UTMCampaign,
			UTMTerm:      req.UTMTerm,
			UTMContent:   req.UTMContent,
			IosURL:       req.IosURL,
			AndroidURL:   req.AndroidURL,
		}
		err := s.db.WithContext(ctx).Create(&row).Error
		if err == nil {
			created = row
			break
		}
		if isDuplicateKey(err) {
			if customCode {
				return nil, errors.New("that short code is already taken, please choose another")
			}
			if attempt >= 4 {
				log.Printf("create link failed: %v", err)
				return nil, errors.New("failed to generate short code, please try again")
			}
			shortCode = generateShortCode()
			continue
		}
		log.Printf("create link failed: %v", err)
		return nil, errors.New("failed to create short link")
	}

	// attach tags (only own tags may be attached, preventing tag name/color leaks across users)
	s.attachOwnedTags(ctx, userID, created.ID, req.TagIDs)

	info := s.toLinkInfo(ctx, created.ID, userID)
	if info == nil {
		// Should not happen: the row was just created. Fall back to the entity we already have instead of
		// returning a half-populated response.
		info = &domain.LinkInfo{
			ID:          created.ID,
			ShortCode:   created.ShortCode,
			OriginalURL: created.OriginalURL,
			Domain:      created.Domain,
		}
	}

	s.cacheLink(ctx, info)

	return info, nil
}

// GetByID gets a link by ID (with full info such as folder, tags and UTM)
func (s *LinkService) GetByID(ctx context.Context, linkID, userID int64) (*domain.LinkInfo, error) {
	info := s.toLinkInfo(ctx, linkID, userID)
	if info == nil {
		return nil, domain.ErrLinkNotFound
	}
	return info, nil
}

// GetByCode gets a link by short code (reads from cache first).
//
// Every way out of the cache is counted, because the hit ratio is the only evidence that the cache is
// earning its place. Both hit and negative_hit mean the request was answered without PostgreSQL, which is
// the number worth reporting; a miss is a lookup that reached the database.
func (s *LinkService) GetByCode(ctx context.Context, shortCode string) (*domain.LinkInfo, error) {
	// try to read from cache
	if s.cache != nil {
		if info, ok := s.cache.GetLink(ctx, shortCode); ok {
			// Counted before the expiry check: the request was answered from the cache either way, and the
			// question this counter answers is whether PostgreSQL was needed.
			metrics.CacheLookup(metrics.CacheHit)

			// check whether it has expired
			if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
				s.cache.InvalidateLink(ctx, shortCode)
				return nil, errors.New("link has expired")
			}
			return info, nil
		}

		// A code that was looked up recently and did not exist is answered from the cache as well.
		// Without this every request for an unknown code - which is what a scanner sends, and what a
		// link someone deleted leaves behind - is a database query.
		if s.cache.IsMissing(ctx, shortCode) {
			metrics.CacheLookup(metrics.CacheNegativeHit)
			return nil, domain.ErrLinkNotFound
		}

		metrics.CacheLookup(metrics.CacheMiss)
	}

	return s.loadLink(ctx, shortCode)
}

// loadLink reads the link from the database and fills the cache.
//
// Concurrent calls for one code share a single read: that is the moment a burst of requests for the
// same absent key arrives together, and without it they would all reach Postgres at once.
//
// The shared read runs under the context of whichever caller arrived first, so a client that
// disconnects mid-read can end it for the waiters too. They see an error once and the next request
// repeats the load.
func (s *LinkService) loadLink(ctx context.Context, shortCode string) (*domain.LinkInfo, error) {
	loaded, err, _ := s.loads.Do(shortCode, func() (any, error) {
		var row entity.Link
		err := s.db.WithContext(ctx).
			Where("short_code = ? AND is_active = TRUE", shortCode).
			First(&row).Error
		if err != nil {
			// Only a row that is genuinely absent is remembered as absent. An unreachable database would
			// otherwise write "this code does not exist" for every code that was asked for, and that
			// verdict would outlive the outage by its whole TTL.
			if errors.Is(err, gorm.ErrRecordNotFound) {
				s.markMissing(ctx, shortCode)
			}
			return nil, domain.ErrLinkNotFound
		}

		info := linkInfoFromEntity(row)

		// check whether it has expired
		if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
			return nil, errors.New("link has expired")
		}

		s.cacheLink(ctx, info)
		return info, nil
	})
	if err != nil {
		return nil, err
	}

	info, ok := loaded.(*domain.LinkInfo)
	if !ok {
		return nil, domain.ErrLinkNotFound
	}
	return info, nil
}

// cacheLink fills the cache for a link and drops the record that its code was missing. The two move
// together: a "missing" verdict that outlived the link it denied would shadow that link until its TTL.
func (s *LinkService) cacheLink(ctx context.Context, info *domain.LinkInfo) {
	if s.cache == nil {
		return
	}
	s.cache.SetLink(ctx, info)
	s.cache.ClearMissing(ctx, info.ShortCode)
}

// markMissing records that a short code did not resolve to a link.
//
// Counted here rather than at the call site: this is the write that turns a stream of requests for an
// absent code into requests the database never sees, so the counter belongs to the act itself.
func (s *LinkService) markMissing(ctx context.Context, shortCode string) {
	if s.cache == nil {
		return
	}
	s.cache.MarkMissing(ctx, shortCode)
	metrics.CacheLookup(metrics.CacheNegativeStored)
}

// CheckPassword checks the link password
func (s *LinkService) CheckPassword(ctx context.Context, shortCode, password string) (bool, *domain.LinkInfo, error) {
	var row entity.Link
	err := s.db.WithContext(ctx).
		Where("short_code = ? AND is_active = TRUE", shortCode).
		First(&row).Error
	if err != nil {
		return false, nil, domain.ErrLinkNotFound
	}

	info := linkInfoFromEntity(row)

	if info.ExpiresAt != nil && info.ExpiresAt.Before(time.Now()) {
		return false, nil, errors.New("link has expired")
	}

	if row.PasswordHash == nil || *row.PasswordHash == "" {
		return true, info, nil // no password
	}

	if password == "" || !checkPasswordHash(password, *row.PasswordHash) {
		return false, info, nil
	}

	return true, info, nil
}

// List gets the user's link list
func (s *LinkService) List(ctx context.Context, userID int64, page, pageSize int, search string, folderID, tagID int64, sort string) (*domain.PaginatedLinks, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// The filters are shared by the COUNT and the page query, so build them once. GORM keeps the SQL
	// injection surface at zero because every value is passed as a bound parameter.
	filters := []string{"l.user_id = ?"}
	args := []any{userID}

	if search != "" {
		filters = append(filters, "(l.title ILIKE ? OR l.original_url ILIKE ? OR l.short_code ILIKE ?)")
		like := "%" + search + "%"
		args = append(args, like, like, like)
	}
	if folderID > 0 {
		filters = append(filters, "l.folder_id = ?")
		args = append(args, folderID)
	}
	if tagID > 0 {
		filters = append(filters, "l.id IN (SELECT link_id FROM link_tags WHERE tag_id = ?)")
		args = append(args, tagID)
	}
	where := strings.Join(filters, " AND ")

	var total int64
	if err := s.linkListQuery(ctx, where, args).Count(&total).Error; err != nil {
		log.Printf("count links failed: %v", err)
		total = 0
	}

	orderBy := "l.created_at DESC"
	switch sort {
	case "clicks_desc":
		orderBy = "l.click_count DESC"
	case "clicks_asc":
		orderBy = "l.click_count ASC"
	case "created_asc":
		orderBy = "l.created_at ASC"
	}

	var rows []entity.Link
	err := s.linkListQuery(ctx, where, args).
		Select("l.*").
		Order(orderBy).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&rows).Error
	if err != nil {
		return nil, errors.New("failed to list links")
	}

	links := make([]domain.LinkInfo, 0, len(rows))
	for _, row := range rows {
		links = append(links, *linkInfoFromEntity(row))
	}

	return &domain.PaginatedLinks{
		Links: links, TotalCount: total, Page: page, PageSize: pageSize,
	}, nil
}

// linkListQuery is the one place the list's FROM and its WHERE are assembled.
//
// The count and the page have to be the same query up to projection and ordering, because the filters are
// written against the `l` alias. A count built on `Model(&entity.Link{})` renders `FROM "links"` with no
// alias, and PostgreSQL answers 42P01 - "missing FROM-clause entry for table l" - which the caller logs and
// swallows into a total of 0. That is what it used to do: every account, every filter combination, a
// `total_count` of zero.
func (s *LinkService) linkListQuery(ctx context.Context, where string, args []any) *gorm.DB {
	return s.db.WithContext(ctx).Table("links AS l").Where(where, args...)
}

// Update updates a link
func (s *LinkService) Update(ctx context.Context, linkID, userID int64, req domain.UpdateLinkRequest) (*domain.LinkInfo, error) {
	if req.OriginalURL != nil && *req.OriginalURL != "" && !urlcheck.IsSafeTarget(*req.OriginalURL) {
		return nil, errors.New("target URL must use http or https")
	}

	// A link that already exists can be pointed somewhere new, so the edit is checked exactly like the
	// creation - otherwise the list only stops what was written after it was written.
	if req.OriginalURL != nil && *req.OriginalURL != "" {
		if category, blocked := s.Blocked(*req.OriginalURL); blocked {
			return nil, &domain.BlockedTargetError{Category: category}
		}
	}

	// validate folder ownership
	if err := s.validateOwnedFolder(ctx, userID, req.FolderID); err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return nil, errors.New("invalid expiry format, use RFC 3339 (for example 2025-01-01T00:00:00Z)")
		}
		expiresAt = &t
	}

	// get the old short code (used for cache invalidation); failure does not affect the main flow
	var oldShortCode string
	{
		var existing entity.Link
		if err := s.db.WithContext(ctx).Select("short_code").Where("id = ?", linkID).First(&existing).Error; err == nil {
			oldShortCode = existing.ShortCode
		}
	}

	// validate the custom short code
	if req.ShortCode != nil && *req.ShortCode != "" {
		if !shortCodePattern.MatchString(*req.ShortCode) {
			return nil, errors.New("invalid short code format: only letters, digits, underscores and hyphens are allowed, length 4-20")
		}
		// fast-path check: on failure make no decision, the unique constraint is the final arbiter
		if s.shortCodeTaken(ctx, *req.ShortCode, linkID) {
			return nil, errors.New("that short code is already taken, please choose another")
		}
	}

	// COALESCE semantics: a nil field means "leave the column alone" — a nil pointer passed to Updates()
	// would otherwise be dropped, and a non-nil pointer to a zero value (is_active=false, folder_id=0)
	// would be dropped too, which is exactly the difference this map preserves.
	updates := map[string]any{"updated_at": gorm.Expr("NOW()")}
	updateIfSet := func(column string, value any) {
		if value != nil {
			updates[column] = value
		}
	}
	updateIfSet("original_url", req.OriginalURL)
	updateIfSet("short_code", req.ShortCode)
	updateIfSet("title", req.Title)
	updateIfSet("description", req.Description)
	updateIfSet("image_url", req.ImageURL)
	updateIfSet("domain", req.Domain)
	updateIfSet("expires_at", expiresAt)
	updateIfSet("is_active", req.IsActive)
	updateIfSet("folder_id", req.FolderID)
	updateIfSet("utm_source", req.UTMSource)
	updateIfSet("utm_medium", req.UTMMedium)
	updateIfSet("utm_campaign", req.UTMCampaign)
	updateIfSet("utm_term", req.UTMTerm)
	updateIfSet("utm_content", req.UTMContent)
	updateIfSet("ios_url", req.IosURL)
	updateIfSet("android_url", req.AndroidURL)

	// req.Password is a pointer-to-pointer: present-and-empty means "clear the password", absent means
	// "leave it". The original SQL used COALESCE($8, password_hash) with a hash that is empty for "".
	if req.Password != nil {
		updates["password_hash"] = hashPassword(*req.Password)
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&entity.Link{}).
			Where("id = ? AND user_id = ?", linkID, userID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		// update tag associations
		if req.TagIDs != nil {
			if err := tx.Where("link_id = ?", linkID).Delete(&entity.LinkTag{}).Error; err != nil {
				return err
			}
			return attachOwnedTagsTx(tx, userID, linkID, req.TagIDs)
		}
		return nil
	})
	if err != nil {
		if isDuplicateKey(err) {
			return nil, errors.New("that short code is already taken, please choose another")
		}
		return nil, errors.New("failed to update link, link not found or access denied")
	}

	// invalidate the cache
	if s.cache != nil {
		if oldShortCode != "" {
			s.cache.InvalidateLink(ctx, oldShortCode)
		}
		if s.newShortCode(ctx, linkID) != oldShortCode {
			s.cache.InvalidateLink(ctx, s.newShortCode(ctx, linkID))
		}
	}

	info := s.toLinkInfo(ctx, linkID, userID)
	if info == nil {
		return nil, domain.ErrLinkNotFound
	}
	return info, nil
}

// Delete deletes a link
func (s *LinkService) Delete(ctx context.Context, linkID, userID int64) error {
	// get the short code for cache invalidation; failure does not affect the main flow
	var shortCode string
	{
		var existing entity.Link
		if err := s.db.WithContext(ctx).Select("short_code").Where("id = ?", linkID).First(&existing).Error; err == nil {
			shortCode = existing.ShortCode
		}
	}

	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", linkID, userID).
		Delete(&entity.Link{}).Error; err != nil {
		return errors.New("failed to delete link")
	}

	if s.cache != nil && shortCode != "" {
		s.cache.InvalidateLink(ctx, shortCode)
	}
	return nil
}

// BatchDelete deletes links in bulk
func (s *LinkService) BatchDelete(ctx context.Context, ids []int64, userID int64) (int64, error) {
	// first fetch the short codes of the links to be deleted, to invalidate the cache afterwards (aligned with single Delete)
	var codes []string
	if err := s.db.WithContext(ctx).Model(&entity.Link{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Pluck("short_code", &codes).Error; err != nil {
		log.Printf("batch delete: fetching short codes failed: %v", err)
	}

	res := s.db.WithContext(ctx).
		Where("id IN ? AND user_id = ?", ids, userID).
		Delete(&entity.Link{})
	if res.Error != nil {
		return 0, errors.New("bulk delete failed")
	}

	// invalidate the cache of deleted short codes: otherwise a deleted link can still be redirected within the cache TTL
	if s.cache != nil {
		for _, c := range codes {
			s.cache.InvalidateLink(ctx, c)
		}
	}
	return res.RowsAffected, nil
}

// BatchTag tags links in bulk
func (s *LinkService) BatchTag(ctx context.Context, ids []int64, tagID int64, userID int64) error {
	// the tag must belong to the current user
	var tagCount int64
	if err := s.db.WithContext(ctx).Model(&entity.Tag{}).
		Where("id = ? AND user_id = ?", tagID, userID).
		Count(&tagCount).Error; err != nil || tagCount == 0 {
		return errors.New("tag not found or access denied")
	}

	// One statement instead of a select+insert per link: the INSERT ... SELECT can only see rows whose
	// user_id matches, which is exactly the ownership filter the loop implemented.
	err := s.db.WithContext(ctx).Exec(`
		INSERT INTO link_tags (link_id, tag_id)
		SELECT id, ? FROM links WHERE id IN ? AND user_id = ?
		ON CONFLICT DO NOTHING
	`, tagID, ids, userID).Error
	if err != nil {
		log.Printf("batch tag links with tag %d failed: %v", tagID, err)
		return errors.New("failed to apply tag")
	}
	return nil
}

// Block stops a link from being served, and records the decision.
//
// The column and the history row are written in one transaction, and the cached copy is dropped after it
// commits. That second step is not an optimisation: the redirect reads the flag from that cache, so a block
// that did not invalidate would keep serving the link for the rest of the TTL - which is the one thing a
// takedown may not do.
func (s *LinkService) Block(ctx context.Context, shortCode, operator, reason, evidence string) (*domain.LinkInfo, error) {
	var row entity.Link
	if err := s.db.WithContext(ctx).Where("short_code = ?", shortCode).First(&row).Error; err != nil {
		return nil, domain.ErrLinkNotFound
	}
	if row.BlockedAt != nil {
		return nil, domain.ErrLinkAlreadyBlocked
	}

	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.Link{}).Where("id = ?", row.ID).
			Updates(map[string]any{"blocked_at": now, "blocked_reason": reason, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Create(&entity.LinkBlock{
			LinkID:    row.ID,
			ShortCode: row.ShortCode,
			Reason:    reason,
			Evidence:  evidence,
			Operator:  operator,
			CreatedAt: now,
		}).Error
	})
	if err != nil {
		return nil, errors.New("failed to stop the link")
	}

	if s.cache != nil {
		s.cache.InvalidateLink(ctx, row.ShortCode)
	}

	row.BlockedAt = &now
	row.BlockedReason = &reason
	return linkInfoFromEntity(row), nil
}

// Unblock restores a link that was stopped, and closes the open history row.
func (s *LinkService) Unblock(ctx context.Context, shortCode, operator, reason string) error {
	var row entity.Link
	if err := s.db.WithContext(ctx).Where("short_code = ?", shortCode).First(&row).Error; err != nil {
		return domain.ErrLinkNotFound
	}
	if row.BlockedAt == nil {
		return domain.ErrLinkNotBlocked
	}

	now := time.Now().UTC()
	var liftedBy *string
	if operator != "" {
		liftedBy = &operator
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.Link{}).Where("id = ?", row.ID).
			Updates(map[string]any{"blocked_at": nil, "blocked_reason": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		// The open row is the one that was never lifted; only it can be. Block refuses to write a second
		// while the first is open, so there is at most one.
		return tx.Model(&entity.LinkBlock{}).
			Where("link_id = ? AND lifted_at IS NULL", row.ID).
			Updates(map[string]any{"lifted_at": now, "lift_reason": reason, "lifted_by": liftedBy}).Error
	})
	if err != nil {
		return errors.New("failed to restore the link")
	}

	if s.cache != nil {
		s.cache.InvalidateLink(ctx, row.ShortCode)
	}
	return nil
}

// Blocks returns a link's takedown history, newest first.
func (s *LinkService) Blocks(ctx context.Context, shortCode string) ([]domain.LinkBlockInfo, error) {
	var rows []entity.LinkBlock
	if err := s.db.WithContext(ctx).Where("short_code = ?", shortCode).
		Order("created_at DESC").Limit(50).Find(&rows).Error; err != nil {
		return nil, errors.New("failed to read the takedown history")
	}

	out := make([]domain.LinkBlockInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.LinkBlockInfo{
			ID:         r.ID,
			ShortCode:  r.ShortCode,
			Reason:     r.Reason,
			Evidence:   r.Evidence,
			Operator:   r.Operator,
			CreatedAt:  r.CreatedAt,
			LiftedAt:   r.LiftedAt,
			LiftReason: r.LiftReason,
			LiftedBy:   r.LiftedBy,
		})
	}
	return out, nil
}

// validateOwnedFolder validates that the folder belongs to the current user (nil or 0 means unset)
func (s *LinkService) validateOwnedFolder(ctx context.Context, userID int64, folderID *int64) error {
	if folderID != nil && *folderID != 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&entity.Folder{}).
			Where("id = ? AND user_id = ?", *folderID, userID).
			Count(&count).Error; err != nil || count == 0 {
			return errors.New("folder not found or access denied")
		}
	}
	return nil
}

// ExportCSV exports the user's links as a CSV string
func (s *LinkService) ExportCSV(ctx context.Context, userID int64) (string, error) {
	type exportRow struct {
		ShortCode   string
		OriginalURL string
		Title       string
		Domain      string
		ClickCount  int64
		IsActive    bool
		CreatedAt   time.Time
	}

	var rows []exportRow
	err := s.db.WithContext(ctx).
		Model(&entity.Link{}).
		Select("short_code, original_url, COALESCE(title, '') AS title, domain, click_count, is_active, created_at").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Scan(&rows).Error
	if err != nil {
		return "", errors.New("failed to query link data")
	}

	var sb strings.Builder
	sb.WriteString("code,target_url,title,domain,clicks,status,created_at\n")
	for _, r := range rows {
		status := "enabled"
		if !r.IsActive {
			status = "disabled"
		}
		fmt.Fprintf(&sb, "%s,%s,%s,%s,%d,%s,%s\n",
			escapeCSV(r.ShortCode), escapeCSV(r.OriginalURL), escapeCSV(r.Title),
			escapeCSV(r.Domain), r.ClickCount, status, r.CreatedAt.Format("2006-01-02 15:04"))
	}
	return sb.String(), nil
}

// LogClick publishes a click event to Kafka; falls back to a direct write when Kafka is unavailable so clicks are not lost.
//
// The kind is carried rather than decided here: only the caller knows whether the client was served the
// guide page (and so may be a prefetch that never confirms) or was sent straight to the target.
func (s *LinkService) LogClick(ctx context.Context, e domain.ClickEvent) {
	// Nothing about recording a click may be able to stop the process. This runs on the goroutine the redirect
	// handler spawns for it - the handler returns its answer without waiting - and Gin's recovery only covers
	// the goroutine that serves the request, so a panic anywhere below this line would end the API instead of
	// failing one click. See publishClick for the case that actually happened.
	defer func() {
		if r := recover(); r != nil {
			metrics.ClickEvent(metrics.ClickPanicked)
			log.Printf("recording a click panicked, the click is lost: %v\n%s", r, debug.Stack())
		}
	}()

	// generate an idempotency key: used to deduplicate on Kafka redelivery or degraded direct write
	event := mq.ClickEvent{
		EventID:   newEventID(),
		LinkID:    e.LinkID,
		IP:        e.IP,
		UserAgent: e.UserAgent,
		Platform:  string(e.Platform),
		Kind:      string(e.Kind),
		Action:    string(e.Action),
		Referer:   e.Referer,
		CreatedAt: time.Now(),
	}

	if s.kafka != nil {
		if err := s.publishClick(ctx, event); err != nil {
			// Kafka failed -> fall back to direct write
			metrics.ClickEvent(metrics.ClickPublishFailed)
			log.Printf("kafka publish failed, falling back to direct write: %v", err)
		} else {
			metrics.ClickEvent(metrics.ClickPublished)
			return
		}
	}
	if s.clickWriter != nil {
		if err := s.clickWriter.WriteClick(ctx, event); err != nil {
			metrics.ClickEvent(metrics.ClickDirectWriteFailed)
			log.Printf("click direct write failed: %v", err)
		} else {
			metrics.ClickEvent(metrics.ClickDirectWrite)
		}
	}
}

// publishClick hands the event to the publisher, turning a panic into an error.
//
// Nothing about recording a click may be able to stop the process. This runs on the goroutine the redirect
// handler spawns for the click - the handler returns a 302 without waiting for it - and Gin's recovery only
// covers the goroutine that serves the request, so a panic here ends the API. That is not a theoretical
// failure: a nil publisher inside a non-nil interface panicked on every click on a host with Kafka disabled,
// which killed the process, restarted it under systemd, and answered 502 to whatever arrived in between.
//
// Recovering rather than letting it go is what also keeps the click: the error returns to the caller, which
// already falls back to writing the row directly.
func (s *LinkService) publishClick(ctx context.Context, event mq.ClickEvent) (err error) {
	defer func() {
		if r := recover(); r != nil {
			metrics.ClickEvent(metrics.ClickPanicked)
			log.Printf("publishing a click panicked, falling back to a direct write: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("the click publisher panicked: %v", r)
		}
	}()

	return s.kafka.PublishClick(ctx, event)
}

// BuildShortURL builds the full short URL
func (s *LinkService) BuildShortURL(domain, code string) string {
	return "https://" + domain + "/r/" + code
}

// ---- helpers ----

// toLinkInfo loads a link with its folder name and tags and maps it onto the API model.
// It returns nil when the link does not exist or is not owned by userID.
func (s *LinkService) toLinkInfo(ctx context.Context, linkID, userID int64) *domain.LinkInfo {
	var row entity.Link
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", linkID, userID).
		First(&row).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("load link %d failed: %v", linkID, err)
		}
		return nil
	}

	info := linkInfoFromEntity(row)

	// look up the folder name
	if info.FolderID != nil {
		var folder entity.Folder
		if err := s.db.WithContext(ctx).Select("name").Where("id = ?", *info.FolderID).First(&folder).Error; err == nil {
			name := folder.Name
			info.FolderName = &name
		}
	}

	// look up tags
	var tags []domain.LinkTagInfo
	if err := s.db.WithContext(ctx).
		Model(&entity.Tag{}).
		Select("tags.id, tags.name, tags.color").
		Joins("JOIN link_tags lt ON tags.id = lt.tag_id").
		Where("lt.link_id = ?", linkID).
		Scan(&tags).Error; err != nil {
		log.Printf("load link tags failed: %v", err)
	}
	if tags == nil {
		tags = []domain.LinkTagInfo{}
	}
	info.Tags = tags

	return info
}

// shortCodeTaken reports whether another link already uses the short code (exceptID=0 excludes nothing).
func (s *LinkService) shortCodeTaken(ctx context.Context, shortCode string, exceptID int64) bool {
	q := s.db.WithContext(ctx).Model(&entity.Link{}).Where("short_code = ?", shortCode)
	if exceptID != 0 {
		q = q.Where("id != ?", exceptID)
	}
	var count int64
	return q.Count(&count).Error == nil && count > 0
}

// newShortCode reads back the stored short code (used for cache invalidation after an update).
func (s *LinkService) newShortCode(ctx context.Context, linkID int64) string {
	var row entity.Link
	if err := s.db.WithContext(ctx).Select("short_code").Where("id = ?", linkID).First(&row).Error; err != nil {
		return ""
	}
	return row.ShortCode
}

// attachOwnedTags inserts the tag associations that belong to userID, skipping anything else
// (previously a non-owned tag could be attached, leaking its name/color across accounts).
func (s *LinkService) attachOwnedTags(ctx context.Context, userID, linkID int64, tagIDs []int64) {
	if len(tagIDs) == 0 {
		return
	}
	if err := attachOwnedTagsTx(s.db.WithContext(ctx), userID, linkID, tagIDs); err != nil {
		log.Printf("attach tags to link %d failed: %v", linkID, err)
	}
}

// attachOwnedTagsTx is the shared implementation so the create/update paths use identical filtering.
func attachOwnedTagsTx(tx *gorm.DB, userID, linkID int64, tagIDs []int64) error {
	if len(tagIDs) == 0 {
		return nil
	}
	return tx.Exec(`
		INSERT INTO link_tags (link_id, tag_id)
		SELECT ?, id FROM tags WHERE id IN ? AND user_id = ?
		ON CONFLICT DO NOTHING
	`, linkID, tagIDs, userID).Error
}

// linkInfoFromEntity maps a persistence row onto the API model.
func linkInfoFromEntity(row entity.Link) *domain.LinkInfo {
	info := &domain.LinkInfo{
		ID:            row.ID,
		ShortCode:     row.ShortCode,
		OriginalURL:   row.OriginalURL,
		Title:         row.Title,
		Description:   row.Description,
		ImageURL:      row.ImageURL,
		Domain:        row.Domain,
		ClickCount:    row.ClickCount,
		IsActive:      row.IsActive,
		BlockedAt:     row.BlockedAt,
		BlockedReason: row.BlockedReason,
		ExpiresAt:     row.ExpiresAt,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
		FolderID:      row.FolderID,
		HasPassword:   row.PasswordHash != nil && *row.PasswordHash != "",
		UTMSource:     row.UTMSource,
		UTMMedium:     row.UTMMedium,
		UTMCampaign:   row.UTMCampaign,
		UTMTerm:       row.UTMTerm,
		UTMContent:    row.UTMContent,
		IosURL:        row.IosURL,
		AndroidURL:    row.AndroidURL,
	}
	info.ShortURL = "https://" + row.Domain + "/r/" + row.ShortCode
	return info
}

// escapeCSV escapes a CSV field: fields starting with = + - @ or a tab get a single-quote prefix
// (preventing Excel formula injection), and fields containing commas/quotes/newlines are wrapped in quotes.
func escapeCSV(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	if strings.HasPrefix(s, "=") || strings.HasPrefix(s, "+") ||
		strings.HasPrefix(s, "-") || strings.HasPrefix(s, "@") ||
		strings.HasPrefix(s, "\t") {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\n") {
		s = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// newEventID generates the click event idempotency key (16 random bytes -> 32 hex chars).
// used to deduplicate Kafka at-least-once redeliveries / degraded direct writes, avoiding double increments of click_count.
func newEventID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateShortCode generates a 6-byte random short code (12 hex digits, 48 bits of entropy,
// about 16.7 million links to reach a 50% birthday collision probability)
func generateShortCode() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// hashPassword hashes the link access password with bcrypt (bcrypt caps at 72 bytes, longer input is truncated automatically)
func hashPassword(pwd string) string {
	if len(pwd) > 72 {
		pwd = pwd[:72]
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		// bcrypt failure is extremely rare (internal error), degrade to a value that never verifies
		return ""
	}
	return string(hash)
}

// checkPasswordHash verifies a password against a stored hash.
// new hashes are bcrypt; legacy unsalted SHA-256 hex hashes (64 hex digits) are still supported, both for
// link passwords and for the user passwords the pre-Go backend wrote.
func checkPasswordHash(password, hash string) bool {
	if hash == "" {
		return false
	}
	if isLegacyHash(hash) {
		// legacy SHA-256 hash, constant-time comparison
		legacy := sha256Hex(password)
		return hmac.Equal([]byte(legacy), []byte(hash))
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// isLegacyHash reports whether a stored hash is one of those unsalted SHA-256 digests. A user password that
// verifies against one is re-hashed with bcrypt on the spot, which is the only moment the plaintext is in
// hand; a link password is not, because nobody types it twice.
func isLegacyHash(hash string) bool {
	return len(hash) == 64 && isHexString(hash)
}

func isHexString(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
