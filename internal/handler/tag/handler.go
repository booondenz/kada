package tag

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/middleware"
	"github.com/chun/kada-backend/internal/service"
)

type Handler struct {
	svc *service.TagService
}

func NewHandler(svc *service.TagService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc) {
	auth := r.Group("", authMW)
	auth.POST("/tags", h.Create)
	auth.GET("/tags", h.List)
	auth.DELETE("/tags/:id", h.Delete)
	auth.POST("/links/:id/tags", h.AddTagToLink)
	auth.DELETE("/links/:id/tags/:tag_id", h.RemoveTagFromLink)
}

func (h *Handler) Create(c *gin.Context) {
	var req domain.CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag name is required"})
		return
	}
	t, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"tag": t})
}

func (h *Handler) List(c *gin.Context) {
	tags, err := h.svc.List(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid ID"})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), middleware.GetUserID(c), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "tag deleted"})
}

func (h *Handler) AddTagToLink(c *gin.Context) {
	linkID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link ID"})
		return
	}
	var req struct {
		TagID int64 `json:"tag_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tag ID is required"})
		return
	}
	if err := h.svc.AddTagToLink(c.Request.Context(), middleware.GetUserID(c), linkID, req.TagID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "tag added"})
}

func (h *Handler) RemoveTagFromLink(c *gin.Context) {
	linkID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	tagID, _ := strconv.ParseInt(c.Param("tag_id"), 10, 64)
	if err := h.svc.RemoveTagFromLink(c.Request.Context(), middleware.GetUserID(c), linkID, tagID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "tag removed"})
}
