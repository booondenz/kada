package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/chun/kada-backend/internal/mq"
)

type assertErr string

func (e assertErr) Error() string { return string(e) }

type recorderWriter struct {
	got []mq.ClickEvent
}

func (r *recorderWriter) WriteClick(_ context.Context, event mq.ClickEvent) error {
	r.got = append(r.got, event)
	return nil
}

func TestProcessClickMessage_Valid(t *testing.T) {
	w := &recorderWriter{}
	e := mq.ClickEvent{EventID: "evt-1", LinkID: 5, IP: "8.8.8.8", UserAgent: "ua", Platform: "qq", Kind: "request", Referer: "r", CreatedAt: time.Unix(1700000000, 0).UTC()}
	b, _ := json.Marshal(e)
	if err := processClickMessage(b, w); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 || w.got[0].LinkID != 5 {
		t.Fatalf("expected 1 write with link 5, got %+v", w.got)
	}
	if w.got[0].EventID != "evt-1" {
		t.Fatalf("expected EventID to be preserved, got %q", w.got[0].EventID)
	}
	// The kind decides whether the click is counted, and the worker is where it would be lost: it is
	// rebuilt from JSON here, and a field the payload does not carry arrives as nothing.
	if w.got[0].Kind != "request" {
		t.Fatalf("expected Kind to survive the queue, got %q", w.got[0].Kind)
	}
	if !w.got[0].CreatedAt.Equal(e.CreatedAt) {
		t.Fatalf("expected CreatedAt to be preserved, got %v want %v", w.got[0].CreatedAt, e.CreatedAt)
	}
}

func TestProcessClickMessage_InvalidJSON(t *testing.T) {
	w := &recorderWriter{}
	if err := processClickMessage([]byte("{not json"), w); err == nil {
		t.Fatal("expected error for invalid json")
	}
	if len(w.got) != 0 {
		t.Fatalf("expected no write on invalid json, got %d", len(w.got))
	}
}

func TestIsPermanentError(t *testing.T) {
	// Invalid JSON -> permanent error
	if err := processClickMessage([]byte("{bad"), &recorderWriter{}); !isPermanentError(err) {
		t.Error("processClickMessage invalid json should be classified permanent")
	}

	// Foreign key violation (23503, e.g. the link was deleted) -> permanent error
	fkErr := &pgconn.PgError{Code: "23503", Message: "violates foreign key constraint"}
	if !isPermanentError(fkErr) {
		t.Error("foreign key violation should be permanent")
	}

	// Connection-class error (08006 connection lost) -> retryable
	connErr := &pgconn.PgError{Code: "08006", Message: "connection lost"}
	if isPermanentError(connErr) {
		t.Error("connection failure should be retryable")
	}

	// Plain error -> retryable
	if isPermanentError(assertErr("boom")) {
		t.Error("plain error should be retryable")
	}
}

func TestAttemptTracker(t *testing.T) {
	tr := newAttemptTracker()
	if n := tr.record("clicks", 0, 42); n != 1 {
		t.Errorf("expected attempt 1, got %d", n)
	}
	if n := tr.record("clicks", 0, 42); n != 2 {
		t.Errorf("expected attempt 2, got %d", n)
	}
	tr.reset("clicks", 0, 42)
	if n := tr.record("clicks", 0, 42); n != 1 {
		t.Errorf("expected attempt 1 after reset, got %d", n)
	}
}
