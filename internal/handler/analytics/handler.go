package analytics

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/chun/kada-backend/internal/domain"
	"github.com/chun/kada-backend/internal/middleware"
	"github.com/chun/kada-backend/internal/service"
)

// OverviewService is the slice of the analytics service this handler needs.
type OverviewService interface {
	Overview(ctx context.Context, userID int64) (service.Overview, error)
}

type Handler struct {
	db        *gorm.DB
	analytics OverviewService
}

// NewHandler takes both the database and the analytics service: the overview moved into the service so the
// assistant's tool and this endpoint read the same query, while the four breakdown endpoints below still
// query here. They are the same candidate for the same extraction, and none of them is needed by the
// assistant yet.
func NewHandler(db *gorm.DB, analytics OverviewService) *Handler {
	return &Handler{db: db, analytics: analytics}
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup, authMW gin.HandlerFunc) {
	auth := r.Group("", authMW)
	auth.GET("/analytics/overview", h.Overview)
	auth.GET("/analytics/platforms", h.Platforms)
	auth.GET("/analytics/daily", h.DailyClicks)
	auth.GET("/analytics/events", h.Events)
	auth.GET("/analytics/customers", h.Customers)
}

// Overview returns the statistics overview.
func (h *Handler) Overview(c *gin.Context) {
	userID := middleware.GetUserID(c)

	totals, err := h.analytics.Overview(c.Request.Context(), userID)
	if err != nil {
		log.Printf("analytics overview failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total_links":  totals.TotalLinks,
		"total_clicks": totals.TotalClicks,
	})
}

