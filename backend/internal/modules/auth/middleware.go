package auth

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/shared/apperr"
)

const (
	claimsKey      = "auth_claims"
	currentUserKey = "auth_current_user"
)

// RequireAuth validates the JWT against the current account state. This makes
// account suspension, role changes, and token-version revocation effective on
// every protected route rather than only when the access token expires.
func RequireAuth(cfg config.AuthConfig, repo *Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, user, err := authenticateCurrent(c, cfg, repo)
		if err != nil {
			c.Abort()
			_ = c.Error(err)
			return
		}
		SetClaims(c, claims)
		SetCurrentUser(c, user)
		c.Next()
	}
}

func RequireAdmin(cfg config.AuthConfig, repo *Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, user, err := authenticateCurrent(c, cfg, repo)
		if err != nil {
			c.Abort()
			_ = c.Error(err)
			return
		}
		if claims.Role != "admin" {
			c.Abort()
			_ = c.Error(apperr.Forbidden("admin access required"))
			return
		}
		SetClaims(c, claims)
		SetCurrentUser(c, user)
		c.Next()
	}
}

func OptionalAuth(cfg config.AuthConfig, repo *Repository) gin.HandlerFunc {
	return func(c *gin.Context) {
		if claims, user, err := authenticateCurrent(c, cfg, repo); err == nil {
			SetClaims(c, claims)
			SetCurrentUser(c, user)
		}
		c.Next()
	}
}

func authenticateCurrent(c *gin.Context, cfg config.AuthConfig, repo *Repository) (*Claims, *domain.User, error) {
	claims, err := authenticate(c, cfg)
	if err != nil {
		return nil, nil, err
	}
	user, err := repo.FindUserByPublicID(c.Request.Context(), claims.UserID)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	if user == nil || user.Status != "active" || user.TokenVersion != claims.TokenVer {
		return nil, nil, apperr.Unauthorized("account is unavailable or token was revoked")
	}
	claims.Role = user.Role
	claims.Username = user.Username
	return claims, user, nil
}

func authenticate(c *gin.Context, cfg config.AuthConfig) (*Claims, error) {
	token := extractBearer(c)
	if token == "" {
		return nil, apperr.Unauthorized("")
	}
	claims, err := VerifyAccessToken(cfg, token)
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			return nil, apperr.Unauthorized("token expired")
		}
		return nil, apperr.Unauthorized("")
	}
	return claims, nil
}

func extractBearer(c *gin.Context) string {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func SetClaims(c *gin.Context, claims *Claims) { c.Set(claimsKey, claims) }

func GetClaims(c *gin.Context) *Claims {
	value, ok := c.Get(claimsKey)
	if !ok {
		return nil
	}
	claims, _ := value.(*Claims)
	return claims
}

func SetCurrentUser(c *gin.Context, user *domain.User) { c.Set(currentUserKey, user) }

func GetCurrentUser(c *gin.Context) *domain.User {
	value, ok := c.Get(currentUserKey)
	if !ok {
		return nil
	}
	user, _ := value.(*domain.User)
	return user
}
