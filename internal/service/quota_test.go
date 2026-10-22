package service

import (
	"strings"
	"testing"
	"time"
)

func TestQuotaDecision_RefusesTheSixthLinkInTenMinutes(t *testing.T) {
	now := time.Now().UTC()
	oldest := now.Add(-2 * time.Minute)

	refusal := quotaDecision(false, 5, burstLinks, oldest, oldest, now)
	if refusal == nil {
		t.Fatal("a burst over the ceiling must be refused")
	}
	if !strings.Contains(refusal.Message, "slow down") {
		t.Errorf("message = %q, want the burst refusal", refusal.Message)
	}
	if refusal.RetryAfterSeconds != 480 {
		t.Errorf("retry after = %d, want the 8 minutes left of the burst window", refusal.RetryAfterSeconds)
	}
}

// The burst window is what a script needs; the daily quota is what a patient one needs. Both have to hold,
// and the daily one is the larger number only for a claimed account.
func TestQuotaDecision_DailyAllowanceDependsOnClaimingANumber(t *testing.T) {
	now := time.Now().UTC()
	oldest := now.Add(-time.Hour)

	if refusal := quotaDecision(true, 30, 0, oldest, oldest, now); refusal != nil {
		t.Errorf("a claimed account with 30 links today must be allowed, got %q", refusal.Message)
	}

	refusal := quotaDecision(false, guestLinksPerDay, 0, oldest, oldest, now)
	if refusal == nil {
		t.Fatal("a guest at the daily allowance must be refused")
	}
	if !strings.Contains(refusal.Message, "20 links a day") {
		t.Errorf("message = %q, want it to name the allowance", refusal.Message)
	}
	if refusal.RetryAfterSeconds != int((23 * time.Hour).Seconds()) {
		t.Errorf("retry after = %d, want the rest of the 24 hour window", refusal.RetryAfterSeconds)
	}
}

func TestQuotaDecision_AllowsAnAccountInsideItsAllowance(t *testing.T) {
	now := time.Now().UTC()
	oldest := now.Add(-time.Hour)

	if refusal := quotaDecision(false, 19, 4, oldest, oldest, now); refusal != nil {
		t.Errorf("20th create of the day must be allowed, got %q", refusal.Message)
	}
}

// With no row inside the window there is no oldest timestamp to count from, and the honest answer is the
// whole window rather than a guess at zero seconds.
func TestQuotaDecision_WithoutAnOldestRowReportsTheWholeWindow(t *testing.T) {
	now := time.Now().UTC()

	refusal := quotaDecision(false, guestLinksPerDay, 0, time.Time{}, time.Time{}, now)
	if refusal == nil {
		t.Fatal("a guest at the daily allowance must be refused")
	}
	if refusal.RetryAfterSeconds != int(quotaWindow.Seconds()) {
		t.Errorf("retry after = %d, want %d", refusal.RetryAfterSeconds, int(quotaWindow.Seconds()))
	}
}

// A window whose oldest row has just fallen out cannot report a negative wait.
func TestWaitUntil_NeverReturnsLessThanASecond(t *testing.T) {
	now := time.Now().UTC()

	if got := waitUntil(now.Add(-2*burstWindow), burstWindow, now); got != 1 {
		t.Errorf("waitUntil = %d, want 1", got)
	}
}
