//go:build integration

package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/cache"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/platform/observability"
)

func TestRedisLimitAndFailureContracts(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	password := os.Getenv("TEST_REDIS_PASSWORD")
	if addr == "" {
		t.Fatal("TEST_REDIS_ADDR is required for integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB(): %v", err)
	}
	keys := cache.NewKeyBuilder(config.RedisConfig{KeyPrefix: "blog:integration:ratelimit:"})

	t.Run("real Redis enforces fixed window", func(t *testing.T) {
		router := testRateLimitRouter(t, Middleware(client, keys, "auth:login", 2, ByIP))
		for request := 1; request <= 3; request++ {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			want := http.StatusNoContent
			if request == 3 {
				want = http.StatusTooManyRequests
				if response.Header().Get("Retry-After") != "60" {
					t.Fatalf("Retry-After = %q, want 60", response.Header().Get("Retry-After"))
				}
			}
			if response.Code != want {
				t.Fatalf("request %d status = %d body=%s, want %d", request, response.Code, response.Body.String(), want)
			}
		}
	})

	unavailable := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 25 * time.Millisecond,
		ReadTimeout: 25 * time.Millisecond, WriteTimeout: 25 * time.Millisecond,
		MaxRetries: 0,
	})
	defer unavailable.Close()

	t.Run("ordinary scope fails open", func(t *testing.T) {
		router := testRateLimitRouter(t, Middleware(unavailable, keys, "auth:login:down", 1, ByIP))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), http.StatusNoContent)
		}
	})

	t.Run("strict scope fails closed", func(t *testing.T) {
		router := testRateLimitRouter(t, StrictMiddleware(unavailable, keys, "ai:ask:down", 1, ByIP))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), http.StatusServiceUnavailable)
		}
	})
}

func testRateLimitRouter(t *testing.T, limiter gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	logger := observability.New(config.ObservabilityConfig{LogLevel: "error", LogFormat: "json", RequestIDHeader: "X-Request-ID"})
	router := gin.New()
	router.Use(httpserver.RequestID("X-Request-ID"), httpserver.ErrorHandler(logger), limiter)
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return router
}
