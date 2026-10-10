package service

import (
	"context"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/mq"
)

// ClickWriter persists click events.
//
// It takes the event itself rather than its columns one at a time: that shape already exists as the
// payload the producer and the consumer share, and a signature that grows a parameter per column is a
// signature whose arguments can eventually be passed in the wrong order.
type ClickWriter interface {
	WriteClick(ctx context.Context, event mq.ClickEvent) error
}

// ClickStore is the GORM implementation of ClickWriter
type ClickStore struct {
	db *gorm.DB
}

func NewClickStore(db *gorm.DB) *ClickStore {
	return &ClickStore{db: db}
}

// WriteClick inside a transaction: insert the click log + increment the counter.
// deduplicates by event_id: when Kafka redelivers, or the degraded direct write races the worker on the same event,
// the second INSERT hits the unique conflict (RowsAffected=0) and click_count is not incremented again.
//
// Only a visit moves the counter. The row is written either way - a request from a platform prefetch is
// worth recording, it is just not worth counting as a person - so links.click_count and the analytics
// endpoints agree on what a click is, rather than one of them filtering afterwards.
func (s *ClickStore) WriteClick(ctx context.Context, event mq.ClickEvent) error {
	kind := domain.ClickKind(event.Kind)
	if kind == "" {
		// An event from a producer older than this column carries no kind. It is a request and nothing
		// more: not being able to say it was a person is the one answer that must never count.
		kind = domain.ClickRequest
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`
			INSERT INTO click_logs (link_id, ip, user_agent, platform, kind, action, referer, created_at, event_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (event_id) DO NOTHING
		`, event.LinkID, event.IP, event.UserAgent, event.Platform, string(kind), nullable(event.Action), event.Referer, event.CreatedAt, event.EventID)
		if res.Error != nil {
			return res.Error
		}

		// duplicate event: the log was already written, skip the counter increment and only commit (staying idempotent)
		if res.RowsAffected == 0 {
			return nil
		}

		if kind != domain.ClickVisit {
			return nil
		}

		return tx.Exec(`UPDATE links SET click_count = click_count + 1 WHERE id = ?`, event.LinkID).Error
	})
}

// nullable turns the empty string into a SQL NULL, which is what an absent action has to be: the question
// "did this row record an interaction" is asked as `action IS NOT NULL`, and a redirect that stored an
// empty string would answer yes to every one of them.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
