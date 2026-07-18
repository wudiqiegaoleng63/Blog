package operations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/platform/observability"
)

type fakeChecker struct {
	err   error
	delay time.Duration
}

func (f fakeChecker) Ping(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestHealthRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Observability: config.ObservabilityConfig{RequestIDHeader: "X-Request-ID"}}
	engine, err := httpserver.New(cfg, observability.New(cfg.Observability))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	router := engine.Router()
	NewModule(cfg, nil, nil).Register(router)

	t.Run("live", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
		}
		var body struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Data.Status != "alive" {
			t.Fatalf("status body = %q, want alive", body.Data.Status)
		}
	})

	t.Run("ready reports missing mysql down and redis degraded", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
		var body struct {
			Success   bool   `json:"success"`
			RequestID string `json:"request_id"`
			Error     struct {
				Code    string `json:"code"`
				Details struct {
					Status string            `json:"status"`
					Checks map[string]string `json:"checks"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Success || body.RequestID == "" || body.Error.Code != "service_unavailable" {
			t.Fatalf("body = %#v, want unified service_unavailable response with request id", body)
		}
		if body.Error.Details.Status != "degraded" || body.Error.Details.Checks["mysql"] != "down" || body.Error.Details.Checks["redis"] != "degraded" {
			t.Fatalf("details = %#v, want degraded mysql/down redis/degraded", body.Error.Details)
		}
	})
}

func TestMetricsRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Observability: config.ObservabilityConfig{RequestIDHeader: "X-Request-ID"}}
	engine, err := httpserver.New(cfg, observability.New(cfg.Observability))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	NewModuleWithMetrics(cfg, nil, nil, engine.Metrics().Handler()).Register(engine.Router())

	response := httptest.NewRecorder()
	engine.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	response = httptest.NewRecorder()
	engine.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("metrics status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), `blog_http_requests_total{method="GET",route="/health/live",status="200"} 1`) {
		t.Fatalf("metrics body missing health route: %s", response.Body.String())
	}
}

func TestReadinessChecksDependenciesConcurrently(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Observability: config.ObservabilityConfig{RequestIDHeader: "X-Request-ID"}}
	engine, err := httpserver.New(cfg, observability.New(cfg.Observability))
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	newModule(cfg, fakeChecker{delay: 100 * time.Millisecond}, fakeChecker{delay: 100 * time.Millisecond}).Register(engine.Router())

	started := time.Now()
	response := httptest.NewRecorder()
	engine.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	elapsed := time.Since(started)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if elapsed >= 180*time.Millisecond {
		t.Fatalf("readiness took %s, want concurrent checks under 180ms", elapsed)
	}
}

func TestReadinessDependencyMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name       string
		mysqlErr   error
		redisErr   error
		aiEnabled  bool
		wantStatus int
		wantMySQL  string
		wantRedis  string
		wantAI     string
	}{
		{name: "all dependencies available", wantStatus: http.StatusOK, wantMySQL: "ok", wantRedis: "ok", wantAI: "disabled"},
		{name: "redis degraded is still ready", redisErr: errors.New("redis unavailable"), wantStatus: http.StatusOK, wantMySQL: "ok", wantRedis: "degraded", wantAI: "disabled"},
		{name: "mysql down is unavailable", mysqlErr: errors.New("mysql unavailable"), wantStatus: http.StatusServiceUnavailable, wantMySQL: "down", wantRedis: "ok", wantAI: "disabled"},
		{name: "ai enabled is reported without probing", aiEnabled: true, wantStatus: http.StatusOK, wantMySQL: "ok", wantRedis: "ok", wantAI: "enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{AI: config.AIConfig{Enabled: tc.aiEnabled}, Observability: config.ObservabilityConfig{RequestIDHeader: "X-Request-ID"}}
			engine, err := httpserver.New(cfg, observability.New(cfg.Observability))
			if err != nil {
				t.Fatalf("new engine: %v", err)
			}
			newModule(cfg, fakeChecker{err: tc.mysqlErr}, fakeChecker{err: tc.redisErr}).Register(engine.Router())

			response := httptest.NewRecorder()
			engine.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tc.wantStatus, response.Body.String())
			}

			var body struct {
				Data struct {
					Checks map[string]string `json:"checks"`
				} `json:"data"`
				Error struct {
					Details struct {
						Checks map[string]string `json:"checks"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			checks := body.Data.Checks
			if tc.wantStatus != http.StatusOK {
				checks = body.Error.Details.Checks
			}
			if checks["mysql"] != tc.wantMySQL || checks["redis"] != tc.wantRedis || checks["ai"] != tc.wantAI {
				t.Fatalf("checks = %#v, want mysql=%s redis=%s ai=%s", checks, tc.wantMySQL, tc.wantRedis, tc.wantAI)
			}
		})
	}
}
