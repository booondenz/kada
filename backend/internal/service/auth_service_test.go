package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
	"github.com/chun/kada-backend/internal/infra/sms"
)

func TestPhonePattern(t *testing.T) {
	valid := []string{"13812345678", "19912345678", "15800000000"}
	for _, p := range valid {
		if !phonePattern.MatchString(p) {
			t.Errorf("expected %q to be valid phone", p)
		}
	}
	invalid := []string{
		"", "123", "12345678901", // wrong length
		"11812345678", "12812345678", // invalid second digit
		"1381234567a",    // contains a letter
		"138 1234 5678",  // contains a space
		"aaaaaaaaaaa",    // all letters
		"+8613812345678", // with country code
	}
	for _, p := range invalid {
		if phonePattern.MatchString(p) {
			t.Errorf("expected %q to be invalid phone", p)
		}
	}
}

// recordingSender counts the calls that would have cost money.
type recordingSender struct {
	calls int
}

func (s *recordingSender) SendVerificationCode(string) (string, error) {
	s.calls++
	return "123456", nil
}

func (s *recordingSender) CheckVerificationCode(string, string) (bool, error) { return true, nil }

// A provider throttle is a wait, not a mistake: it has to leave the service as a quota so the handler
// answers 429 with a retry-after, and the provider's own code must not travel with it, because that is what
// put "biz.FREQUENCY" in front of a user.
func TestSMSSendErrorMapsAProviderThrottleToAQuota(t *testing.T) {
	throttled := fmt.Errorf("%w: biz.FREQUENCY", sms.ErrProviderThrottled)

	err := smsSendError(throttled)
	var limited *domain.RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("a throttled send must become a RateLimitError, got %T: %v", err, err)
	}
	if limited.RetryAfterSeconds != smsProviderThrottleWait {
		t.Errorf("retry after = %d, want %d", limited.RetryAfterSeconds, smsProviderThrottleWait)
	}
	if strings.Contains(limited.Message, "FREQUENCY") || strings.Contains(limited.Message, "provider code") {
		t.Errorf("the provider code must not reach the user: %q", limited.Message)
	}
}

// Everything else has to keep its cause: the provider code is what identifies an unapproved signature or
// template, and nobody can debug a failed sign-up without it.
func TestSMSSendErrorKeepsTheCauseOfARejection(t *testing.T) {
	rejected := errors.New("failed to send SMS, please try again later (provider code: isv.SMS_SIGNATURE_ILLEGAL)")

	err := smsSendError(rejected)
	if !errors.Is(err, rejected) {
		t.Errorf("the cause must stay in the chain, got: %v", err)
	}
	var limited *domain.RateLimitError
	if errors.As(err, &limited) {
		t.Error("a rejected signature must not be reported as a quota")
	}
}

// A malformed number must be rejected before anything is read or written. A nil *gorm.DB is the sharpest
// way to say so: if the check moved below the first query, this test would panic instead of passing.
func TestSendSMSCodeRejectsMalformedPhoneBeforeTouchingTheDatabase(t *testing.T) {
	sender := &recordingSender{}
	svc := NewAuthService(nil, "secret", "1h", sender)

	err := svc.SendSMSCode(context.Background(), "12345", "203.0.113.9", "captcha-id", "A3B7")
	if err == nil {
		t.Fatal("expected an error for a malformed phone number")
	}
	if sender.calls != 0 {
		t.Errorf("an SMS was sent for a malformed number (calls=%d)", sender.calls)
	}
}

// The captcha is checked first, and the same nil-DB trick proves the ordering: an unanswered challenge
// must be refused before any query, and therefore long before any provider call.
func TestSendSMSCodeRequiresASolvedCaptcha(t *testing.T) {
	sender := &recordingSender{}
	svc := NewAuthService(nil, "secret", "1h", sender)

	for _, c := range []struct{ id, answer string }{{"", ""}, {"", "A3B7"}, {"captcha-id", ""}} {
		err := svc.SendSMSCode(context.Background(), "13800138000", "203.0.113.9", c.id, c.answer)
		if err == nil {
			t.Errorf("expected an error for captcha id=%q answer=%q", c.id, c.answer)
		}
		var limited *domain.RateLimitError
		if errors.As(err, &limited) {
			t.Errorf("a missing captcha is bad input, not a quota: %v", err)
		}
	}
	if sender.calls != 0 {
		t.Errorf("an SMS was sent without a solved captcha (calls=%d)", sender.calls)
	}
}

