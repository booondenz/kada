package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

type FolderService struct {
	db *gorm.DB
}

func NewFolderService(db *gorm.DB) *FolderService {
	return &FolderService{db: db}
}

func (s *FolderService) Create(ctx context.Context, userID int64, req domain.CreateFolderRequest) (*domain.Folder, error) {
	row := entity.Folder{UserID: userID, Name: req.Name}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, errors.New("failed to create folder")
	}
	return &domain.Folder{
		ID:        row.ID,
		UserID:    row.UserID,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *FolderService) List(ctx context.Context, userID int64) ([]domain.Folder, error) {
	var folders []domain.Folder
	// LEFT JOIN + GROUP BY keeps folders without links in the result (link_count = 0).
	err := s.db.WithContext(ctx).
		Table("folders AS f").
		Select("f.id, f.user_id, f.name, f.created_at, f.updated_at, COUNT(l.id) AS link_count").
		Joins("LEFT JOIN links l ON f.id = l.folder_id").
		Where("f.user_id = ?", userID).
		Group("f.id").
		Order("f.name").
		Scan(&folders).Error
	if err != nil {
		return nil, errors.New("failed to list folders")
	}
	if folders == nil {
		folders = []domain.Folder{}
	}
	return folders, nil
}

func (s *FolderService) Update(ctx context.Context, userID, folderID int64, name string) (*domain.Folder, error) {
	// GORM does not generate RETURNING for Update, so run the UPDATE and the read-back inside one
	// transaction: the original code did both in a single statement and must keep the same guarantees
	// (the caller gets the persisted row, and a concurrent delete cannot leave a stale answer).
	var row entity.Folder
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&entity.Folder{}).
			Where("id = ? AND user_id = ?", folderID, userID).
			Update("name", name)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("id = ? AND user_id = ?", folderID, userID).First(&row).Error
	})
	if err != nil {
		return nil, errors.New("folder not found or access denied")
	}
	return &domain.Folder{
		ID:        row.ID,
		UserID:    row.UserID,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (s *FolderService) Delete(ctx context.Context, userID, folderID int64) error {
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", folderID, userID).
		Delete(&entity.Folder{}).Error; err != nil {
		return errors.New("failed to delete folder")
	}
	return nil
}
