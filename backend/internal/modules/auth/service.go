package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/domain"
	"github.com/lsy/blog/internal/platform/ids"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Service implements auth business logic.
type Service struct {
	cfg  *config.Config
	repo *Repository
}

// NewService creates an auth service.
func NewService(cfg *config.Config, repo *Repository) *Service {
	return &Service{cfg: cfg, repo: repo}
}

// --- Registration ---

type RegisterInput struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Service) Register(ctx *gin.Context, input RegisterInput) (*domain.TokenPair, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	username := strings.TrimSpace(input.Username)

	if email == "" || !strings.Contains(email, "@") {
		return nil, apperr.Validation("invalid email", gin.H{"field": "email"})
	}
	if len(username) < 3 || len(username) > 32 {
		return nil, apperr.Validation("username must be 3-32 characters", gin.H{"field": "username"})
	}
	if len(input.Password) < 8 {
		return nil, apperr.Validation("password must be at least 8 characters", gin.H{"field": "password"})
	}

	existing, err := s.repo.FindUserByEmail(ctx.Request.Context(), email)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if existing != nil {
		return nil, apperr.Conflict("email already registered")
	}

	existing, err = s.repo.FindUserByUsername(ctx.Request.Context(), username)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if existing != nil {
		return nil, apperr.Conflict("username already taken")
	}

	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}

	publicID := ids.MustNewULID()
	user := &domain.User{
		PublicID:        publicID,
		Email:           email,
		EmailNormalized: email,
		Username:        username,
		PasswordHash:    passwordHash,
		Role:            "user",
		Status:          "active",
		TokenVersion:    1,
	}
	profile := &domain.UserProfile{
		DisplayName: username,
	}

	if err := s.repo.CreateUser(ctx.Request.Context(), user, profile); err != nil {
		if errors.Is(err, ErrEmailTaken) || errors.Is(err, ErrUsernameTaken) {
			return nil, apperr.Conflict(err.Error())
		}
		return nil, apperr.Internal(err, "")
	}

	return s.issueTokenPair(ctx, user.ID, publicID, username, "user", 1)
}

// --- Login ---

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Service) Login(ctx *gin.Context, input LoginInput) (*domain.TokenPair, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return nil, apperr.Validation("email is required", gin.H{"field": "email"})
	}

	user, err := s.repo.FindUserByEmail(ctx.Request.Context(), email)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if user == nil {
		return nil, apperr.Unauthorized("invalid email or password")
	}
	if user.Status == "banned" {
		return nil, apperr.Forbidden("account is banned")
	}

	if err := VerifyPassword(input.Password, user.PasswordHash); err != nil {
		return nil, apperr.Unauthorized("invalid email or password")
	}

	_ = s.repo.UpdateLastLogin(ctx.Request.Context(), user.ID, time.Now().UTC())

	return s.issueTokenPair(ctx, user.ID, user.PublicID, user.Username, user.Role, user.TokenVersion)
}

// --- Token Refresh ---

func (s *Service) Refresh(ctx *gin.Context, refreshTokenStr string) (*domain.TokenPair, error) {
	tokenHash := HashToken(refreshTokenStr)

	rt, err := s.repo.FindRefreshTokenByHash(ctx.Request.Context(), tokenHash)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if rt == nil {
		return nil, apperr.Unauthorized("invalid or expired refresh token")
	}

	// Load user to get current token version
	user, err := s.repo.FindUserByID(ctx.Request.Context(), rt.UserID)
	if err != nil {
		return nil, apperr.Internal(err, "")
	}
	if user == nil || user.Status == "banned" {
		return nil, apperr.Unauthorized("account not found or banned")
	}

	// Issue new pair (this also creates a new refresh token row)
	pair, newRT, err := s.issueTokenPairWithRefresh(ctx, user.ID, user.PublicID, user.Username, user.Role, user.TokenVersion, rt.FamilyID)
	if err != nil {
		return nil, err
	}

	// Rotate: mark old token as replaced by the new one
	_ = s.repo.SetTokenReplaced(ctx.Request.Context(), rt.ID, newRT.ID)

	pair.RefreshToken = newRT.Raw
	s.setRefreshCookie(ctx, newRT.Raw)
	return pair, nil
}

