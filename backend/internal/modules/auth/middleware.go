package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/shared/apperr"
)

// contextKey is a private type to avoid key collisions.
type contextKey string

const claimsKey contextKey = "auth_claims"

// RequireAuth is a Gin middleware that validates the Bearer token and injects Claims.
func RequireAuth(cfg config.AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearer(c)
		if token == "" {
			c.Abort()
			c.Error(apperr.Unauthorized(""))
			return
		}

		claims, err := VerifyAccessToken(cfg, token)
		if err != nil {
			c.Abort()
			if err == ErrTokenExpired {
				c.Error(apperr.Unauthorized("token expired"))
			} else {
				c.Error(apperr.Unauthorized(""))
			}
			return
		}

		SetClaims(c, claims)
		c.Next()
	}
}

// RequireAdmin requires a valid token with role "admin".
func RequireAdmin(cfg config.AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearer(c)
		if token == "" {
			c.Abort()
			c.Error(apperr.Unauthorized(""))
			return
		}

		claims, err := VerifyAccessToken(cfg, token)
		if err != nil {
			c.Abort()
			if err == ErrTokenExpired {
				c.Error(apperr.Unauthorized("token expired"))
			} else {
				c.Error(apperr.Unauthorized(""))
			}
			return
		}

		if claims.Role != "admin" {
			c.Abort()
			c.Error(apperr.Forbidden("admin access required"))
			return
		}

		SetClaims(c, claims)
		c.Next()
	}
}

// OptionalAuth extracts the token if present but does not require it.
func OptionalAuth(cfg config.AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearer(c)
		if token != "" {
			claims, err := VerifyAccessToken(cfg, token)
			if err == nil {
				SetClaims(c, claims)
			}
		}
		c.Next()
	}
}

func extractBearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if header == "" || !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// SetClaims stores parsed token claims in the Gin context.
func SetClaims(c *gin.Context, claims *Claims) {
	c.Set(string(claimsKey), claims)
}

// GetClaims retrieves parsed token claims from the Gin context.
func GetClaims(c *gin.Context) *Claims {
	v, ok := c.Get(string(claimsKey))
	if !ok {
		return nil
	}
	claims, ok := v.(*Claims)
	if !ok {
		return nil
	}
	return claims
}

var _ = http.StatusOK
