// Package auth opens the session the app runs in.
//
// There is one way in and it is not a login: `POST /api/auth/guest` mints an account with an empty identity
// and returns the token that carries its id. The site is free, so there is nothing to identify - phone
// numbers, SMS codes and passwords were removed with the sign-in that needed them.
package auth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/middleware"
)

// AuthService is the auth service interface (mockable for tests).
type AuthService interface {
	CreateGuest(ctx context.Context) (*domain.AuthResponse, error)
	GetUserByID(ctx context.Context, userID int64) (*domain.UserInfo, error)
}

type Handler struct {
	svc AuthService
}

func NewHandler(svc AuthService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the session routes.
//
// The limiters are positional and optional: the first is the shared strict bucket, the second is the tighter
// ceiling on creating an account - which still matters, because every account is an identity that can create
// links even if it cannot be claimed.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc, limiters ...gin.HandlerFunc) {
	public := r.Group("")
	if len(limiters) > 0 && limiters[0] != nil {
		public.Use(limiters[0])
	}

	guest := []gin.HandlerFunc{h.CreateGuest}
	if len(limiters) > 1 && limiters[1] != nil {
		guest = append([]gin.HandlerFunc{limiters[1]}, guest...)
	}
	public.POST("/auth/guest", guest...)

	auth := r.Group("").Use(authMW)
	auth.GET("/me", h.GetMe)
}

// CreateGuest opens the session the app runs in before anyone has done anything.
func (h *Handler) CreateGuest(c *gin.Context) {
	resp, err := h.svc.CreateGuest(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetMe returns the account the token belongs to.
func (h *Handler) GetMe(c *gin.Context) {
	user, err := h.svc.GetUserByID(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}
