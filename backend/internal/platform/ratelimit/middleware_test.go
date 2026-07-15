package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/cache"
)

func TestNilRedisFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Middleware(nil, cache.NewKeyBuilder(config.RedisConfig{KeyPrefix: "test:"}), "auth:login", 1, ByIP))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
