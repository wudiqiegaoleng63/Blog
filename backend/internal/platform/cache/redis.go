// Package cache 封装 Redis 客户端与缓存键约定。
//
// Redis 不是业务事实源；当 Redis 不可用时，公开读路径应降级到 MySQL。
// 本包仅提供客户端与键生成器，具体缓存策略由各模块实现。
package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lsy/blog/internal/config"
)

// New 创建 Redis 客户端并验证连通性。
func New(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	if cfg.Addr == "" {
		return nil, errors.New("cache: REDIS_ADDR is required")
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		// 保留客户端：go-redis 会在后续请求自动重连，使软依赖恢复时无需重启进程。
		return client, fmt.Errorf("cache: ping redis: %w", err)
	}

	return client, nil
}

// KeyBuilder 在配置的 KeyPrefix 基础上拼接命名空间化的缓存键。
// 例如 posts:slug:hello-world、posts:list:namespace。
type KeyBuilder struct {
	prefix string
}

// NewKeyBuilder 构造键构造器。
func NewKeyBuilder(cfg config.RedisConfig) KeyBuilder {
	return KeyBuilder{prefix: cfg.KeyPrefix}
}

// Post 返回文章相关缓存键。
func (k KeyBuilder) Post(parts ...string) string {
	return k.join(append([]string{"post"}, parts...)...)
}

// PostsList 返回文章列表缓存键。
func (k KeyBuilder) PostsList(parts ...string) string {
	return k.join(append([]string{"posts", "list"}, parts...)...)
}

// PostsNamespace 返回文章列表命名空间版本键。
// 发布文章时递增该值，使旧列表缓存整体失效。
func (k KeyBuilder) PostsNamespace() string {
	return k.join("posts", "namespace")
}

// Taxonomy 返回分类/标签缓存键。
func (k KeyBuilder) Taxonomy(kind string) string {
	return k.join("taxonomy", kind)
}

// RateLimit 返回限流计数键。
func (k KeyBuilder) RateLimit(scope, identity string) string {
	return k.join("ratelimit", scope, identity)
}

func (k KeyBuilder) join(parts ...string) string {
	out := k.prefix
	for _, p := range parts {
		out += p + ":"
	}
	return out[:len(out)-1] // 去掉末尾 ":"
}
