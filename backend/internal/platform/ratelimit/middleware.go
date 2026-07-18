// Package ratelimit provides scoped Redis-backed fixed-window rate limiting.
package ratelimit

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/lsy/blog/internal/platform/cache"
	"github.com/lsy/blog/internal/platform/observability"
	"github.com/lsy/blog/internal/shared/apperr"
)

var incrementScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current
`)

type IdentityFunc func(*gin.Context) string

// Middleware enforces a scoped limit. Redis failures deliberately fail open to
// preserve the project's soft-dependency contract; callers should separately
// protect public ingress with a gateway/WAF in production.
func Middleware(client redis.Scripter, keys cache.KeyBuilder, scope string, limitPerMinute int, identity IdentityFunc) gin.HandlerFunc {
	return middleware(client, keys, scope, limitPerMinute, identity, false, nil)
}

func MiddlewareWithMetrics(client redis.Scripter, keys cache.KeyBuilder, scope string, limitPerMinute int, identity IdentityFunc, metrics *observability.Metrics) gin.HandlerFunc {
	return middleware(client, keys, scope, limitPerMinute, identity, false, metrics)
}

// StrictMiddleware fails closed when Redis cannot enforce a cost-sensitive limit.
func StrictMiddleware(client redis.Scripter, keys cache.KeyBuilder, scope string, limitPerMinute int, identity IdentityFunc) gin.HandlerFunc {
	return middleware(client, keys, scope, limitPerMinute, identity, true, nil)
}

func StrictMiddlewareWithMetrics(client redis.Scripter, keys cache.KeyBuilder, scope string, limitPerMinute int, identity IdentityFunc, metrics *observability.Metrics) gin.HandlerFunc {
	return middleware(client, keys, scope, limitPerMinute, identity, true, metrics)
}

func middleware(client redis.Scripter, keys cache.KeyBuilder, scope string, limitPerMinute int, identity IdentityFunc, failClosed bool, metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		if client == nil || limitPerMinute <= 0 {
			if failClosed {
				metrics.ObserveRateLimit(scope, "fail_closed")
				c.Abort()
				_ = c.Error(apperr.ServiceUnavailable("AI rate limiting is temporarily unavailable"))
				return
			}
			metrics.ObserveRateLimit(scope, "fail_open")
			c.Next()
			return
		}
		key := keys.RateLimit(scope, identity(c)+":"+time.Now().UTC().Format("200601021504"))
		ctx, cancel := context.WithTimeout(c.Request.Context(), 250*time.Millisecond)
		defer cancel()
		result, err := incrementScript.Run(ctx, client, []string{key}, int64((2*time.Minute)/time.Millisecond)).Int64()
		if err != nil {
			if failClosed {
				metrics.ObserveRateLimit(scope, "fail_closed")
				c.Abort()
				_ = c.Error(apperr.ServiceUnavailable("AI rate limiting is temporarily unavailable"))
				return
			}
			metrics.ObserveRateLimit(scope, "fail_open")
			c.Next()
			return
		}
		if result > int64(limitPerMinute) {
			metrics.ObserveRateLimit(scope, "rejected")
			c.Header("Retry-After", "60")
			c.Abort()
			_ = c.Error(apperr.RateLimited(""))
			return
		}
		c.Next()
	}
}

func ByIP(c *gin.Context) string { return c.ClientIP() }

func ByUser(c *gin.Context) string {
	if value, exists := c.Get("auth_claims"); exists {
		if claims, ok := value.(interface{ GetSubject() (string, error) }); ok {
			if subject, err := claims.GetSubject(); err == nil && subject != "" {
				return "user:" + subject
			}
		}
	}
	return "ip:" + strings.TrimSpace(c.ClientIP())
}
