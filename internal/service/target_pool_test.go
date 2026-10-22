package service

import (
	"testing"
	"time"

	"github.com/booondenz/kada/internal/domain"
)

// The roll decides, and the arithmetic decides the roll: 3 against 1 means the first target owns rolls 0, 1
// and 2 whatever the random source did.
func TestWeightedPick_DividesTheRollsInProportion(t *testing.T) {
	candidates := []domain.LinkTargetInfo{{ID: 1, Weight: 3}, {ID: 2, Weight: 1}}

	want := map[int]int64{0: 1, 1: 1, 2: 1, 3: 2}
	for roll, id := range want {
		got := weightedPick(candidates, roll)
		if got == nil || got.ID != id {
			t.Errorf("weightedPick(roll=%d) = %v, want target %d", roll, got, id)
		}
	}

	if got := weightedPick(candidates, 4); got != nil {
		t.Errorf("a roll past the total picks %v, want nil", got)
	}
}

// A weight of zero is how a target is kept in the pool without receiving traffic, so it must never be the
// answer to a roll of zero - which is the roll it would win if the arithmetic were wrong.
func TestWeightedPick_NeverPicksAWeightOfZero(t *testing.T) {
	candidates := []domain.LinkTargetInfo{{ID: 1, Weight: 0}, {ID: 2, Weight: 1}}

	got := weightedPick(candidates, 0)
	if got == nil || got.ID != 2 {
		t.Errorf("weightedPick = %v, want the only target with weight", got)
	}
}

func TestPickTarget_SkipsDisabledZeroWeightAndFullTargets(t *testing.T) {
	svc := &LinkService{}
	link := &domain.LinkInfo{Targets: []domain.LinkTargetInfo{
		{ID: 1, Weight: 5, Enabled: false},
		{ID: 2, Weight: 0, Enabled: true},
		{ID: 3, Weight: 1, Enabled: true},
	}}

	for i := 0; i < 20; i++ {
		got := svc.PickTarget(link)
		if got == nil || got.ID != 3 {
			t.Fatalf("PickTarget = %v, want the one live target", got)
		}
	}
}

func TestPickTarget_ReturnsNothingWithoutAPool(t *testing.T) {
	svc := &LinkService{}

	if got := svc.PickTarget(&domain.LinkInfo{}); got != nil {
		t.Errorf("PickTarget on a link with no pool = %v, want nil", got)
	}
	if got := svc.PickTarget(nil); got != nil {
		t.Errorf("PickTarget(nil) = %v, want nil", got)
	}
}

func TestPickTarget_SkipsAnExpiredTarget(t *testing.T) {
	svc := &LinkService{}
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	link := &domain.LinkInfo{Targets: []domain.LinkTargetInfo{
		{ID: 1, Weight: 1, Enabled: true, Kind: domain.TargetWechatGroup, ExpiresAt: &past},
		{ID: 2, Weight: 1, Enabled: true, Kind: domain.TargetWechatGroup, ExpiresAt: &future},
	}}

	for i := 0; i < 20; i++ {
		got := svc.PickTarget(link)
		if got == nil || got.ID != 2 {
			t.Fatalf("PickTarget = %v, want the target that has not expired", got)
		}
	}
}

func TestPickTarget_ReturnsNothingWhenEveryTargetExpired(t *testing.T) {
	svc := &LinkService{}
	past := time.Now().Add(-time.Minute)
	link := &domain.LinkInfo{Targets: []domain.LinkTargetInfo{
		{ID: 1, Weight: 1, Enabled: true, Kind: domain.TargetWechatGroup, ExpiresAt: &past},
	}}

	if got := svc.PickTarget(link); got != nil {
		t.Errorf("PickTarget = %v, want nil when every target expired", got)
	}
}

func TestDispatchQuota_CapsATargetForTheDay(t *testing.T) {
	var q dispatchQuota
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

	if !q.available(1, 2, now) {
		t.Fatal("a target with room must be available")
	}
	q.consume(1, now)
	q.consume(1, now)

	if q.available(1, 2, now) {
		t.Error("a target at its quota must stop receiving")
	}
	if !q.available(2, 2, now) {
		t.Error("one target's quota must not close another")
	}
	if !q.available(1, 0, now) {
		t.Error("a quota of zero means unlimited")
	}
}

func TestDispatchQuota_ResetsOnTheNextDay(t *testing.T) {
	var q dispatchQuota
	day1 := time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Hour)

	q.consume(1, day1)
	if q.available(1, 1, day1) {
		t.Fatal("the target should be full on the day it was used")
	}
	if !q.available(1, 1, day2) {
		t.Error("a new day must have room again")
	}
}
