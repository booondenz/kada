package mq

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// ClickEvent is a short-link click event (message payload shared by producer and consumer)
type ClickEvent struct {
	EventID   string `json:"event_id"` // idempotency key used by the worker for deduplication
	LinkID    int64  `json:"link_id"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	Platform  string `json:"platform"`
	// Kind is what this event is evidence of (domain.ClickKind). It travels with the event rather than
	// being derived at write time: the decision is made where the request is, which is the only place that
	// knows whether the guide page was served or the client went straight to the target.
	Kind string `json:"kind"`
	// Action is which guide page interaction this was (domain.Action), empty for anything else. It is a
	// field of its own: it used to ride in Referer behind an "action:" prefix, so every report that read
	// referers had to know that some of them were not referers.
	Action    string    `json:"action,omitempty"`
	Referer   string    `json:"referer"`
	CreatedAt time.Time `json:"created_at"`
}

// ClickPublisher publishes click events (producer-side interface, eases test mocks and degradation)
type ClickPublisher interface {
	PublishClick(ctx context.Context, e ClickEvent) error
}

// messageWriter narrows kafka.Writer's write interface so tests can inject a fake
type messageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// KafkaClickPublisher is the segmentio/kafka-go based implementation
type KafkaClickPublisher struct {
	writer messageWriter
}

// NewKafkaClickPublisher builds a publisher; returns nil when brokers is empty (Kafka disabled)
func NewKafkaClickPublisher(brokers []string, topic string) *KafkaClickPublisher {
	var valid []string
	for _, b := range brokers {
		if b = strings.TrimSpace(b); b != "" {
			valid = append(valid, b)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return &KafkaClickPublisher{
		writer: kafka.NewWriter(kafka.WriterConfig{
			Brokers:      valid,
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: int(kafka.RequireOne),
		}),
	}
}

// PublishClick serializes a ClickEvent and writes it to the topic.
//
// The nil check is not decoration. This value travels in an interface, and a nil *KafkaClickPublisher inside
// a non-nil interface passes every `publisher != nil` guard the caller has - so the alternative to this line
// is a nil dereference on whatever goroutine called it. It reports an error instead, which the caller
// already answers by writing the click straight to the database.
func (p *KafkaClickPublisher) PublishClick(ctx context.Context, e ClickEvent) error {
	if p == nil {
		return errors.New("the kafka publisher is not configured")
	}

	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return p.writer.WriteMessages(ctx, kafka.Message{Value: b})
}

// Close closes the underlying writer
func (p *KafkaClickPublisher) Close() error {
	if w, ok := p.writer.(*kafka.Writer); ok {
		return w.Close()
	}
	return nil
}
