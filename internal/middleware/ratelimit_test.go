package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Each limiter has to count in a bucket of its own. They used to share one key, which made the strictest
// limit - twenty requests a minute on the auth routes - a limit on every request from that address: twenty
// dashboard reads and the sign-in was refused, for a minute.
func TestRateLimitKey_SeparatesTheBuckets(t *testing.T) {
	const subject = "203.0.113.9"

	names := []string{LimiterAPI, LimiterStrict, LimiterRedirect, LimiterAction, LimiterGuest}
	seen := make(map[string]string, len(names))
	for _, name := range names {
		key := rateLimitKey(name, subject)
		if other, clash := seen[key]; clash {
			t.Errorf("limiters %q and %q share the key %q", other, name, key)
		}
		seen[key] = name
		if !strings.Contains(key, subject) {
			t.Errorf("key %q does not name the subject", key)
		}
	}
}

func TestDefaultKeyFunc(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/test", nil)
	c.Request.RemoteAddr = "192.168.1.100:54321"

	key := defaultKeyFunc(c)
	if key != "192.168.1.100" {
		t.Errorf("expected client IP '192.168.1.100', got %q", key)
	}
}

// Must not panic even when a nil client is passed, and must keep the nil client rather than substituting
// one. The field is read directly: a separate `if rl == nil` guard is redundant - the line below already
// dereferences rl, so a constructor that returned nil would fail this test either way - and staticcheck
// reads that guard as evidence that the dereference can happen on nil (SA5011).
func TestNewRateLimiter_NilClient(t *testing.T) {
	rl := NewRateLimiter(nil)
	if rl.client != nil {
		t.Error("client should be nil")
	}
}

func TestRealIP_PrefersXRealIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	_ = r.SetTrustedProxies(nil) // same as production: trust no proxy headers
	var got string
	r.GET("/test", func(c *gin.Context) { got = RealIP(c) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // forged value
	req.Header.Set("X-Real-IP", "5.6.7.8")       // nginx-rewritten value
	r.ServeHTTP(w, req)

	if got != "5.6.7.8" {
		t.Errorf("expected X-Real-IP '5.6.7.8', got %q", got)
	}
}

func TestRealIP_IgnoresSpoofedXFF(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	_ = r.SetTrustedProxies(nil) // same as production: trust no proxy headers
	var got string
	r.GET("/test", func(c *gin.Context) { got = RealIP(c) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4") // only a forged XFF
	r.ServeHTTP(w, req)

	if got != "203.0.113.9" {
		t.Errorf("expected RemoteAddr '203.0.113.9' (spoofed XFF ignored), got %q", got)
	}
}

func TestRealIP_InvalidXRealIPFallsBack(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	var got string
	r.GET("/test", func(c *gin.Context) { got = RealIP(c) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Real-IP", "not-an-ip")
	r.ServeHTTP(w, req)

	if got != "203.0.113.9" {
		t.Errorf("expected RemoteAddr fallback '203.0.113.9', got %q", got)
	}
}
