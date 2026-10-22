package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
)

// ReportService files complaints about short links and reads them back for the operator.
//
// It is separate from LinkService because it has a different caller: the writer is an anonymous visitor on
// a stopped page, not the owner of anything. Keeping them apart is also what keeps "who may read a report"
// a question with one answer - the operator routes, and nothing else.
type ReportService struct {
	db *gorm.DB
}

func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{db: db}
}

// maxReportDetail bounds what an anonymous writer can put in one row. It is generous for a sentence about
// what a link showed them, and small enough that the table cannot be filled a page at a time.
const maxReportDetail = 2000

// Create stores a report.
//
// A short code that does not resolve is still accepted, with LinkID left null: by the time somebody reports
// a link it may already be gone, and a report about it is the only remaining trace of what it pointed at.
func (s *ReportService) Create(ctx context.Context, req domain.CreateReportRequest, ip string) (*domain.AbuseReportInfo, error) {
	if !slices.Contains(domain.ReportReasons, req.Reason) {
		return nil, errors.New("unknown report reason")
	}

	code := strings.TrimSpace(req.ShortCode)
	if code == "" || len(code) > 20 {
		return nil, errors.New("invalid short code")
	}

	detail := strings.TrimSpace(req.Detail)
	if len(detail) > maxReportDetail {
		detail = detail[:maxReportDetail]
	}

	var linkID *int64
	var row entity.Link
	if err := s.db.WithContext(ctx).Select("id").Where("short_code = ?", code).First(&row).Error; err == nil {
		id := row.ID
		linkID = &id
	}

	var contact *string
	if req.Contact != nil {
		if trimmed := strings.TrimSpace(*req.Contact); trimmed != "" {
			if len(trimmed) > 255 {
				trimmed = trimmed[:255]
			}
			contact = &trimmed
		}
	}

	var reporterIP *string
	if ip != "" {
		reporterIP = &ip
	}

	report := entity.AbuseReport{
		ShortCode: code,
		LinkID:    linkID,
		Reason:    req.Reason,
		Detail:    detail,
		Contact:   contact,
		IP:        reporterIP,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&report).Error; err != nil {
		return nil, errors.New("failed to file the report")
	}

	return &domain.AbuseReportInfo{
		ID:        report.ID,
		ShortCode: report.ShortCode,
		LinkID:    report.LinkID,
		Reason:    report.Reason,
		Detail:    report.Detail,
		Contact:   report.Contact,
		CreatedAt: report.CreatedAt,
	}, nil
}

// List returns the most recent reports, each with whether the link it names is stopped now.
//
// The reporter's IP is deliberately not published here: it is kept for investigating a flood of reports,
// and it is not part of what the operator needs to act on one.
func (s *ReportService) List(ctx context.Context, limit int) ([]domain.AbuseReportInfo, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var rows []struct {
		ID          int64
		ShortCode   string
		LinkID      *int64
		Reason      string
		Detail      string
		Contact     *string
		CreatedAt   time.Time
		LinkBlocked bool
	}
	err := s.db.WithContext(ctx).
		Table("abuse_reports AS r").
		Select("r.id, r.short_code, r.link_id, r.reason, r.detail, r.contact, r.created_at, (l.blocked_at IS NOT NULL) AS link_blocked").
		Joins("LEFT JOIN links l ON l.id = r.link_id").
		Order("r.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, errors.New("failed to read the reports")
	}

	out := make([]domain.AbuseReportInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.AbuseReportInfo{
			ID:          r.ID,
			ShortCode:   r.ShortCode,
			LinkID:      r.LinkID,
			Reason:      r.Reason,
			Detail:      r.Detail,
			Contact:     r.Contact,
			CreatedAt:   r.CreatedAt,
			LinkBlocked: r.LinkBlocked,
		})
	}
	return out, nil
}
