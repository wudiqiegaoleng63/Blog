package config

import (
	"slices"
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		"APP_ENV":              "dev",
		"APP_SERVICE_MODE":     "api",
		"MYSQL_DSN":            "blog:blog@tcp(localhost:3306)/blog?parseTime=true&loc=UTC&charset=utf8mb4",
		"JWT_SECRET":           "test-secret-must-be-at-least-thirty-two-bytes-long",
		"HTTP_TRUSTED_PROXIES": "127.0.0.0/8",
	}
}

func TestLoad_ValidDev(t *testing.T) {
	setEnv(t, baseEnv())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid dev config, got error: %v", err)
	}
	if cfg.App.Env != "dev" {
		t.Fatalf("env=%s want dev", cfg.App.Env)
	}
	if cfg.AI.Enabled {
		t.Fatalf("AI should be disabled by default")
	}
}

func TestLoad_MissingDSN(t *testing.T) {
	kv := baseEnv()
	delete(kv, "MYSQL_DSN")
	setEnv(t, kv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when MYSQL_DSN missing")
	}
}

func TestLoad_ShortJWTSecret(t *testing.T) {
	kv := baseEnv()
	kv["JWT_SECRET"] = "short"
	setEnv(t, kv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when JWT_SECRET too short")
	}
}

func TestLoad_ProductionRequiresSecureCookie(t *testing.T) {
	kv := baseEnv()
	kv["APP_ENV"] = "production"
	kv["AUTH_COOKIE_SECURE"] = "false"
	setEnv(t, kv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when production without secure cookie")
	}
}

func TestLoad_RejectsMalformedTypedEnvironmentValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "boolean", key: "CORS_ALLOW_CREDENTIALS", value: "fals"},
		{name: "integer", key: "MYSQL_MAX_OPEN_CONNS", value: "many"},
		{name: "duration", key: "HTTP_READ_TIMEOUT", value: "soon"},
		{name: "float", key: "RAG_SCORE_THRESHOLD", value: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kv := baseEnv()
			kv[tc.key] = tc.value
			setEnv(t, kv)
			if _, err := Load(); err == nil {
				t.Fatalf("expected malformed %s=%q to be rejected", tc.key, tc.value)
			}
		})
	}
}

func TestLoad_RejectsNonPositiveRuntimeDurations(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "APP_REQUEST_TIMEOUT", value: "0s"},
		{key: "HTTP_SHUTDOWN_TIMEOUT", value: "-1s"},
		{key: "MYSQL_CONN_MAX_LIFETIME", value: "0s"},
		{key: "REDIS_DIAL_TIMEOUT", value: "-1ms"},
		{key: "JOBS_POLL_INTERVAL", value: "0s"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			kv := baseEnv()
			kv[tc.key] = tc.value
			setEnv(t, kv)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("Load() error = %v, want error naming %s", err, tc.key)
			}
		})
	}
}

func TestLoad_CustomRequestIDHeaderIsAddedToCORS(t *testing.T) {
	kv := baseEnv()
	kv["REQUEST_ID_HEADER"] = "X-Correlation-ID"
	kv["CORS_ALLOWED_HEADERS"] = "Content-Type"
	kv["CORS_EXPOSED_HEADERS"] = "ETag"
	setEnv(t, kv)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !slices.Contains(cfg.CORS.AllowedHeaders, "X-Correlation-ID") {
		t.Fatalf("allowed headers = %#v, want custom request ID header", cfg.CORS.AllowedHeaders)
	}
	if !slices.Contains(cfg.CORS.ExposedHeaders, "X-Correlation-ID") {
		t.Fatalf("exposed headers = %#v, want custom request ID header", cfg.CORS.ExposedHeaders)
	}
}

func TestLoad_RejectsUnsafeRequestIDHeaders(t *testing.T) {
	for _, header := range []string{"Bad Header", "Authorization", "Cookie"} {
		t.Run(header, func(t *testing.T) {
			kv := baseEnv()
			kv["REQUEST_ID_HEADER"] = header
			setEnv(t, kv)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "REQUEST_ID_HEADER") {
				t.Fatalf("Load() error = %v, want REQUEST_ID_HEADER validation error", err)
			}
		})
	}
}

func TestLoad_ProductionRejectsWildcardCORSWithoutCredentials(t *testing.T) {
	kv := baseEnv()
	kv["APP_ENV"] = "production"
	kv["AUTH_COOKIE_SECURE"] = "true"
	kv["CORS_ALLOW_CREDENTIALS"] = "false"
	kv["CORS_ALLOWED_ORIGINS"] = "*"
	setEnv(t, kv)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("Load() error = %v, want production wildcard CORS rejection", err)
	}
}

func TestLoad_ProductionRejectsWildcardCORS(t *testing.T) {
	kv := baseEnv()
	kv["APP_ENV"] = "production"
	kv["AUTH_COOKIE_SECURE"] = "true"
	kv["CORS_ALLOWED_ORIGINS"] = "*"
	setEnv(t, kv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when wildcard origin with credentials")
	}
}

func TestLoad_AIRequiresEmbeddingDimensions(t *testing.T) {
	kv := baseEnv()
	kv["AI_ENABLED"] = "true"
	kv["AI_CHAT_BASE_URL"] = "https://api.example.com/v1"
	kv["AI_CHAT_API_KEY"] = "k"
	kv["AI_CHAT_MODEL"] = "gpt"
	kv["AI_EMBEDDING_BASE_URL"] = "https://api.example.com/v1"
	kv["AI_EMBEDDING_API_KEY"] = "k"
	kv["AI_EMBEDDING_MODEL"] = "embed"
	kv["MILVUS_ADDR"] = "localhost:19530"
	// 故意不设置 AI_EMBEDDING_DIMENSIONS
	setEnv(t, kv)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when embedding dimensions missing")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.example.com", "https://api.example.com"},
		{"https://api.example.com/", "https://api.example.com"},
		{"https://api.example.com/v1", "https://api.example.com/v1"},
		{"api.example.com/v1/", "https://api.example.com/v1"},
	}
	for _, c := range cases {
		got, err := normalizeBaseURL(c.in)
		if err != nil {
			t.Fatalf("normalizeBaseURL(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("normalizeBaseURL(%q) = %q want %q", c.in, got, c.want)
		}
	}
}
