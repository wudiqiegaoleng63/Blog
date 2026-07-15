package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Module wires auth routes into the API.
type Module struct {
	svc *Service
}

// NewModule creates an auth module.
func NewModule(cfg *config.Config, repo *Repository) *Module {
	return &Module{svc: NewService(cfg, repo)}
}

// Register mounts auth routes under the given router group.
func (m *Module) Register(r *gin.Engine) {
	v1 := r.Group("/api/v1/auth")
	{
		v1.POST("/register", m.register)
		v1.POST("/login", m.login)
		v1.POST("/refresh", m.refresh)
		v1.POST("/logout", m.logout)
		v1.GET("/me", m.me)
	}
}

func (m *Module) register(c *gin.Context) {
	var input RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	pair, err := m.svc.Register(c, input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.Created(c, pair)
}

func (m *Module) login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperr.Validation("invalid request body", nil))
		return
	}
	pair, err := m.svc.Login(c, input)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, pair)
}

func (m *Module) refresh(c *gin.Context) {
	rt, err := getRefreshToken(c, m.svc.cfg.Auth.RefreshCookieName)
	if err != nil {
		c.Error(apperr.Unauthorized("refresh token not found"))
		return
	}
	pair, err := m.svc.Refresh(c, rt)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, pair)
}

func (m *Module) logout(c *gin.Context) {
	rt, err := getRefreshToken(c, m.svc.cfg.Auth.RefreshCookieName)
	if err != nil {
		m.svc.clearRefreshCookie(c)
		httpserver.OK(c, gin.H{"message": "logged out"})
		return
	}
	if err := m.svc.Logout(c, rt); err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{"message": "logged out"})
}

func (m *Module) me(c *gin.Context) {
	claims := GetClaims(c)
	if claims == nil {
		c.Error(apperr.Unauthorized(""))
		return
	}
	user, profile, err := m.svc.Me(c, claims.UserID)
	if err != nil {
		c.Error(err)
		return
	}
	httpserver.OK(c, gin.H{
		"user":    sanitizeUser(user),
		"profile": profile,
	})
}

func sanitizeUser(u *domain.User) gin.H {
	return gin.H{
		"public_id": u.PublicID,
		"email":     u.Email,
		"username":  u.Username,
		"role":      u.Role,
		"status":    u.Status,
	}
}

func getRefreshToken(c *gin.Context, cookieName string) (string, error) {
	cookie, err := c.Cookie(cookieName)
	if err == nil && cookie != "" {
		return cookie, nil
	}
	header := c.GetHeader("X-Refresh-Token")
	if header != "" {
		return header, nil
	}
	return "", err
}