// --- Logout ---

func (s *Service) Logout(ctx *gin.Context, refreshTokenStr string) error {
	tokenHash := HashToken(refreshTokenStr)
	rt, err := s.repo.FindRefreshTokenByHash(ctx.Request.Context(), tokenHash)
	if err != nil {
		return apperr.Internal(err, "")
	}
	if rt == nil {
		return nil // already logged out; idempotent
	}
	if err := s.repo.RevokeTokenFamily(ctx.Request.Context(), rt.FamilyID); err != nil {
		return apperr.Internal(err, "")
	}
	s.clearRefreshCookie(ctx)
	return nil
}

// --- Me ---

func (s *Service) Me(ctx *gin.Context, userPublicID string) (*domain.User, *domain.UserProfile, error) {
	user, err := s.repo.FindUserByPublicID(ctx.Request.Context(), userPublicID)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	if user == nil {
		return nil, nil, apperr.NotFound("user not found")
	}
	profile, err := s.repo.GetProfile(ctx.Request.Context(), user.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	return user, profile, nil
}

// --- internal helpers ---

type refreshTokenRaw struct {
	*domain.RefreshToken
	Raw string
}

func (s *Service) issueTokenPair(ctx *gin.Context, numericID uint64, publicID, username, role string, tokenVer uint64) (*domain.TokenPair, error) {
	pair, rt, err := s.issueTokenPairWithRefresh(ctx, numericID, publicID, username, role, tokenVer, ids.MustNewULID())
	if err != nil {
		return nil, err
	}
	pair.RefreshToken = rt.Raw
	s.setRefreshCookie(ctx, rt.Raw)
	return pair, nil
}

func (s *Service) issueTokenPairWithRefresh(ctx *gin.Context, numericID uint64, publicID, username, role string, tokenVer uint64, familyID string) (*domain.TokenPair, *refreshTokenRaw, error) {
	accessToken, _, err := GenerateAccessToken(s.cfg.Auth, publicID, username, role, tokenVer)
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}

	rawRT, err := GenerateRefreshToken()
	if err != nil {
		return nil, nil, apperr.Internal(err, "")
	}
	rtHash := HashToken(rawRT)

	newRT := &domain.RefreshToken{
		PublicID: ids.MustNewULID(),
		UserID:   numericID,
		FamilyID: familyID,
		TokenHash: rtHash,
		ExpiresAt: time.Now().UTC().Add(s.cfg.Auth.RefreshTokenTTL),
	}

	if err := s.repo.CreateRefreshToken(ctx.Request.Context(), newRT); err != nil {
		return nil, nil, apperr.Internal(err, "")
	}

	return &domain.TokenPair{
		AccessToken: accessToken,
		ExpiresIn:   int64(s.cfg.Auth.AccessTokenTTL.Seconds()),
		TokenType:   "Bearer",
	}, &refreshTokenRaw{RefreshToken: newRT, Raw: rawRT}, nil
}

func (s *Service) setRefreshCookie(ctx *gin.Context, token string) {
	secure := s.cfg.Auth.Secure
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     s.cfg.Auth.RefreshCookieName,
		Value:    token,
		Path:     s.cfg.Auth.RefreshCookiePath,
		Domain:   s.cfg.Auth.CookieDomain,
		MaxAge:   int(s.cfg.Auth.RefreshTokenTTL.Seconds()),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearRefreshCookie(ctx *gin.Context) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     s.cfg.Auth.RefreshCookieName,
		Value:    "",
		Path:     s.cfg.Auth.RefreshCookiePath,
		Domain:   s.cfg.Auth.CookieDomain,
		MaxAge:   -1,
		Secure:   s.cfg.Auth.Secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
