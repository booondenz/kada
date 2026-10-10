package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain/entity"
)

// Overview is a user's totals: how many links they have and how many clicks those links have taken.
type Overview struct {
	TotalLinks  int64 `json:"total_links"`
	TotalClicks int64 `json:"total_clicks"`
}

// AnalyticsService answers the aggregate questions about a user's links.
type AnalyticsService struct {
	db *gorm.DB
}

// NewAnalyticsService builds the analytics service on an open database handle.
func NewAnalyticsService(db *gorm.DB) *AnalyticsService {
	return &AnalyticsService{db: db}
}

// Overview sums up one user's links.
//
// The query lives here rather than in the HTTP handler because there are two callers now: the dashboard
// endpoint and the assistant's "how many links do I have" tool. Two copies of the same SUM drift apart,
// and then the assistant answers a question the dashboard contradicts.
//
// Ownership is the WHERE clause, so another user's links are not merely filtered out afterwards - they are
// never read.
func (s *AnalyticsService) Overview(ctx context.Context, userID int64) (Overview, error) {
	var totals Overview
	err := s.db.WithContext(ctx).
		Model(&entity.Link{}).
		Select("COUNT(*) AS total_links, COALESCE(SUM(click_count), 0) AS total_clicks").
		Where("user_id = ?", userID).
		Scan(&totals).Error
	if err != nil {
		return Overview{}, fmt.Errorf("failed to total the links of user %d: %w", userID, err)
	}
	return totals, nil
}
