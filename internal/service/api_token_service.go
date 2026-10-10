package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

type APITokenService struct {
	db *gorm.DB
}

func NewAPITokenService(db *gorm.DB) *APITokenService {
	return &APITokenService{db: db}
}

// Create creates an API Token and returns the raw token (this is the only time it is returned)
func (s *APITokenService) Create(ctx context.Context, userID int64, req domain.CreateAPITokenRequest) (*domain.CreateAPITokenResponse, error) {
	// generate a random token
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return nil, errors.New("failed to create API token")
	}
	rawToken := "kada_" + hex.EncodeToString(b)

	// store the SHA256 hash; the raw token is never persisted
	row := entity.APIToken{
		UserID:    userID,
		Name:      req.Name,
		TokenHash: sha256Hex(rawToken),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, errors.New("failed to create API token")
	}

	return &domain.CreateAPITokenResponse{
		Token: rawToken,
		APIToken: domain.APIToken{
			ID:        row.ID,
			UserID:    row.UserID,
			Name:      row.Name,
			LastUsed:  row.LastUsed,
			CreatedAt: row.CreatedAt,
		},
	}, nil
}

// List lists the user's API Tokens
func (s *APITokenService) List(ctx context.Context, userID int64) ([]domain.APIToken, error) {
	var rows []entity.APIToken
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, errors.New("failed to list API tokens")
	}

	// never return nil so the JSON body is [] rather than null
	tokens := make([]domain.APIToken, 0, len(rows))
	for _, r := range rows {
		tokens = append(tokens, domain.APIToken{
			ID:        r.ID,
			UserID:    r.UserID,
			Name:      r.Name,
			LastUsed:  r.LastUsed,
			CreatedAt: r.CreatedAt,
		})
	}
	return tokens, nil
}

// Delete deletes an API Token
func (s *APITokenService) Delete(ctx context.Context, id, userID int64) error {
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&entity.APIToken{}).Error; err != nil {
		return errors.New("failed to delete API token")
	}
	return nil
}

// ValidateToken validates an API Token and returns the userID.
//
// The UPDATE also stamps last_used, so it must stay a single statement: reading the row and then
// updating it would race with a concurrent delete.
func (s *APITokenService) ValidateToken(ctx context.Context, rawToken string) (int64, error) {
	if rawToken == "" || len(rawToken) < 6 || rawToken[:5] != "kada_" {
		return 0, fmt.Errorf("invalid API token format")
	}

	tokenHash := sha256Hex(rawToken)

	var userID int64
	err := s.db.WithContext(ctx).Raw(`
		UPDATE api_tokens SET last_used = NOW()
		WHERE token_hash = ?
		RETURNING user_id
	`, tokenHash).Scan(&userID).Error
	if err != nil || userID == 0 {
		return 0, fmt.Errorf("invalid API token")
	}

	return userID, nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
