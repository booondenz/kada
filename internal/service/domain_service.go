package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/domain/entity"
)

// txtPrefix is the DNS TXT record prefix for domain ownership verification
const txtPrefix = "kada-verify="

// verificationCode computes the domain verification code: first 16 hex chars of sha256(userID:domainID:name)
func verificationCode(userID, domainID int64, name string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", userID, domainID, name)))
	return hex.EncodeToString(h[:])[:16]
}

// expectedTXT is the TXT record value the user is expected to configure in DNS
func expectedTXT(userID, domainID int64, name string) string {
	return txtPrefix + verificationCode(userID, domainID, name)
}

type DomainService struct {
	db *gorm.DB
}

func NewDomainService(db *gorm.DB) *DomainService {
	return &DomainService{db: db}
}

// Create adds a custom domain
func (s *DomainService) Create(ctx context.Context, userID int64, req domain.CreateDomainRequest) (*domain.Domain, error) {
	row := entity.Domain{UserID: userID, Name: req.Name}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, errors.New("failed to add domain, it may already exist")
	}
	d := domain.Domain{
		ID:         row.ID,
		UserID:     row.UserID,
		Name:       row.Name,
		Verified:   row.Verified,
		VerifiedAt: row.VerifiedAt,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
	d.VerificationCode = verificationCode(userID, d.ID, d.Name)
	return &d, nil
}

// List gets the user's domain list
func (s *DomainService) List(ctx context.Context, userID int64) ([]domain.Domain, error) {
	var rows []entity.Domain
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, errors.New("failed to list domains")
	}

	domains := make([]domain.Domain, 0, len(rows))
	for _, r := range rows {
		d := domain.Domain{
			ID:         r.ID,
			UserID:     r.UserID,
			Name:       r.Name,
			Verified:   r.Verified,
			VerifiedAt: r.VerifiedAt,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
		}
		if !d.Verified {
			d.VerificationCode = verificationCode(d.UserID, d.ID, d.Name)
		}
		domains = append(domains, d)
	}
	return domains, nil
}

// Verify verifies domain ownership by checking the DNS TXT record
func (s *DomainService) Verify(ctx context.Context, userID, domainID int64) (*domain.Domain, error) {
	var row entity.Domain
	if err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", domainID, userID).
		First(&row).Error; err != nil {
		return nil, errors.New("domain not found")
	}

	want := expectedTXT(userID, domainID, row.Name)

	// DNS TXT lookup (8-second timeout)
	dnsCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	records, err := net.DefaultResolver.LookupTXT(dnsCtx, row.Name)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed (%s), please make sure the domain resolves correctly", row.Name)
	}
	found := false
	for _, r := range records {
		if strings.TrimSpace(r) == want {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("verification record not found, please add a TXT record for domain %s in DNS with value: %s", row.Name, want)
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&entity.Domain{}).
			Where("id = ? AND user_id = ?", domainID, userID).
			Updates(map[string]any{"verified": true, "verified_at": now, "updated_at": gorm.Expr("NOW()")})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Where("id = ? AND user_id = ?", domainID, userID).First(&row).Error
	}); err != nil {
		return nil, errors.New("verification failed")
	}

	return &domain.Domain{
		ID:               row.ID,
		UserID:           row.UserID,
		Name:             row.Name,
		Verified:         row.Verified,
		VerifiedAt:       row.VerifiedAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		VerificationCode: verificationCode(row.UserID, row.ID, row.Name),
	}, nil
}

// Delete deletes a domain
func (s *DomainService) Delete(ctx context.Context, userID, domainID int64) error {
	res := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", domainID, userID).
		Delete(&entity.Domain{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("domain not found")
	}
	return nil
}
