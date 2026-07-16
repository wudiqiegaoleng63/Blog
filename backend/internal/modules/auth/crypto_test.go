package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/lsy/blog/internal/config"
)

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:      "test-secret-must-be-at-least-thirty-two-bytes-long",
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 24 * time.Hour,
		Argon2MemoryKiB: 8 * 1024, Argon2Iterations: 1, Argon2Parallelism: 1,
	}
}

func TestPasswordHashUsesArgon2id(t *testing.T) {
	cfg := testAuthConfig()
	hash, err := HashPassword("correct horse battery staple", cfg)
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want Argon2id PHC string", hash)
	}
	if err := VerifyPassword("correct horse battery staple", hash); err != nil {
		t.Fatalf("VerifyPassword(correct) error: %v", err)
	}
	if err := VerifyPassword("wrong", hash); err == nil {
		t.Fatal("VerifyPassword(wrong) succeeded")
	}
}

func TestPasswordLengthIsBoundedBeforeArgon2(t *testing.T) {
	cfg := testAuthConfig()
	oversized := strings.Repeat("p", maxPasswordBytes+1)
	if _, err := HashPassword(oversized, cfg); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("HashPassword(oversized) error = %v, want ErrInvalidCredentials", err)
	}
	hash, err := HashPassword("valid-password", cfg)
	if err != nil {
		t.Fatalf("HashPassword(valid) error: %v", err)
	}
	if err := VerifyPassword(oversized, hash); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("VerifyPassword(oversized) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestVerifyPasswordSupportsLegacyHash(t *testing.T) {
	salt := []byte("0123456789abcdef")
	input := append(append([]byte(nil), salt...), []byte("legacy-password")...)
	hash := sha256.Sum256(input)
	encoded := base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash[:])
	if err := VerifyPassword("legacy-password", encoded); err != nil {
		t.Fatalf("VerifyPassword(legacy) error: %v", err)
	}
	if err := VerifyPassword("wrong", encoded); err == nil {
		t.Fatal("VerifyPassword(wrong legacy) succeeded")
	}
}

func TestVerifyAccessTokenRequiresHS256AndExpiry(t *testing.T) {
	cfg := testAuthConfig()
	valid, _, err := GenerateAccessToken(cfg, "user-id", "alice", "user", 1)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error: %v", err)
	}
	if _, err := VerifyAccessToken(cfg, valid); err != nil {
		t.Fatalf("VerifyAccessToken(valid) error: %v", err)
	}

	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		expiry bool
	}{
		{name: "hs384", method: jwt.SigningMethodHS384, expiry: true},
		{name: "missing expiry", method: jwt.SigningMethodHS256, expiry: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
				Issuer: "blog", Subject: "user-id", IssuedAt: jwt.NewNumericDate(time.Now()),
			}, UserID: "user-id", Username: "alice", Role: "user", TokenVer: 1}
			if tc.expiry {
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Minute))
			}
			token, err := jwt.NewWithClaims(tc.method, claims).SignedString([]byte(cfg.JWTSecret))
			if err != nil {
				t.Fatalf("sign token: %v", err)
			}
			if _, err := VerifyAccessToken(cfg, token); err == nil {
				t.Fatal("VerifyAccessToken() succeeded, want rejection")
			}
		})
	}
}
