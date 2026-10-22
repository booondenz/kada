package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
)

type mockAuthService struct {
	createGuest func(ctx context.Context) (*domain.AuthResponse, error)
	getUserByID func(ctx context.Context, id int64) (*domain.UserInfo, error)
}

func (m *mockAuthService) CreateGuest(ctx context.Context) (*domain.AuthResponse, error) {
	if m.createGuest != nil {
		return m.createGuest(ctx)
	}
	return &domain.AuthResponse{Token: "guest-token", User: domain.UserInfo{ID: 1}}, nil
}

func (m *mockAuthService) GetUserByID(ctx context.Context, id int64) (*domain.UserInfo, error) {
	if m.getUserByID != nil {
		return m.getUserByID(ctx, id)
	}
	return &domain.UserInfo{ID: id}, nil
}

func testRouter(svc AuthService) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	auth := func(c *gin.Context) {
		c.Set("user_id", int64(7))
		c.Next()
	}
	NewHandler(svc).RegisterRoutes(r.Group("/api"), auth)
	return r
}

// The only way in: no body, no credentials, and an answer that carries a token.
func TestCreateGuest_ReturnsASession(t *testing.T) {
	w := httptest.NewRecorder()
	testRouter(&mockAuthService{}).ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/guest", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"token":"guest-token"`) {
		t.Errorf("body = %s, want a token", body)
	}
}

func TestCreateGuest_ReportsAFailedSession(t *testing.T) {
	svc := &mockAuthService{createGuest: func(context.Context) (*domain.AuthResponse, error) {
		return nil, errors.New("could not open a session")
	}}

	w := httptest.NewRecorder()
	testRouter(svc).ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/guest", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestGetMe_ReturnsTheAccountTheTokenNames(t *testing.T) {
	w := httptest.NewRecorder()
	testRouter(&mockAuthService{}).ServeHTTP(w, httptest.NewRequest("GET", "/api/me", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"id":7`) {
		t.Errorf("body = %s, want the id the middleware set", body)
	}
}
