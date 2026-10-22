package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminSecretHeader is where the operator's secret travels.
const AdminSecretHeader = "X-Admin-Secret"

// RequireAdmin refuses everyone who does not present the deployment's operator secret.
//
// It is a shared secret rather than an account because the site has no accounts: every visitor has the same
// kind of session, and a phone number that could sign in was removed with the sign-in. The secret is set on
// the host (ADMIN_SECRET) and typed into the console once, where it is kept in that browser's storage.
//
// An empty secret refuses everybody rather than allowing everybody: the setting is absent on a fresh
// deployment, and a missing environment variable must not be the thing that hands out the moderation routes.
// The comparison is constant-time, because the alternative leaks the answer one byte at a time.
func RequireAdmin(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		presented := c.GetHeader(AdminSecretHeader)
		if secret == "" || presented == "" ||
			subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "this account is not an operator"})
			c.Abort()
			return
		}
		c.Next()
	}
}
