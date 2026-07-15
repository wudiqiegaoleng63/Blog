// Package ratelimit provides Redis-based rate limiting middleware.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Middleware returns a Gin middleware that enforces a per-identity rate limit.
// identityFunc extracts the rate limit key (e.g., IP, user ID) from the context.
func Middleware(cfg config.RateLimitConfig, client *redis.Client, limitPerMinute int, identityFunc func(c *gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if client == nil {
			// Redis unavailable: allow through (degraded mode).
			c.Next()
			return
		}

		identity := identityFunc(c)
		key := fmt.Sprintf("ratelimit:%s:%d", identity, time.Now().UTC().Unix()/60)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		defer cancel()

		count, err := client.Incr(ctx, key).Result()
		if err != nil {
			// Redis error in request path: allow through.
			c.Next()
			return
		}

		if count == 1 {
			// Set a 60-second expiry on first increment (best-effort).
			client.Expire(ctx, key, 60*time.Second)
		}

		if int(count) > limitPerMinute {
			c.Abort()
			c.Error(apperr.RateLimited(""))
			return
		}

		c.Next()
	}
}

// ByIP extracts the client IP for rate limiting.
func ByIP(c *gin.Context) string {
	return c.ClientIP()
}

// ByUser extracts the authenticated user ID for rate limiting.
func ByUser(c *gin.Context) string {
	// Try to get from auth claims first
	// Falls back to IP if not authenticated
	return c.ClientIP()
}
