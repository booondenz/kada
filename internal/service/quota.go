package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
)

// The ceilings on what one account may create.
//
// They are constants rather than environment variables on purpose: the numbers are a product decision, and a
// deployment that can raise its own is a deployment that quietly stops being protected. There is one
// allowance because there is one kind of account - a phone number used to buy a larger one, and the site has
// no phone numbers any more.
const (
	linksPerDay = 20

	quotaWindow = 24 * time.Hour
	// burstLinks per burstWindow applies to every account. It is what stops a new account from spending its
	// whole daily allowance in the minute it was created: the daily quota bounds a day, this bounds a moment,
	// and a script needs the moment.
	burstLinks  = 5
	burstWindow = 10 * time.Minute
)

// quotaDecision is the pure part of the ceiling: given what the account already holds, is this create one
// too many? It returns the refusal rather than a bool so the message and the retry-after are decided next to
// the numbers they describe.
//
// oldestDaily and oldestBurst are the created_at of the oldest row still inside each window; the retry-after
// is when that row falls out, which is the first moment the allowance has room again. A zero time means no
// row was found, and the whole window is reported instead of a guess.
func quotaDecision(daily, burst int64, oldestDaily, oldestBurst time.Time, now time.Time) *domain.RateLimitError {
	if burst >= burstLinks {
		return &domain.RateLimitError{
			Message:           "too many links created in a short time, please slow down",
			RetryAfterSeconds: waitUntil(oldestBurst, burstWindow, now),
		}
	}

	if daily >= int64(linksPerDay) {
		return &domain.RateLimitError{
			Message:           fmt.Sprintf("this account can create %d links a day, try again later", linksPerDay),
			RetryAfterSeconds: waitUntil(oldestDaily, quotaWindow, now),
		}
	}

	return nil
}

// waitUntil is how long until the oldest row in the window falls out of it.
func waitUntil(oldest time.Time, window time.Duration, now time.Time) int {
	if oldest.IsZero() {
		return int(window.Seconds())
	}
	seconds := int(window.Seconds() - now.Sub(oldest).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

// checkQuota refuses a create that would exceed the account's allowance.
//
// A failure to read the counts allows the create. The ceiling exists to stop a script, and a database that
// cannot answer a count is a database that would refuse the insert a moment later anyway; turning one bad
// query into "nobody can create a link" would be the more expensive failure.
func (s *LinkService) checkQuota(ctx context.Context, userID int64) error {
	now := time.Now().UTC()
	daily, oldestDaily := s.linksCreatedSince(ctx, userID, now.Add(-quotaWindow))
	burst, oldestBurst := s.linksCreatedSince(ctx, userID, now.Add(-burstWindow))

	if refusal := quotaDecision(daily, burst, oldestDaily, oldestBurst, now); refusal != nil {
		return refusal
	}
	return nil
}

// linksCreatedSince counts an account's links since a point in time, and returns the oldest of them - which
// is what the retry-after is calculated from, so the refusal says when it will lift rather than "later".
func (s *LinkService) linksCreatedSince(ctx context.Context, userID int64, since time.Time) (int64, time.Time) {
	var row struct {
		Count  int64
		Oldest *time.Time
	}

	err := s.db.WithContext(ctx).Model(&entity.Link{}).
		Select("count(*) AS count, min(created_at) AS oldest").
		Where("user_id = ? AND created_at >= ?", userID, since).
		Scan(&row).Error
	if err != nil {
		log.Printf("count a user's recent links failed: %v", err)
		return 0, time.Time{}
	}

	if row.Oldest == nil {
		return row.Count, time.Time{}
	}
	return row.Count, *row.Oldest
}
