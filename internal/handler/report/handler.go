// Package report takes complaints about short links from the people who followed one.
//
// The route is public on purpose: the person who needs it is a visitor who was sent somewhere illegal, and
// asking them to sign in first would be asking them to help us with our problem. It is rate limited, and
// what it writes is bounded - see ReportService.Create - because a public write with no ceiling is a way to
// fill a table from one address.
package report

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/middleware"
)

// Reporter files a complaint. An interface for the same reason as the other handlers: the two refusals it
// can return - a reason nobody offers, and a failure to write - are worth reaching without a database.
type Reporter interface {
	Create(ctx context.Context, req domain.CreateReportRequest, ip string) (*domain.AbuseReportInfo, error)
}

type Handler struct {
	svc Reporter
}

func NewHandler(svc Reporter) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup, limitMW gin.HandlerFunc) {
	g := r.Group("/reports")
	if limitMW != nil {
		g.Use(limitMW)
	}
	g.POST("", h.Create)
}

// Create files a report.
func (h *Handler) Create(c *gin.Context) {
	var req domain.CreateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a short code and a reason are required"})
		return
	}

	report, err := h.svc.Create(c.Request.Context(), req, middleware.RealIP(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"report": report})
}
