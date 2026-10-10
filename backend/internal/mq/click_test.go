package mq

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

type fakeWriter struct {
	got []kafka.Message
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	f.got = append(f.got, msgs...)
	return nil
}

func TestClickEventRoundTrip(t *testing.T) {
	e := ClickEvent{
		EventID: "evt-idempotent-key", LinkID: 42, IP: "1.2.3.4", UserAgent: "Mozilla/5.0",
		Platform: "wechat", Referer: "https://x.com",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got ClickEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != e {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, e)
	}
}

func TestNewKafkaClickPublisher_NoBrokersReturnsNil(t *testing.T) {
	if got := NewKafkaClickPublisher(nil, "clicks"); got != nil {
		t.Fatalf("expected nil for empty brokers, got %+v", got)
	}
	if got := NewKafkaClickPublisher([]string{"  ", ""}, "clicks"); got != nil {
		t.Fatalf("expected nil for blank brokers, got %+v", got)
	}
}

// A nil publisher answers with an error rather than dereferencing itself.
//
// The constructor above returns nil when no broker is configured, and that value reaches callers inside an
// interface - where it is not nil any more, so every `publisher != nil` guard passes. This is what the caller
// then hits, and the difference between an error and a panic is the difference between a click written
// straight to the database and the API process dying on the goroutine that recorded it.
func TestKafkaClickPublisher_NilReceiverIsAnErrorNotAPanic(t *testing.T) {
	var p *KafkaClickPublisher

	err := p.PublishClick(context.Background(), ClickEvent{LinkID: 1})
	if err == nil {
		t.Fatal("a nil publisher must report an error")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("the error should say what is wrong, got: %v", err)
	}
}

func TestKafkaClickPublisher_PublishClickWritesJSON(t *testing.T) {
	w := &fakeWriter{}
	p := &KafkaClickPublisher{writer: w}
	e := ClickEvent{LinkID: 7, IP: "9.9.9.9", Platform: "browser", CreatedAt: time.Unix(1700000000, 0).UTC()}
	if err := p.PublishClick(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(w.got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(w.got))
	}
	var decoded ClickEvent
	if err := json.Unmarshal(w.got[0].Value, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != e {
		t.Fatalf("decoded mismatch: got %+v want %+v", decoded, e)
	}
}
