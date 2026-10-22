// Package upload takes files for file links, and serves them back.
//
// The two routes have different audiences for that reason: `POST /api/uploads` is somebody with an account
// who is creating a link, and `GET /f/:id` is a visitor following one, who has no session and never will.
// The public one is served from this domain because that is the whole point - a "short link" that pointed at
// somebody else's image host is what this replaces.
package upload

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/booondenz/kada/internal/domain"
	"github.com/booondenz/kada/internal/middleware"
	"github.com/booondenz/kada/internal/service"
)

// maxMultipartMemory is how much of a multipart body gin keeps in memory before spilling to disk.
const maxMultipartMemory = 8 << 20

// hardBodyLimit is the ceiling the request body is read through. It is the per-file limit plus room for the
// multipart envelope, so an oversized upload is refused while it is being read rather than after it has
// been buffered.
const hardBodyLimit = (20 << 20) + (1 << 20)

type Handler struct {
	svc *service.UploadService
}

func NewHandler(svc *service.UploadService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r *gin.Engine, authMW gin.HandlerFunc) {
	// The public route is mounted at the root, next to /r/, because it is part of what a short link does.
	r.GET("/f/:id", h.Serve)

	api := r.Group("/api")
	if authMW != nil {
		api.Use(authMW)
	}
	api.POST("/uploads", h.Create)
}

// Create stores one file and returns where it is served from.
func (h *Handler) Create(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, hardBodyLimit)
	if err := c.Request.ParseMultipartForm(maxMultipartMemory); err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "the upload is too large"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a file is required"})
		return
	}
	defer func() { _ = file.Close() }()

	upload, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), header.Filename, file)
	if err != nil {
		respondUploadError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"upload": upload})
}

// Serve returns a stored file.
//
// The id is unguessable and the contents never change, so the response is cached for a year as immutable;
// `?download=1` is the only thing that varies, and it only changes the disposition header.
func (h *Handler) Serve(c *gin.Context) {
	row, file, err := h.svc.Open(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, domain.ErrLinkNotFound) {
			c.String(http.StatusNotFound, "file not found")
			return
		}
		c.String(http.StatusInternalServerError, "file unavailable")
		return
	}
	defer func() { _ = file.Close() }()

	c.Header("Content-Type", row.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Disposition", disposition(row.ContentType, row.Filename, c.Query("download") == "1"))
	c.Header("ETag", `"`+row.SHA256+`"`)

	http.ServeContent(c.Writer, c.Request, row.Filename, row.CreatedAt, file)
}

// disposition decides whether the browser shows the file or saves it.
//
// Images and PDFs are shown: the first has to be visible to be saved by hand, and the second is what a PDF
// viewer in the browser is for. Everything else - the Office formats - is offered as a download, because a
// browser cannot render one and a page that renders nothing looks broken.
func disposition(contentType, filename string, download bool) string {
	kind := "attachment"
	if !download && (strings.HasPrefix(contentType, "image/") || contentType == "application/pdf") {
		kind = "inline"
	}
	// The filename is quoted and stripped of the characters that would end the header value early.
	safe := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, filename)
	return kind + `; filename="` + safe + `"`
}

// respondUploadError maps the two refusals the service can raise.
func respondUploadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrUnsupportedFile):
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrStorageQuota):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store the file"})
	}
}
