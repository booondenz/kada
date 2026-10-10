package service

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

const defaultTagColor = "#6366F1"

type TagService struct {
	db *gorm.DB
}

func NewTagService(db *gorm.DB) *TagService {
	return &TagService{db: db}
}

func (s *TagService) Create(ctx context.Context, userID int64, req domain.CreateTagRequest) (*domain.Tag, error) {
	color := defaultTagColor
	if req.Color != nil && *req.Color != "" {
		color = *req.Color
	}
	row := entity.Tag{UserID: userID, Name: req.Name, Color: color}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, errors.New("failed to create tag")
	}
	return &domain.Tag{
		ID:        row.ID,
		UserID:    row.UserID,
		Name:      row.Name,
		Color:     row.Color,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (s *TagService) List(ctx context.Context, userID int64) ([]domain.Tag, error) {
	var rows []entity.Tag
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("name").
		Find(&rows).Error; err != nil {
		return nil, errors.New("failed to list tags")
	}

	tags := make([]domain.Tag, 0, len(rows))
	for _, r := range rows {
		tags = append(tags, domain.Tag{
			ID:        r.ID,
			UserID:    r.UserID,
			Name:      r.Name,
			Color:     r.Color,
			CreatedAt: r.CreatedAt,
		})
	}
	return tags, nil
}

func (s *TagService) Delete(ctx context.Context, userID, tagID int64) error {
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", tagID, userID).
		Delete(&entity.Tag{}).Error; err != nil {
		return errors.New("failed to delete tag")
	}
	return nil
}

// AddTagToLink adds a tag to a link
func (s *TagService) AddTagToLink(ctx context.Context, userID, linkID, tagID int64) error {
	// verify the link belongs to the user
	if !s.ownsLink(ctx, userID, linkID) {
		return errors.New("link not found or access denied")
	}
	// verify the tag also belongs to that user (previously another user's tag could be attached, leaking its name/color)
	if !s.ownsTag(ctx, userID, tagID) {
		return errors.New("tag not found or access denied")
	}

	// Clauses(clause.OnConflict{DoNothing: true}) is the ORM equivalent of ON CONFLICT DO NOTHING, so
	// re-adding an existing tag stays a no-op instead of failing the unique constraint.
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&entity.LinkTag{LinkID: linkID, TagID: tagID}).Error
}

// RemoveTagFromLink removes a link tag
func (s *TagService) RemoveTagFromLink(ctx context.Context, userID, linkID, tagID int64) error {
	// The ownership check stays inside the DELETE: the original SQL filtered on
	// link_id IN (SELECT id FROM links WHERE user_id = $3), which is race-free.
	return s.db.WithContext(ctx).
		Where("link_id = ? AND tag_id = ?", linkID, tagID).
		Where("link_id IN (?)", s.db.WithContext(ctx).Model(&entity.Link{}).Select("id").Where("user_id = ?", userID)).
		Delete(&entity.LinkTag{}).Error
}

func (s *TagService) ownsLink(ctx context.Context, userID, linkID int64) bool {
	var count int64
	err := s.db.WithContext(ctx).Model(&entity.Link{}).
		Where("id = ? AND user_id = ?", linkID, userID).
		Count(&count).Error
	return err == nil && count > 0
}

func (s *TagService) ownsTag(ctx context.Context, userID, tagID int64) bool {
	var count int64
	err := s.db.WithContext(ctx).Model(&entity.Tag{}).
		Where("id = ? AND user_id = ?", tagID, userID).
		Count(&count).Error
	return err == nil && count > 0
}