// With no matching row, the atomic consume cannot confirm anything, so the send has to stop there. The
// dry-run database renders the SQL without executing it and returns zero rows, which is exactly the
// "nothing matched" case a wrong or expired challenge produces in production.
func TestSendSMSCodeStopsWhenTheCaptchaCannotBeConfirmed(t *testing.T) {
	sender := &recordingSender{}
	svc := NewAuthService(newDryRunDB(t), "secret", "1h", sender)

	err := svc.SendSMSCode(context.Background(), "13800138000", "203.0.113.9", "captcha-id", "WRONG")
	if err == nil {
		t.Fatal("expected the send to be refused when no captcha row matched")
	}
	if sender.calls != 0 {
		t.Errorf("an SMS was sent despite an unconfirmed captcha (calls=%d)", sender.calls)
	}
}

// The quotas are the thing standing between one script and an SMS bill, so their ordering matters and is
// easy to break by editing a constant: the hourly figure must be smaller than the daily one, and the
// per-IP allowance must be larger than the per-phone one (a NAT legitimately shares an address).
func TestQuotaConstantsAreConsistent(t *testing.T) {
	if smsIPHourlyMax >= smsIPDailyMax {
		t.Errorf("the per-IP hourly quota (%d) must be below the daily one (%d)", smsIPHourlyMax, smsIPDailyMax)
	}
	if smsPhoneDailyMax > smsIPDailyMax {
		t.Errorf("a single phone may not consume more than the whole network's daily allowance (%d > %d)",
			smsPhoneDailyMax, smsIPDailyMax)
	}
	if smsPhoneCooldown < 30*time.Second {
		t.Errorf("a 60s cooldown is the floor that makes retrying pointless; got %v", smsPhoneCooldown)
	}
}

func TestRandomIDIsHexAndNotPredictable(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := randomID()
		if err != nil {
			t.Fatalf("randomID() failed: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("randomID() returned %d characters (%q), want 32", len(id), id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("randomID() returned %q, which is not hex: %v", id, err)
		}
		if seen[id] {
			t.Fatalf("randomID() repeated %q within 100 calls", id)
		}
		seen[id] = true
	}
}

// An unknown address is stored as NULL, so it cannot be counted as one shared bucket by the per-IP quotas.
func TestStringPtrTreatsEmptyAsAbsent(t *testing.T) {
	if got := stringPtr(""); got != nil {
		t.Errorf("stringPtr(\"\") = %v, want nil", got)
	}
	if got := stringPtr("203.0.113.9"); got == nil || *got != "203.0.113.9" {
		t.Errorf("stringPtr(\"203.0.113.9\") = %v, want a pointer to that value", got)
	}
}

// ========== Passwords ==========

// The floor counts characters and the ceiling counts bytes, because the two limits answer different
// questions: a password has to be long enough to be worth hashing, and short enough that bcrypt reads all
// of it. A passphrase of Chinese characters is the case that tells them apart.
func TestValidatePasswordBounds(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"the shortest accepted", "12345678", false},
		{"one below the floor", "1234567", true},
		{"empty", "", true},
		{"eight Chinese characters: eight characters, twenty-four bytes", "智能短链接管理平台", false},
		{"exactly the byte ceiling", strings.Repeat("a", 72), false},
		{"one byte past the ceiling", strings.Repeat("a", 73), true},
		{"twenty-five Chinese characters is over the byte ceiling", strings.Repeat("密", 25), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassword(%d characters) = %v, wantErr %v", len([]rune(tt.password)), err, tt.wantErr)
			}
		})
	}
}

// A password that cannot be the credential it is meant to be is refused before anything is read or written.
// A nil *gorm.DB is the sharpest way to say so: if the check moved below the first query, this test would
// panic instead of passing - and the query it would reach costs an SMS code that has already been sent.
func TestRegisterRejectsAWeakPasswordBeforeTouchingTheDatabase(t *testing.T) {
	svc := NewAuthService(nil, "secret", "1h", &recordingSender{})

	for _, password := range []string{"", "short", "1234567"} {
		if _, err := svc.Register(context.Background(), "13800138000", "123456", password); err == nil {
			t.Errorf("password %q was accepted", password)
		}
	}
	// The number is checked first of all: a malformed one must not reach the password rule either.
	if _, err := svc.Register(context.Background(), "12345", "123456", "correct-horse"); err == nil {
		t.Error("a malformed phone number was accepted")
	}
}

