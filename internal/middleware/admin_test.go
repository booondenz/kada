package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// withPhone mounts RequireAdmin behind a context that stands in for JWTAuth.
func withPhone(t *testing.T, phone *string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/admin", func(c *gin.Context) {
		c.Set("user_id", int64(7))
		if phone != nil {
			c.Set("user_phone", phone)
		}
		c.Next()
	}, RequireAdmin([]string{"13800000000"}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func get(r *gin.Engine) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
	return w.Code
}

func TestRequireAdmin_AllowsAListedNumber(t *testing.T) {
	phone := "13800000000"

	if got := get(withPhone(t, &phone)); got != http.StatusOK {
		t.Errorf("status = %d, want %d", got, http.StatusOK)
	}
}

func TestRequireAdmin_RefusesAnUnlistedNumber(t *testing.T) {
	phone := "13900000000"

	if got := get(withPhone(t, &phone)); got != http.StatusForbidden {
		t.Errorf("status = %d, want %d", got, http.StatusForbidden)
	}
}

// An API token authenticates an account, not a person signing in, and it carries no number.
func TestRequireAdmin_RefusesATokenWithoutANumber(t *testing.T) {
	if got := get(withPhone(t, nil)); got != http.StatusForbidden {
		t.Errorf("status = %d, want %d", got, http.StatusForbidden)
	}
}

// The setting is absent on a fresh deployment, and an empty list has to refuse everybody: the missing value
// must not be the thing that hands out the moderation routes.
func TestRequireAdmin_RefusesEverybodyWhenTheListIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	phone := "13800000000"

	r := gin.New()
	r.GET("/admin", func(c *gin.Context) {
		c.Set("user_phone", &phone)
		c.Next()
	}, RequireAdmin(nil), func(c *gin.Context) { c.Status(http.StatusOK) })

	if got := get(r); got != http.StatusForbidden {
		t.Errorf("status = %d, want %d", got, http.StatusForbidden)
	}
}
