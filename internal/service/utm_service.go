package service

import (
	"context"
	"errors"
	"log"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

type UTMTemplateService struct {
	db *gorm.DB
}

func NewUTMTemplateService(db *gorm.DB) *UTMTemplateService {
	return &UTMTemplateService{db: db}
}

func (s *UTMTemplateService) Create(ctx context.Context, userID int64, req domain.CreateUTMTemplateRequest) (*domain.UTMTemplate, error) {
	row := entity.UTMTemplate{
		UserID:      userID,
		Name:        req.Name,
		UTMSource:   req.UTMSource,
		UTMMedium:   req.UTMMedium,
		UTMCampaign: req.UTMCampaign,
		UTMTerm:     req.UTMTerm,
		UTMContent:  req.UTMContent,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		log.Printf("create utm template failed: %v", err)
		return nil, errors.New("failed to create template")
	}
	return &domain.UTMTemplate{
		ID:          row.ID,
		UserID:      row.UserID,
		Name:        row.Name,
		UTMSource:   row.UTMSource,
		UTMMedium:   row.UTMMedium,
		UTMCampaign: row.UTMCampaign,
		UTMTerm:     row.UTMTerm,
		UTMContent:  row.UTMContent,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (s *UTMTemplateService) List(ctx context.Context, userID int64) ([]domain.UTMTemplate, error) {
	var rows []entity.UTMTemplate
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, errors.New("failed to list templates")
	}

	templates := make([]domain.UTMTemplate, 0, len(rows))
	for _, r := range rows {
		templates = append(templates, domain.UTMTemplate{
			ID:          r.ID,
			UserID:      r.UserID,
			Name:        r.Name,
			UTMSource:   r.UTMSource,
			UTMMedium:   r.UTMMedium,
			UTMCampaign: r.UTMCampaign,
			UTMTerm:     r.UTMTerm,
			UTMContent:  r.UTMContent,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		})
	}
	return templates, nil
}

func (s *UTMTemplateService) Delete(ctx context.Context, userID, templateID int64) error {
	res := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", templateID, userID).
		Delete(&entity.UTMTemplate{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("template not found")
	}
	return nil
}
