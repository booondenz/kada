package link

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/infra/preview"
	"github.com/booondenz/kada/internal/middleware"
	"github.com/booondenz/kada/internal/service"
)

type Handler struct {
	svc     *service.LinkService
	fetcher *preview.Fetcher
}

func NewHandler(svc *service.LinkService) *Handler {
	return &Handler{svc: svc, fetcher: preview.NewFetcher()}
}

// respondLinkError maps the service's refusals onto status codes.
//
// The fallback is the caller's to choose, because the same error means different things on the two paths: a
// create that failed is a server fault, while a failed edit is usually a link that is not there. What is not
// the caller's to choose is the two refusals that are neither - a target in a blocked category is a bad
// request, and a ceiling that has been reached is a 429 with a retry-after, not a 500 telling the client to
// try the one request that cannot succeed.
func respondLinkError(c *gin.Context, err error, fallback int) {
	var limited *domain.RateLimitError
	if errors.As(err, &limited) {
		if limited.RetryAfterSeconds > 0 {
			c.Header("Retry-After", strconv.Itoa(limited.RetryAfterSeconds))
		}
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":               limited.Message,
			"retry_after_seconds": limited.RetryAfterSeconds,
		})
		return
	}

	var blocked *domain.BlockedTargetError
	if errors.As(err, &blocked) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(fallback, gin.H{"error": err.Error()})
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc) {
	r.Use(authMW)
	r.POST("/links/preview", h.Preview)
	r.POST("/links", h.Create)
	r.GET("/links", h.List)
	r.POST("/links/batch-delete", h.BatchDelete)
	r.POST("/links/batch-tag", h.BatchTag)
	r.GET("/links/export", h.ExportCSV)
	r.GET("/links/:id", h.Get)
	r.PATCH("/links/:id", h.Update)
	r.DELETE("/links/:id", h.Delete)
	r.GET("/links/:id/targets", h.Targets)
	r.PUT("/links/:id/targets", h.ReplaceTargets)
}

// ReplaceTargets replaces a link's pool of destinations.
//
// The whole pool arrives in one request and replaces what is there: the pool is a decision ("these three, in
// this proportion"), and an edit that lands one row at a time has no moment at which it can be recorded as a
// decision. It is the only write path that touches targets, so a pool cannot be half-edited.
func (h *Handler) ReplaceTargets(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}

	var req domain.ReplaceTargetsRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameters"})
		return
	}

	link, err := h.svc.ReplaceTargets(c.Request.Context(), id, middleware.GetUserID(c), operatorPhone(c), req.Targets)
	if err != nil {
		respondLinkError(c, err, http.StatusBadRequest)
		return
	}

	c.JSON(http.StatusOK, gin.H{"link": link})
}

// Targets lists a link's pool with the clicks each target took.
func (h *Handler) Targets(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}

	targets, err := h.svc.Targets(c.Request.Context(), id, middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"targets": targets})
}

// operatorPhone is who a change is attributed to. An API token carries no number, and the audit says so
// rather than guessing.
func operatorPhone(c *gin.Context) string {
	if phone := middleware.GetUserPhone(c); phone != nil {
		return *phone
	}
	return ""
}

// Create creates a short link.
func (h *Handler) Create(c *gin.Context) {
	var req domain.CreateLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid link details are required: " + err.Error()})
		return
	}

	link, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), req)
	if err != nil {
		respondLinkError(c, err, http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"link": link})
}

// Get returns a single link.
func (h *Handler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}

	link, err := h.svc.GetByID(c.Request.Context(), id, middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "link not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"link": link})
}

// List returns a paginated list of links.
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	folderID, _ := strconv.ParseInt(c.Query("folder_id"), 10, 64)
	tagID, _ := strconv.ParseInt(c.Query("tag_id"), 10, 64)
	sort := c.DefaultQuery("sort", "created_desc")

	result, err := h.svc.List(c.Request.Context(), middleware.GetUserID(c), page, pageSize, search, folderID, tagID, sort)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"links":       result.Links,
		"total_count": result.TotalCount,
		"page":        result.Page,
		"page_size":   result.PageSize,
	})
}

// Update updates a link.
func (h *Handler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}

	var req domain.UpdateLinkRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request parameters"})
		return
	}

	link, err := h.svc.Update(c.Request.Context(), id, middleware.GetUserID(c), req)
	if err != nil {
		respondLinkError(c, err, http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, gin.H{"link": link})
}

// Delete deletes a link.
func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id, middleware.GetUserID(c)); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "link deleted"})
}

// BatchDelete deletes links in bulk.
func (h *Handler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []int64 `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "link ID list is required"})
		return
	}

	count, err := h.svc.BatchDelete(c.Request.Context(), req.IDs, middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": count})
}

// BatchTag applies a tag to links in bulk.
func (h *Handler) BatchTag(c *gin.Context) {
	var req struct {
		IDs   []int64 `json:"ids" binding:"required"`
		TagID int64   `json:"tag_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "link ID and tag ID are required"})
		return
	}

	if err := h.svc.BatchTag(c.Request.Context(), req.IDs, req.TagID, middleware.GetUserID(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "tags applied successfully"})
}

// ExportCSV exports link data as CSV.
func (h *Handler) ExportCSV(c *gin.Context) {
	csvStr, err := h.svc.ExportCSV(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="kada-links.csv"`)
	c.String(http.StatusOK, csvStr)
}

// Preview fetches the target URL's OG metadata (title, description, image).
func (h *Handler) Preview(c *gin.Context) {
	var req domain.LinkPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid URL is required"})
		return
	}

	preview, err := h.fetcher.Fetch(c.Request.Context(), req.URL)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"preview": domain.LinkPreview{
				Title: req.URL,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"preview": preview})
}
