package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func adminRouter(secret, presented string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	// The header has to be set before the route's chain runs, so this goes on the engine first.
	r.Use(func(c *gin.Context) {
		if presented != "" {
			c.Request.Header.Set(AdminSecretHeader, presented)
		}
		c.Next()
	})
	r.GET("/admin", RequireAdmin(secret), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func status(r *gin.Engine) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
	return w.Code
}

func TestRequireAdmin_AllowsTheConfiguredSecret(t *testing.T) {
	if got := status(adminRouter("s3cret", "s3cret")); got != http.StatusOK {
		t.Errorf("status = %d, want %d", got, http.StatusOK)
	}
}

func TestRequireAdmin_RefusesAnythingElse(t *testing.T) {
	for _, presented := range []string{"", "wrong", "s3cre", "s3cret "} {
		if got := status(adminRouter("s3cret", presented)); got != http.StatusForbidden {
			t.Errorf("presented %q: status = %d, want %d", presented, got, http.StatusForbidden)
		}
	}
}

// The setting is absent on a fresh deployment, and an empty secret has to refuse everybody: the missing
// value must not be the thing that hands out the moderation routes.
func TestRequireAdmin_RefusesEverybodyWhenUnset(t *testing.T) {
	if got := status(adminRouter("", "")); got != http.StatusForbidden {
		t.Errorf("status = %d, want %d", got, http.StatusForbidden)
	}
	if got := status(adminRouter("", "anything")); got != http.StatusForbidden {
		t.Errorf("status = %d, want %d", got, http.StatusForbidden)
	}
}