// withUserRow makes a dry-run handle answer a user lookup with this row.
//
// DryRun never executes anything, so a query that is supposed to find an account finds nothing at all.
// Without this, the only reachable outcome is "no such account", which is exactly the path a password test
// must not stop at.
func withUserRow(t *testing.T, row entity.User) *gorm.DB {
	t.Helper()

	db := newDryRunDB(t)
	if err := db.Callback().Query().After("gorm:query").Register("test:user_row", func(tx *gorm.DB) {
		if dest, ok := tx.Statement.Dest.(*entity.User); ok {
			*dest = row
		}
	}); err != nil {
		t.Fatalf("failed to register the user-row callback: %v", err)
	}
	return db
}

// Every way a password sign-in can fail has to be the same answer: an unknown number, a wrong password, and
// an account that has no password at all. Anything else turns the endpoint into a way to ask which numbers
// have accounts here.
func TestPasswordLoginDoesNotSayWhichWayItFailed(t *testing.T) {
	hash := hashPassword("correct-horse")

	tests := []struct {
		name     string
		row      *entity.User
		password string
	}{
		{"an unknown number", nil, "correct-horse"},
		{"a wrong password", &entity.User{ID: 7, PasswordHash: &hash}, "wrong-horse"},
		{"an account with no password yet", &entity.User{ID: 7, PasswordHash: nil}, "correct-horse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newDryRunDB(t)
			if tt.row != nil {
				db = withUserRow(t, *tt.row)
			}
			svc := NewAuthService(db, "secret", "1h", nil)

			_, err := svc.LoginWithPassword(context.Background(), "13800138000", tt.password)
			if !errors.Is(err, domain.ErrInvalidCredentials) {
				t.Errorf("error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

// The whole point of the change: the right password signs in, and it costs no SMS.
func TestPasswordLoginIssuesATokenForTheRightPassword(t *testing.T) {
	hash := hashPassword("correct-horse")
	phone := "13800138000"
	svc := NewAuthService(withUserRow(t, entity.User{ID: 42, Phone: &phone, PasswordHash: &hash}), "secret", "1h", nil)

	// The sender records every message that would have cost money; a password sign-in must not reach it.
	sender := &recordingSender{}
	svc.sms = sender

	resp, err := svc.LoginWithPassword(context.Background(), phone, "correct-horse")
	if err != nil {
		t.Fatalf("LoginWithPassword returned %v", err)
	}
	if resp.Token == "" {
		t.Error("expected a token")
	}
	if resp.User.ID != 42 {
		t.Errorf("token issued for user %d, want 42", resp.User.ID)
	}
	if sender.calls != 0 {
		t.Errorf("signing in with a password sent %d SMS", sender.calls)
	}
}

// A password stored by the pre-Go backend is accepted once and upgraded in place. Doing it here is the only
// chance: the plaintext is in hand for exactly this moment, and the alternative is leaving those accounts on
// an unsalted digest for good.
func TestASignInUpgradesALegacyPasswordHash(t *testing.T) {
	legacy := sha256Hex("correct-horse")
	phone := "13800138000"
	db := withUserRow(t, entity.User{ID: 4, Phone: &phone, PasswordHash: &legacy})

	upgraded := false
	if err := db.Callback().Update().After("gorm:update").Register("test:capture_upgrade", func(tx *gorm.DB) {
		// The same request also stamps last_login_at, so the column is what tells the two writes apart.
		if strings.Contains(tx.Statement.SQL.String(), "password_hash") {
			upgraded = true
		}
	}); err != nil {
		t.Fatalf("failed to register the update capture: %v", err)
	}

	svc := NewAuthService(db, "secret", "1h", nil)
	if _, err := svc.LoginWithPassword(context.Background(), phone, "correct-horse"); err != nil {
		t.Fatalf("LoginWithPassword returned %v", err)
	}
	if !upgraded {
		t.Error("a legacy hash was accepted without being upgraded")
	}
}

// A malformed number is rejected before the lookup, so a login attempt cannot be used to probe the database
// with arbitrary input.
func TestPasswordLoginRejectsAMalformedPhone(t *testing.T) {
	svc := NewAuthService(nil, "secret", "1h", nil)

	if _, err := svc.LoginWithPassword(context.Background(), "12345", "correct-horse"); err == nil {
		t.Fatal("expected an error for a malformed phone number")
	}
}
