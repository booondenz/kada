package service

import (
	"context"
	"errors"
	"log"
	"regexp"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,48}[a-z0-9]$`)

type WorkspaceService struct {
	db *gorm.DB
}

func NewWorkspaceService(db *gorm.DB) *WorkspaceService {
	return &WorkspaceService{db: db}
}

// Create creates a workspace
func (s *WorkspaceService) Create(ctx context.Context, userID int64, req domain.CreateWorkspaceRequest) (*domain.Workspace, error) {
	if !slugPattern.MatchString(req.Slug) {
		return nil, errors.New("invalid slug format: only lowercase letters, digits and hyphens are allowed, length 3-50")
	}

	// fast-path check: on failure make no decision, the unique constraint is the final arbiter
	if s.slugTaken(ctx, req.Slug, 0) {
		return nil, errors.New("that slug is already taken")
	}

	row := entity.Workspace{Name: req.Name, Slug: req.Slug, UserID: userID}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, errors.New("failed to create workspace")
	}
	return &domain.Workspace{
		ID:        row.ID,
		Name:      row.Name,
		Slug:      row.Slug,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// List gets the user's workspace list
func (s *WorkspaceService) List(ctx context.Context, userID int64) ([]domain.Workspace, error) {
	var workspaces []domain.Workspace
	err := s.db.WithContext(ctx).
		Table("workspaces AS w").
		Select("w.id, w.name, w.slug, w.user_id, w.created_at, w.updated_at, "+
			"COALESCE((SELECT COUNT(*) FROM links WHERE workspace_id = w.id), 0) AS link_count").
		Where("w.user_id = ?", userID).
		Order("w.created_at DESC").
		Scan(&workspaces).Error
	if err != nil {
		return nil, errors.New("failed to list workspaces")
	}
	if workspaces == nil {
		workspaces = []domain.Workspace{}
	}
	return workspaces, nil
}

// GetByID gets a workspace by ID
func (s *WorkspaceService) GetByID(ctx context.Context, workspaceID, userID int64) (*domain.Workspace, error) {
	var w domain.Workspace
	err := s.db.WithContext(ctx).
		Table("workspaces AS w").
		Select("w.id, w.name, w.slug, w.user_id, w.created_at, w.updated_at, "+
			"COALESCE((SELECT COUNT(*) FROM links WHERE workspace_id = w.id), 0) AS link_count").
		Where("w.id = ? AND w.user_id = ?", workspaceID, userID).
		Take(&w).Error
	if err != nil {
		return nil, errors.New("workspace not found")
	}
	return &w, nil
}

// Update updates a workspace
func (s *WorkspaceService) Update(ctx context.Context, workspaceID, userID int64, req domain.UpdateWorkspaceRequest) (*domain.Workspace, error) {
	if req.Slug != nil {
		if !slugPattern.MatchString(*req.Slug) {
			return nil, errors.New("invalid slug format")
		}
		if s.slugTaken(ctx, *req.Slug, workspaceID) {
			return nil, errors.New("that slug is already taken")
		}
	}

	// COALESCE semantics: only the fields the caller actually sent are written, and updated_at moves.
	updates := map[string]any{"updated_at": gorm.Expr("NOW()")}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Slug != nil {
		updates["slug"] = *req.Slug
	}

	var row entity.Workspace
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&entity.Workspace{}).
			Where("id = ? AND user_id = ?", workspaceID, userID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("id = ? AND user_id = ?", workspaceID, userID).First(&row).Error
	})
	if err != nil {
		return nil, errors.New("workspace not found or access denied")
	}

	return &domain.Workspace{
		ID:        row.ID,
		Name:      row.Name,
		Slug:      row.Slug,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

// Delete deletes a workspace
func (s *WorkspaceService) Delete(ctx context.Context, workspaceID, userID int64) error {
	// unlink associated links first. This runs before the ownership check of the DELETE below, exactly as
	// the original code did, so it stays inside the same transaction to avoid a half-applied delete.
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&entity.Link{}).
			Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
			Update("workspace_id", gorm.Expr("NULL")).Error; err != nil {
			log.Printf("unlink workspace %d failed: %v", workspaceID, err)
			return err
		}
		return tx.Where("id = ? AND user_id = ?", workspaceID, userID).
			Delete(&entity.Workspace{}).Error
	})
	if err != nil {
		return errors.New("failed to delete workspace")
	}
	return nil
}

// slugTaken reports whether another workspace already uses the slug (id=0 means "no workspace to exclude").
func (s *WorkspaceService) slugTaken(ctx context.Context, slug string, exceptID int64) bool {
	q := s.db.WithContext(ctx).Model(&entity.Workspace{}).Where("slug = ?", slug)
	if exceptID != 0 {
		q = q.Where("id != ?", exceptID)
	}
	var count int64
	return q.Count(&count).Error == nil && count > 0
}
