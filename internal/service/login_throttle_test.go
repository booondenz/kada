package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// The boundary is the whole feature: one failure short of the bound has to let a person try again, and the
// failure that reaches it has to stop a script. An off-by-one here is either a bound nobody can reach or one
// that fires on the fourth honest mistake, and nothing else in the system would report it.
func TestLoginLockFiresExactlyAtTheBound(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		failures   int64
		oldest     time.Time
		wantLocked bool
		wantRetry  int
		reason     string
	}{
		{"nothing recorded", 0, now, false, 0, "a number that has not failed may try"},
		{"one short of the bound", loginMaxFailures - 1, now, false, 0, "the last honest mistake still gets an answer"},
		{"the bound, just failed", loginMaxFailures, now, true, int(loginFailureWindow.Seconds()), "the whole window is left to wait"},
		{"a second into the wait", loginMaxFailures, now.Add(-time.Second), true, int(loginFailureWindow.Seconds()) - 1, "the wait counts down"},
		{"a fraction left", loginMaxFailures, now.Add(-loginFailureWindow + 1500*time.Millisecond), true, 2, "rounded up, not truncated"},
		{"one second left", loginMaxFailures, now.Add(-loginFailureWindow + time.Second), true, 1, "never zero: a zero invites a retry that cannot work"},
		{"exactly at the window", loginMaxFailures, now.Add(-loginFailureWindow), false, 0, "the window has passed"},
		{"beyond the window", loginMaxFailures, now.Add(-2 * loginFailureWindow), false, 0, "unreachable through the store, answered anyway"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locked, retry := loginLock(tt.failures, tt.oldest, now)
			if locked != tt.wantLocked || retry != tt.wantRetry {
				t.Errorf("loginLock(%d failures, oldest %v) = (%v, %ds), want (%v, %ds): %s",
					tt.failures, tt.oldest, locked, retry, tt.wantLocked, tt.wantRetry, tt.reason)
			}
		})
	}
}

// The count is the statement that can break the feature without breaking anything else: a query that matches no
// rows, or the wrong column, reads as "nobody has failed" and the bound silently stops existing. Rendering it
// is enough to pin the table and the parameterized window, and it is what the other service tests do.
func TestLoginFailureCountLooksAtTheRightRows(t *testing.T) {
	db := newDryRunDB(t)

	var built string
	if err := db.Callback().Query().After("gorm:query").Register("test:capture_login_count", func(tx *gorm.DB) {
		built = tx.Statement.SQL.String()
	}); err != nil {
		t.Fatalf("failed to register the capture callback: %v", err)
	}

	store := &loginFailureStore{db: db}
	retryAfter, err := store.retryAfter(context.Background(), "13800138000")
	if err != nil {
		t.Fatalf("retryAfter returned %v", err)
	}
	if retryAfter != 0 {
		t.Errorf("retryAfter = %d with no rows read, want 0", retryAfter)
	}

	for _, want := range []string{
		"COUNT(*)",
		"MIN(created_at)",
		"login_attempts",
		// The window is a bound parameter, so the constant is the only place its length is written.
		"INTERVAL '1 second' *",
	} {
		if !strings.Contains(built, want) {
			t.Errorf("the count statement does not contain %q:\n%s", want, built)
		}
	}
	if !strings.Contains(built, "phone") {
		t.Errorf("the count statement does not filter by phone:\n%s", built)
	}
}

// Both writes are asserted the same way, for the same reason: a DELETE that forgets the phone clause would
// clear every number's record on one sign-in, and a purge that filters the wrong column would either keep
// everything or delete the rows the count depends on.
func TestLoginFailureWrites(t *testing.T) {
	tests := []struct {
		name string
		run  func(*loginFailureStore) error
		want []string
	}{
		{
			name: "clearing one number",
			run:  func(s *loginFailureStore) error { return s.clear(context.Background(), "13800138000") },
			want: []string{"DELETE", "login_attempts", "phone"},
		},
		{
			name: "recording one failure",
			run:  func(s *loginFailureStore) error { return s.record(context.Background(), "13800138000") },
			want: []string{"INSERT", "login_attempts", "created_at"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDryRunDB(t)

			var built []string
			capture := func(tx *gorm.DB) { built = append(built, tx.Statement.SQL.String()) }
			if err := db.Callback().Create().After("gorm:create").Register("test:capture_create", capture); err != nil {
				t.Fatalf("failed to register the create callback: %v", err)
			}
			if err := db.Callback().Delete().After("gorm:delete").Register("test:capture_delete", capture); err != nil {
				t.Fatalf("failed to register the delete callback: %v", err)
			}

			if err := tt.run(&loginFailureStore{db: db}); err != nil {
				t.Fatalf("the statement returned %v", err)
			}

			statement := strings.Join(built, "\n")
			for _, want := range tt.want {
				if !strings.Contains(statement, want) {
					t.Errorf("the statement does not contain %q:\n%s", want, statement)
				}
			}
		})
	}
}
