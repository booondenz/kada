// Package admin holds the routes an operator uses to stop a link and to read what was reported.
//
// It is not a dashboard. The operator is one person on a single-tenant deployment and the job is four
// requests, but they are routes rather than a database client because a takedown has to record who decided
// it and why - a hand-written UPDATE records neither, and that is the record the owner is entitled to.
package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
)

// Moderator is what this handler needs from the links: the three decisions and nothing else.
//
// It is an interface so the refusals - already stopped, not stopped, not found - can be reached from a test
// without a database. The three are the part that decides what an operator sees, and they are the part a
// rewrite is most likely to flatten into a 500.
type Moderator interface {
	Block(ctx context.Context, shortCode, operator, reason, evidence string) (*domain.LinkInfo, error)
	Unblock(ctx context.Context, shortCode, operator, reason string) error
	Blocks(ctx context.Context, shortCode string) ([]domain.LinkBlockInfo, error)
}

// ReportReader lists what visitors reported.
type ReportReader interface {
	List(ctx context.Context, limit int) ([]domain.AbuseReportInfo, error)
}

type Handler struct {
	links   Moderator
	reports ReportReader
}

func NewHandler(links Moderator, reports ReportReader) *Handler {
	return &Handler{links: links, reports: reports}
}

// RegisterRoutes mounts the operator routes under /admin.
//
// Both middlewares may be nil, and a nil one is left out of the chain rather than passed as a no-op: an
// admin limiter that is only skipped in development is the one that gets skipped in production by accident.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW, adminMW gin.HandlerFunc) {
	g := r.Group("/admin")
	if authMW != nil {
		g.Use(authMW)
	}
	if adminMW != nil {
		g.Use(adminMW)
	}

	g.POST("/links/:code/block", h.BlockLink)
	g.POST("/links/:code/unblock", h.UnblockLink)
	g.GET("/links/:code/blocks", h.LinkBlocks)
	g.GET("/reports", h.ListReports)
}

// BlockLink stops a link from being served.
func (h *Handler) BlockLink(c *gin.Context) {
	var req domain.BlockLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a reason is required: " + err.Error()})
		return
	}

	link, err := h.links.Block(c.Request.Context(), c.Param("code"), operatorPhone(c), req.Reason, req.Evidence)
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"link": link})
}

// UnblockLink puts a stopped link back.
func (h *Handler) UnblockLink(c *gin.Context) {
	// The body is optional: restoring a link needs no justification to be valid, and an empty request is
	// the ordinary way to do it.
	var req domain.UnblockLinkRequest
	_ = c.ShouldBindJSON(&req)

	if err := h.links.Unblock(c.Request.Context(), c.Param("code"), operatorPhone(c), req.Reason); err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "link restored"})
}

// LinkBlocks returns the takedown history of one link.
func (h *Handler) LinkBlocks(c *gin.Context) {
	blocks, err := h.links.Blocks(c.Request.Context(), c.Param("code"))
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"blocks": blocks})
}

// ListReports returns the most recent reports, newest first.
func (h *Handler) ListReports(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	reports, err := h.reports.List(c.Request.Context(), limit)
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reports": reports})
}

// operatorPhone is who the decision is attributed to. There are no accounts to name any more, so the record
// says that an operator made it - which is the truth, and the history is still complete in every other
// respect (when, why, on what evidence).
func operatorPhone(c *gin.Context) string {
	return "operator"
}

// respond maps the three refusals the operator can hit. A conflict is not an error in the request: it says
// the link is already in the state the caller wanted, which is worth telling them apart from "not found".
func respond(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrLinkNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrLinkAlreadyBlocked), errors.Is(err, domain.ErrLinkNotBlocked):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
