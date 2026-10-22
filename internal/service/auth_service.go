package service

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/domain/entity"
	"github.com/booondenz/kada/internal/middleware"
)

// AuthService opens sessions.
//
// It used to do more: phone numbers, SMS codes, captchas, passwords and claiming a guest account. The site is
// free now, so there is nothing to sign in to - every visitor gets the same kind of account, and this service
// only has to mint the token that carries its id.
type AuthService struct {
	db        *gorm.DB
	jwtSecret string
	jwtExpire time.Duration
}

func NewAuthService(db *gorm.DB, jwtSecret, jwtExpire string) *AuthService {
	d, _ := time.ParseDuration(jwtExpire)
	return &AuthService{db: db, jwtSecret: jwtSecret, jwtExpire: d}
}

// CreateGuest opens a session for a visitor.
//
// The row it writes has an empty identity, and it always will: there is no way to put a phone number or a
// password on it any more. The id is what makes an account's links its own - it is not a login, it is the
// scope every row is written under.
//
// A browser that loses its token gets a new account with no way back to the old one's links. That is the
// trade a free product makes: the alternative is asking for a phone number.
func (s *AuthService) CreateGuest(ctx context.Context) (*domain.AuthResponse, error) {
	row := &entity.User{}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		log.Printf("create guest user failed: %v", err)
		return nil, errors.New("could not open a session, please try again")
	}

	return s.startSession(ctx, toUserInfo(*row))
}

// GetUserByID gets user info
func (s *AuthService) GetUserByID(ctx context.Context, userID int64) (*domain.UserInfo, error) {
	var row entity.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&row).Error; err != nil {
		return nil, errors.New("user not found")
	}
	return toUserInfo(row), nil
}

// startSession records the session and issues the token.
func (s *AuthService) startSession(ctx context.Context, user *domain.UserInfo) (*domain.AuthResponse, error) {
	if err := s.db.WithContext(ctx).Model(&entity.User{}).
		Where("id = ?", user.ID).
		Update("last_login_at", time.Now()).Error; err != nil {
		log.Printf("update last login for user %d failed: %v", user.ID, err)
	}

	token, err := s.generateToken(*user)
	if err != nil {
		return nil, err
	}

	return &domain.AuthResponse{Token: token, User: *user}, nil
}

// toUserInfo maps the persistence model onto the API model.
func toUserInfo(row entity.User) *domain.UserInfo {
	return &domain.UserInfo{ID: row.ID, Phone: row.Phone}
}

// generateToken generates the JWT that carries the account id.
func (s *AuthService) generateToken(user domain.UserInfo) (string, error) {
	claims := middleware.Claims{
		UserID: user.ID,
		Phone:  user.Phone,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kada",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}
