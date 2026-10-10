package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload: the account identity, and nothing about the profile.
//
// The email claim that used to sit here outlived email sign-in - it was read by nobody - and the profile
// is the phone number now, so there is nothing else to carry.
type Claims struct {
	UserID int64   `json:"user_id"`
	Phone  *string `json:"phone"`
	jwt.RegisteredClaims
}

// TokenValidator validates API tokens
type TokenValidator interface {
	ValidateToken(ctx context.Context, rawToken string) (int64, error)
}

// JWTAuth is the JWT + API Token authentication middleware
func JWTAuth(secret string, tokenValidator TokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication token not provided"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			c.Abort()
			return
		}

		tokenStr := parts[1]

		// Try API Token validation (tokens prefixed with kada_)
		if strings.HasPrefix(tokenStr, "kada_") {
			if tokenValidator != nil {
				userID, err := tokenValidator.ValidateToken(c.Request.Context(), tokenStr)
				if err == nil {
					c.Set("user_id", userID)
					c.Next()
					return
				}
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid API token"})
			c.Abort()
			return
		}

		// JWT validation: explicitly restrict to HS256 and require an expiry,
		// defending against algorithm confusion (alg=none/RS256, etc.) and non-expiring tokens
		token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication token is invalid or expired"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(*Claims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to parse authentication token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_phone", claims.Phone)
		c.Next()
	}
}

// GetUserID returns the current user ID from the gin.Context
func GetUserID(c *gin.Context) int64 {
	id, exists := c.Get("user_id")
	if !exists || id == nil {
		return 0
	}
	return id.(int64)
}
