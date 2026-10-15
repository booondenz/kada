package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain/entity"
)

// Password guessing is bounded per phone number, not only per address.
//
// The strict limiter counts per IP, so a pool of addresses multiplies the guesses one account has to survive:
// twenty attempts a minute each, and nothing that notices the same number being tried from all of them. These
// two numbers are the bound that does not care where the caller is. They are deliberately reachable by a
// person who has forgotten which password they used, and deliberately expensive for a script: five mistakes
// inside a quarter of an hour, after which that number waits the window out.
const (
	loginFailureWindow = 15 * time.Minute
	loginMaxFailures   = 5
)

// loginLock decides whether the failures recorded for one number block the next sign-in, and for how long.
//
// A pure function of the count, the oldest failure and now, so the boundary is pinned by a table-driven test
// rather than by a database: an off-by-one here is either a bound nobody can reach or one that fires on the
// fourth honest mistake, and neither shows up as a failure anywhere else.
//
// The lock lasts until the oldest of the counted failures falls out of the window, which is what makes the
// retry-after a number the caller can actually wait out.
func loginLock(failures int64, oldest, now time.Time) (locked bool, retryAfterSeconds int) {
	if failures < loginMaxFailures {
		return false, 0
	}

	remaining := oldest.Add(loginFailureWindow).Sub(now)
	if remaining <= 0 {
		// The store only counts rows inside the window, so this is unreachable through it. It is answered
		// rather than assumed because the cost of being wrong is a lockout with no way out.
		return false, 0
	}

	// Rounded up: a retry-after of 0 invites a retry that cannot succeed yet.
	seconds := int(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return true, seconds
}

// loginThrottle is the counter a password sign-in is bound by.
//
// It is an interface because the behavior worth testing is what the service does with it - that a failure is
// recorded, that a success clears the record, and that a locked number never reaches the password comparison -
// and those are assertions about calls rather than about SQL. The statements are rendered and asserted in the
// store's own test.
type loginThrottle interface {
	// retryAfter returns how many seconds this number has to wait, or 0 when it may try.
	retryAfter(ctx context.Context, phone string) (int, error)
	// record stores one failure.
	record(ctx context.Context, phone string) error
	// clear forgets the failures recorded for a number.
	clear(ctx context.Context, phone string) error
}

// loginFailureStore is the PostgreSQL implementation: one row per failure, read as a count over the window.
//
// A table rather than Redis for the reason the captcha is one: caching fails open in this deployment on
// purpose, and a lockout that disappears when the cache does is a security feature that silently stops
// existing. The database is not optional.
type loginFailureStore struct{ db *gorm.DB }

// retryAfter counts the failures inside the window and asks loginLock what they mean.
//
// A read error is reported to the caller rather than swallowed here, because whether an unreadable counter
// should block a sign-in is the service's decision and it depends on the database being reachable at all - see
// LoginWithPassword.
func (s *loginFailureStore) retryAfter(ctx context.Context, phone string) (int, error) {
	var row struct {
		Failures int64
		Oldest   *time.Time
	}

	// The window is a bound parameter multiplied by a literal interval, so the constant above is the only
	// place the length is written and nothing is interpolated into the statement.
	//
	// Find rather than Scan, deliberately: for a struct destination the two do the same thing, and Find is
	// the one GORM still builds under a DryRun session - which is how this statement is asserted in the test
	// without a database.
	err := s.db.WithContext(ctx).Model(&entity.LoginAttempt{}).
		Select("COUNT(*) AS failures, MIN(created_at) AS oldest").
		Where("phone = ? AND created_at > NOW() - INTERVAL '1 second' * ?",
			phone, int(loginFailureWindow.Seconds())).
		Find(&row).Error
	if err != nil {
		return 0, err
	}
	if row.Oldest == nil {
		return 0, nil
	}

	locked, retryAfter := loginLock(row.Failures, *row.Oldest, time.Now())
	if !locked {
		return 0, nil
	}
	return retryAfter, nil
}

// record stores one failure and drops the rows that have fallen out of the window.
//
// The purge rides on the write, like the captcha purge does, because this table only grows while somebody is
// guessing and this deployment has no scheduler to ask. A row outside the window can never affect a decision
// again, so there is nothing to keep.
func (s *loginFailureStore) record(ctx context.Context, phone string) error {
	// created_at is filled from the application clock and the window is compared against the database's;
	// running both on one host is what makes those the same clock.
	if err := s.db.WithContext(ctx).Create(&entity.LoginAttempt{Phone: phone}).Error; err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).
		Where("created_at < NOW() - INTERVAL '1 second' * ?", int(loginFailureWindow.Seconds())).
		Delete(&entity.LoginAttempt{}).Error; err != nil {
		return fmt.Errorf("purge expired login attempts: %w", err)
	}
	return nil
}

// clear forgets every failure recorded for a number, which is what makes the bound consecutive.
func (s *loginFailureStore) clear(ctx context.Context, phone string) error {
	return s.db.WithContext(ctx).
		Where("phone = ?", phone).
		Delete(&entity.LoginAttempt{}).Error
}
