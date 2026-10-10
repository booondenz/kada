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
	Window  time.Duration               // time window
	Limit   int                         // max requests within the window
	KeyFunc func(c *gin.Context) string // custom key generator (defaults to the IP)
}

func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

// Limit returns the Gin middleware
func (rl *RateLimiter) Limit(cfg RateLimitConfig) gin.HandlerFunc {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = defaultKeyFunc
	}

	return func(c *gin.Context) {
		key := "ratelimit:" + cfg.KeyFunc(c)
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

// Strict strict mode - auth endpoints (login, register, send verification code)
func (rl *RateLimiter) Strict() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Window: 1 * time.Minute,
		Limit:  20,
	})
}

// Normal normal mode - general API
func (rl *RateLimiter) Normal() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Window: 1 * time.Minute,
		Limit:  120,
	})
}

// Redirect redirect mode - short-link redirection (high frequency)
func (rl *RateLimiter) Redirect() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Window: 1 * time.Minute,
		Limit:  300,
	})
}

// Action - guide page interactions, the one endpoint here that only writes analytics.
//
// Tighter than Redirect on purpose, and the damage of getting it wrong is bounded in the safe direction:
// these rows are recorded but never counted (see docs/design.md 7.3), so a limit that drops a report costs
// a line of engagement detail, while what it stops is a script manufacturing activity on somebody else's
// link. It is still far above what one visitor generates, which is one to three reports per page.
func (rl *RateLimiter) Action() gin.HandlerFunc {
	return rl.Limit(RateLimitConfig{
		Window: 1 * time.Minute,
		Limit:  30,
	})
}

func defaultKeyFunc(c *gin.Context) string {
	return RealIP(c)
}
