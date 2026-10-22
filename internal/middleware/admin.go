package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

// GetUserPhone returns the phone number the token was issued for, if it carried one.
//
// API tokens do not: they identify an account, not a person signing in, and an operator route is a person
// signing in. That is what keeps a leaked `kada_` token from stopping links - the token can create them.
func GetUserPhone(c *gin.Context) *string {
	v, exists := c.Get("user_phone")
	if !exists || v == nil {
		return nil
	}
	phone, ok := v.(*string)
	if !ok || phone == nil {
		return nil
	}
	return phone
}

// RequireAdmin refuses everyone whose number is not in the operator list.
//
// An empty list refuses everybody rather than allowing everybody: the setting is absent on a fresh
// deployment, and a missing environment variable must not be the thing that hands out the moderation
// routes. It runs after JWTAuth, so a request without a valid token has already been refused.
func RequireAdmin(phones []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		phone := GetUserPhone(c)
		if phone == nil || !slices.Contains(phones, *phone) {
			c.JSON(http.StatusForbidden, gin.H{"error": "this account is not an operator"})
			c.Abort()
			return
		}
		c.Next()
	}
}
