// Package auth implements password hashing and access-token cryptography.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"

	"github.com/lsy/blog/internal/config"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrTokenExpired       = errors.New("auth: token expired")
	ErrTokenInvalid       = errors.New("auth: token invalid")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrUsernameTaken      = errors.New("auth: username already taken")
)

const (
	argonSaltLength = 16
	argonKeyLength  = 32
	// maxPasswordBytes bounds Argon2 work for both registration and login.
	// Password managers can still use passphrases while oversized request fields
	// cannot turn password verification into an allocation attack.
	maxPasswordBytes = 1024
)

// HashPassword derives an Argon2id password hash and encodes its parameters in
// PHC form so future parameter upgrades remain verifiable.
func HashPassword(password string, cfg config.AuthConfig) (string, error) {
	if password == "" || len(password) > maxPasswordBytes {
		return "", ErrInvalidCredentials
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, cfg.Argon2Iterations, cfg.Argon2MemoryKiB, cfg.Argon2Parallelism, argonKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		cfg.Argon2MemoryKiB,
		cfg.Argon2Iterations,
		cfg.Argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies a PHC Argon2id hash while bounding parsed parameters
// before allocating memory.
func VerifyPassword(password, encoded string) error {
	if len(password) > maxPasswordBytes {
		return ErrInvalidCredentials
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		return verifyLegacyPassword(password, encoded)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return ErrInvalidCredentials
	}
	version, err := parsePrefixedUint(parts[2], "v=")
	if err != nil || version != argon2.Version {
		return ErrInvalidCredentials
	}

	var memory, iterations uint64
	var parallelism uint64
	for _, parameter := range strings.Split(parts[3], ",") {
		switch {
		case strings.HasPrefix(parameter, "m="):
			memory, err = parsePrefixedUint(parameter, "m=")
		case strings.HasPrefix(parameter, "t="):
			iterations, err = parsePrefixedUint(parameter, "t=")
		case strings.HasPrefix(parameter, "p="):
			parallelism, err = parsePrefixedUint(parameter, "p=")
		default:
			return ErrInvalidCredentials
		}
		if err != nil {
			return ErrInvalidCredentials
		}
	}
	if memory < 8*1024 || memory > 1024*1024 || iterations < 1 || iterations > 20 || parallelism < 1 || parallelism > 32 {
		return ErrInvalidCredentials
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return ErrInvalidCredentials
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return ErrInvalidCredentials
	}

	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expected)))
	if subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// verifyLegacyPassword keeps accounts created by the Stage 1 foundation
// usable during the Argon2id rollout. New and changed passwords never use it.
func verifyLegacyPassword(password, encoded string) error {
	parts := strings.SplitN(encoded, "$", 2)
	if len(parts) != 2 {
		return ErrInvalidCredentials
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != argonSaltLength {
		return ErrInvalidCredentials
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(expected) != sha256.Size {
		return ErrInvalidCredentials
	}
	input := make([]byte, 0, len(salt)+len(password))
	input = append(input, salt...)
	input = append(input, password...)
	actual := sha256.Sum256(input)
	if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

func parsePrefixedUint(value, prefix string) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, ErrInvalidCredentials
	}
	return strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 64)
}

func GenerateRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

type Claims struct {
	jwt.RegisteredClaims
	UserID   string `json:"uid"`
	Username string `json:"uname"`
	Role     string `json:"role"`
	TokenVer uint64 `json:"tver"`
}

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
		UserID: userID, Username: username, Role: role, TokenVer: tokenVer,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func VerifyAccessToken(cfg config.AuthConfig, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{},
		func(*jwt.Token) (any, error) { return []byte(cfg.JWTSecret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
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
	if !ok || !token.Valid || claims.UserID == "" || claims.Subject != claims.UserID {
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
