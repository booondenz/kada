package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RealIP returns the real client IP.
// The backend trusts no client-spoofable proxy header (X-Forwarded-For can be forged to bypass rate limiting);
// it accepts only the X-Real-IP rewritten by nginx (set from $remote_addr, which a client cannot forge),
// and falls back to RemoteAddr for direct connections (no nginx).
func RealIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
			return parsed.String()
		}
	}
	return c.ClientIP()
}

// RateLimiter is a Redis sliding-window rate limiting middleware
type RateLimiter struct {
	client *redis.Client
}

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	// Name is which limiter this is. It is part of the Redis key, so two limiters never count into the
	// same bucket - which they used to, and the consequence was that the strictest bucket throttled
	// everything else: 20 requests a minute to any route refused a sign-in.
	Name    string
	Window  time.Duration               // time window
	Limit   int                         // max requests within the window
	KeyFunc func(c *gin.Context) string // custom key generator (defaults to the IP)
}

func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

// rateLimitKey is the key one limiter counts under: the limiter's name, then the subject.
//
// Both halves matter. The name is what makes the buckets independent - they used to share one key, so the
// strictest limit was really a limit on all traffic. The subject is what a limit is usually about, which is
// the client address unless a limiter names something else.
func rateLimitKey(name, subject string) string {
	return "ratelimit:" + name + ":" + subject
}

// Limit returns the Gin middleware
func (rl *RateLimiter) Limit(cfg RateLimitConfig) gin.HandlerFunc {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = defaultKeyFunc
	}

	return func(c *gin.Context) {
		key := rateLimitKey(cfg.Name, cfg.KeyFunc(c))
		now := time.Now().UnixNano()
		windowStart := now - cfg.Window.Nanoseconds()

		// Execute in a batch pipeline
		pipe := rl.client.Pipeline()
		// Remove entries outside the window
		pipe.ZRemRangeByScore(c.Request.Context(), key,
			"0",
			fmt.Sprintf("%d", windowStart))
		// Count requests within the current window
		countCmd := pipe.ZCard(c.Request.Context(), key)
		// Add the current request
		pipe.ZAdd(c.Request.Context(), key, redis.Z{
			Score:  float64(now),
			Member: fmt.Sprintf("%d", now),
		})
		// Set the key expiry
		pipe.Expire(c.Request.Context(), key, cfg.Window*2)

		if _, err := pipe.Exec(c.Request.Context()); err != nil {
			// Fail open when Redis is unavailable
			c.Next()
			return
		}

		count, _ := countCmd.Result()

		// Set rate limit response headers
		remaining := cfg.Limit - int(count) - 1
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if int(count) >= cfg.Limit {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":               "too many requests, please try again later",
				"retry_after_seconds": int(cfg.Window.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Strict is the shared bucket for the session endpoints: `/auth/guest` and the report form. It is the
// strictest tier because it is cheap to call and expensive to serve - every guest call is a row.
func (rl *RateLimiter) Strict() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Name:   LimiterStrict,
		Window: 1 * time.Minute,
		Limit:  20,
	})
}

// Normal normal mode - general API
func (rl *RateLimiter) Normal() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Name:   LimiterAPI,
		Window: 1 * time.Minute,
		Limit:  120,
	})
}

// Redirect redirect mode - short-link redirection (high frequency)
func (rl *RateLimiter) Redirect() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Name:   LimiterRedirect,
		Window: 1 * time.Minute,
		Limit:  300,
	})
}

// Action - guide page interactions, the one endpoint here that only writes analytics.
//
// Tighter than Redirect on purpose, and the damage of getting it wrong is bounded in the safe direction:
// these rows are recorded but never counted (see CONTRIBUTING.md, Part 1 section 7.3), so a limit that drops a report costs
// a line of engagement detail, while what it stops is a script manufacturing activity on somebody else's
// link. It is still far above what one visitor generates, which is one to three reports per page.
func (rl *RateLimiter) Action() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Name:   LimiterAction,
		Window: 1 * time.Minute,
		Limit:  30,
	})
}

func defaultKeyFunc(c *gin.Context) string {
	return RealIP(c)
}

// The names of the buckets. They are constants because they appear in Redis, where a typo is not a compile
// error and a changed name silently starts every client from zero.
const (
	LimiterAPI      = "api"
	LimiterStrict   = "strict"
	LimiterRedirect = "redirect"
	LimiterAction   = "action"
	LimiterGuest    = "guest"
)

// Guest - creating a guest session, the one public route that mints an identity.
//
// Ten a minute from one address covers an office or a carrier NAT opening the site together, and is far
// less than a farm wants when every account it mints can then create links.
func (rl *RateLimiter) Guest() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Name:   LimiterGuest,
		Window: 1 * time.Minute,
		Limit:  10,
	})
}
