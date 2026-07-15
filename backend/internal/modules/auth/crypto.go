// Package auth 实现用户认证：密码哈希、JWT 签发/验证、注册/登录/登出/刷新。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/lsy/blog/internal/config"
)

// Common errors returned by this package.
var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrTokenExpired       = errors.New("auth: token expired")
	ErrTokenInvalid       = errors.New("auth: token invalid")
	ErrUserBanned         = errors.New("auth: user is banned")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrUsernameTaken      = errors.New("auth: username already taken")
)

// HashPassword hashes a plaintext password using a constant-time comparison safe
// approach. We use a simple SHA-256 based scheme for Stage 1; Argon2id will be
// introduced when the dependency is added.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	hash := sha256.Sum256(append(salt, []byte(password)...))
	encoded := base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash[:])
	return encoded, nil
}

// VerifyPassword compares a plaintext password against a stored hash.
func VerifyPassword(password, stored string) error {
	parts := strings.SplitN(stored, "$", 2)
	if len(parts) != 2 {
		return ErrInvalidCredentials
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return ErrInvalidCredentials
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return ErrInvalidCredentials
	}
	hash := sha256.Sum256(append(salt, []byte(password)...))
	if subtle.ConstantTimeCompare(hash[:], expected) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// GenerateRefreshToken creates a random 32-byte token encoded as URL-safe base64.
func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hash of a token string, suitable for storage.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Claims carries the JWT payload.
type Claims struct {
	jwt.RegisteredClaims
	UserID      string `json:"uid"`
	Username    string `json:"uname"`
	Role        string `json:"role"`
	TokenVer    uint64 `json:"tver"`
}

// GenerateAccessToken signs a new JWT access token.
func GenerateAccessToken(cfg config.AuthConfig, userID, username, role string, tokenVer uint64) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(cfg.AccessTokenTTL)
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "blog",
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        mustGenerateID(),
		},
		UserID:   userID,
		Username: username,
		Role:     role,
		TokenVer: tokenVer,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// VerifyAccessToken parses and validates a JWT access token.
func VerifyAccessToken(cfg config.AuthConfig, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(cfg.JWTSecret), nil
		},
		jwt.WithIssuer("blog"),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

func mustGenerateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand failed: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