// Platforms returns the distribution of platform sources.
//
// Only visits are counted. Without the filter this breakdown answers "which app fetched my link", which is
// not the question: a link shared in a WeChat group is fetched by WeChat's preview card builder before
// anyone taps it, and that fetch wears the same User-Agent as the in-app browser.
func (h *Handler) Platforms(c *gin.Context) {
	userID := middleware.GetUserID(c)
	linkID, _ := strconv.ParseInt(c.Query("link_id"), 10, 64)

	type PlatformStat struct {
		Platform string `json:"platform"`
		Count    int64  `json:"count"`
	}

	query := h.db.WithContext(c.Request.Context()).
		Table("click_logs AS cl").
		Select("COALESCE(cl.platform, 'browser') AS platform, COUNT(*) AS count").
		Joins("JOIN links l ON cl.link_id = l.id").
		Where("l.user_id = ?", userID).
		Where("cl.kind = ?", string(domain.ClickVisit))
	if linkID > 0 {
		query = query.Where("cl.link_id = ?", linkID)
	}

	var stats []PlatformStat
	if err := query.Group("cl.platform").Order("COUNT(*) DESC").Scan(&stats).Error; err != nil {
		log.Printf("analytics platforms failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if stats == nil {
		stats = []PlatformStat{}
	}

	c.JSON(http.StatusOK, gin.H{"platforms": stats})
}

// DailyClicks returns daily click counts for the last 30 days.
//
// Visits only, for the same reason as the platform breakdown: a prefetch is a request, not a visit, and a
// chart of requests would show a spike every time a link was pasted into a group chat.
func (h *Handler) DailyClicks(c *gin.Context) {
	userID := middleware.GetUserID(c)
	linkID, _ := strconv.ParseInt(c.Query("link_id"), 10, 64)

	// date is returned as text: PostgreSQL would hand back a time.Time for DATE(), and formatting it in
	// SQL keeps the JSON shape stable across drivers.
	type DailyStat struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}

	query := h.db.WithContext(c.Request.Context()).
		Table("click_logs AS cl").
		Select("TO_CHAR(DATE(cl.created_at), 'YYYY-MM-DD') AS date, COUNT(*) AS count").
		Joins("JOIN links l ON cl.link_id = l.id").
		Where("l.user_id = ? AND cl.created_at > NOW() - INTERVAL '30 days'", userID).
		Where("cl.kind = ?", string(domain.ClickVisit))
	if linkID > 0 {
		query = query.Where("cl.link_id = ?", linkID)
	}

	var stats []DailyStat
	if err := query.Group("DATE(cl.created_at)").Order("DATE(cl.created_at)").Scan(&stats).Error; err != nil {
		log.Printf("analytics daily failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if stats == nil {
		stats = []DailyStat{}
	}

	c.JSON(http.StatusOK, gin.H{"daily": stats})
}

// Events returns the list of click events.
//
// This is the one endpoint that returns the rows the counters leave out, and it labels each of them, which
// is what keeps the amount of prefetch measurable: /analytics/events?kind=request is every request the
// link took that no person confirmed. The default is still every row - the list is a log of what happened
// to the link - so a caller that wants the counters' view asks for ?kind=visit.
//
// An unknown kind is refused rather than silently returning nothing, because an empty list is also what a
// working filter looks like.
func (h *Handler) Events(c *gin.Context) {
	userID := middleware.GetUserID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// The filters are shared by the COUNT and the page query, so build them once. Every value is passed as
	// a bound parameter, so the kind cannot reach the SQL as text.
	filters := []string{"l.user_id = ?"}
	args := []any{userID}

	if kind := c.Query("kind"); kind != "" {
		switch domain.ClickKind(kind) {
		case domain.ClickVisit, domain.ClickRequest, domain.ClickAction:
			filters = append(filters, "cl.kind = ?")
			args = append(args, kind)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown kind"})
			return
		}
	}
	where := strings.Join(filters, " AND ")

	var total int64
	if err := h.db.WithContext(c.Request.Context()).
		Table("click_logs AS cl").
		Joins("JOIN links l ON cl.link_id = l.id").
		Where(where, args...).
		Count(&total).Error; err != nil {
		log.Printf("events count failed: %v", err)
		total = 0
	}

	type Event struct {
		ID          int64     `json:"id"`
		LinkID      int64     `json:"link_id"`
		ShortCode   string    `json:"short_code"`
		OriginalURL string    `json:"original_url"`
		Platform    *string   `json:"platform"`
		Kind        string    `json:"kind"`
		Action      *string   `json:"action"`
		IP          *string   `json:"ip"`
		Referer     *string   `json:"referer"`
		CreatedAt   time.Time `json:"created_at"`
	}

	var events []Event
	if err := h.db.WithContext(c.Request.Context()).
		Table("click_logs AS cl").
		Select("cl.id, cl.link_id, l.short_code, l.original_url, cl.platform, cl.kind, cl.action, cl.ip, cl.referer, cl.created_at").
		Joins("JOIN links l ON cl.link_id = l.id").
		Where(where, args...).
		Order("cl.created_at DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Scan(&events).Error; err != nil {
		log.Printf("events query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if events == nil {
		events = []Event{}
	}

	c.JSON(http.StatusOK, gin.H{
		"events":    events,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Customers returns the list of unique visitors.
//
// A visit per row, not a request: otherwise the same person appears twice for a link, once as the
// prefetch and once as the tap, and a group chat's worth of preview fetches shows up as an audience.
func (h *Handler) Customers(c *gin.Context) {
	userID := middleware.GetUserID(c)

	type Customer struct {
		IP          string    `json:"ip"`
		ClickCount  int64     `json:"click_count"`
		UniqueLinks int64     `json:"unique_links"`
		LastSeen    time.Time `json:"last_seen"`
	}

	var customers []Customer
	if err := h.db.WithContext(c.Request.Context()).
		Table("click_logs AS cl").
		Select("cl.ip AS ip, COUNT(*) AS click_count, MAX(cl.created_at) AS last_seen, COUNT(DISTINCT cl.link_id) AS unique_links").
		Joins("JOIN links l ON cl.link_id = l.id").
		Where("l.user_id = ? AND cl.ip IS NOT NULL AND cl.ip != ''", userID).
		Where("cl.kind = ?", string(domain.ClickVisit)).
		Group("cl.ip").
		Order("click_count DESC").
		Limit(50).
		Scan(&customers).Error; err != nil {
		log.Printf("customers query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if customers == nil {
		customers = []Customer{}
	}

	c.JSON(http.StatusOK, gin.H{"customers": customers})
}
