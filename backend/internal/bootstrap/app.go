// Package bootstrap 装配应用依赖、Gin 引擎与 API/Worker 生命周期。
//
// 本包是唯一同时接触配置、平台层与业务模块的位置，负责按依赖顺序
// 创建各组件并注册路由。业务模块之间不直接依赖，只通过注入的接口协作。
package bootstrap

import (
	"context"
	"errors"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	aimod "github.com/lsy/blog/internal/modules/ai"
	authmod "github.com/lsy/blog/internal/modules/auth"
	"github.com/lsy/blog/internal/modules/comments"
	"github.com/lsy/blog/internal/modules/operations"
	"github.com/lsy/blog/internal/modules/posts"
	"github.com/lsy/blog/internal/platform/cache"
	"github.com/lsy/blog/internal/platform/database"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/platform/observability"
	"github.com/lsy/blog/internal/platform/openaicompat"
	"github.com/lsy/blog/internal/platform/ratelimit"
)

// Container 持有应用运行期的共享依赖。
type Container struct {
	Cfg     *config.Config
	Logger  *observability.Logger
	DB      *gorm.DB
	Redis   *redis.Client
	Keys    cache.KeyBuilder
	engine  *httpserver.Engine
	mu      sync.Mutex
	stopped bool
}

// New 根据配置创建依赖容器，验证关键依赖连通性。
func New(ctx context.Context, cfg *config.Config) (*Container, error) {
	if cfg == nil {
		return nil, errors.New("bootstrap: config is nil")
	}

	logger := observability.New(cfg.Observability)

	db, err := database.New(ctx, cfg.MySQL, cfg.App.Env)
	if err != nil {
		return nil, err
	}

	// Redis 在 worker 模式下也用于限流和任务协调，仍需连接。
	var redisClient *redis.Client
	redisClient, err = cache.New(ctx, cfg.Redis)
	if err != nil {
		// Redis 是软依赖；保留客户端，服务恢复后 go-redis 可自动重连。
		logger.Warn("redis unavailable, running in degraded mode", "error", err)
	}

	c := &Container{
		Cfg:    cfg,
		Logger: logger,
		DB:     db,
		Redis:  redisClient,
		Keys:   cache.NewKeyBuilder(cfg.Redis),
	}

	switch cfg.App.ServiceMode {
	case "api":
		engine, err := httpserver.New(cfg, logger)
		if err != nil {
			_ = c.Close(context.Background())
			return nil, err
		}
		c.engine = engine
		if err := c.registerModules(); err != nil {
			_ = c.Close(context.Background())
			return nil, err
		}
	case "worker":
		// Worker 不监听 HTTP，不装配 Gin、中间件或 API 路由。
	default:
		_ = c.Close(context.Background())
		return nil, errors.New("bootstrap: unsupported service mode")
	}

	return c, nil
}

// registerModules 注册业务模块路由。后续阶段在此按顺序加入模块。
func (c *Container) registerModules() error {
	router := c.engine.Router()

	// 运维端点直接挂在根路径（/health/*）。
	ops := operations.NewModuleWithMetrics(c.Cfg, c.DB, c.Redis, c.engine.Metrics().Handler())
	ops.Register(router)

	// --- Stage 1: 认证与博客领域 ---

	authRepo := authmod.NewRepository(c.DB)
	authMW := authmod.RequireAuth(c.Cfg.Auth, authRepo)
	optionalAuthMW := authmod.OptionalAuth(c.Cfg.Auth, authRepo)
	adminMW := authmod.RequireAdmin(c.Cfg.Auth, authRepo)
	registerLimit := ratelimit.MiddlewareWithMetrics(c.Redis, c.Keys, "auth:register", c.Cfg.RateLimit.RegisterPerMinute, ratelimit.ByIP, c.engine.Metrics())
	loginLimit := ratelimit.MiddlewareWithMetrics(c.Redis, c.Keys, "auth:login", c.Cfg.RateLimit.LoginPerMinute, ratelimit.ByIP, c.engine.Metrics())
	refreshLimit := ratelimit.MiddlewareWithMetrics(c.Redis, c.Keys, "auth:refresh", c.Cfg.RateLimit.RefreshPerMinute, ratelimit.ByIP, c.engine.Metrics())

	authModule := authmod.NewModule(c.Cfg, authRepo)
	authModule.Register(router, authMW, registerLimit, loginLimit, refreshLimit)

	postsRepoOptions := []posts.RepositoryOption{}
	if c.Cfg.AI.IndexingEnabled {
		postsRepoOptions = append(postsRepoOptions, posts.WithIndexJobs(c.Cfg.Jobs.MaxAttempts))
	}
	postsRepo := posts.NewRepository(c.DB, postsRepoOptions...)
	postsModule := posts.NewModule(postsRepo)
	postsModule.Register(router, authMW, adminMW, optionalAuthMW)

	commentsRepo := comments.NewRepository(c.DB, c.Cfg.Jobs.MaxAttempts)
	commentsModule := comments.NewModule(commentsRepo, postsRepo)
	commentLimit := ratelimit.MiddlewareWithMetrics(c.Redis, c.Keys, "comment:write", c.Cfg.RateLimit.CommentPerMinute, ratelimit.ByUser, c.engine.Metrics())
	commentsModule.Register(router, authMW, commentLimit)

	if c.Cfg.AI.IndexingEnabled || c.Cfg.AI.RAGEnabled {
		aiRepo := aimod.NewRepository(c.DB, c.Cfg.Jobs.MaxAttempts)
		var rag *aimod.RAGService
		if c.Cfg.AI.RAGEnabled {
			embedder, err := openaicompat.NewWithMetrics(c.Cfg.AI.Embedding.BaseURL, c.Cfg.AI.Embedding.APIKey, c.Cfg.AI.Embedding.Timeout, c.Cfg.AI.Embedding.MaxRetries, c.engine.Metrics())
			if err != nil {
				return err
			}
			chat, err := openaicompat.NewWithMetrics(c.Cfg.AI.Chat.BaseURL, c.Cfg.AI.Chat.APIKey, c.Cfg.AI.Chat.Timeout, c.Cfg.AI.Chat.MaxRetries, c.engine.Metrics())
			if err != nil {
				return err
			}
			vectors := aimod.NewMilvusStoreWithMetrics(c.Cfg.Milvus, c.Cfg.AI.Embedding.Dimensions, c.engine.Metrics())
			rag = aimod.NewRAGService(c.DB, embedder, chat, vectors, c.Cfg.AI)
		}
		aiLimit := ratelimit.StrictMiddlewareWithMetrics(c.Redis, c.Keys, "ai:ask", c.Cfg.RateLimit.AIPerMinute, ratelimit.ByIP, c.engine.Metrics())
		aimod.NewModule(aiRepo, rag).Register(router, adminMW, aiLimit)
	}

	return nil
}

// Router 暴露底层 Gin 引擎，供测试或额外注册使用。
func (c *Container) Router() *gin.Engine {
	if c == nil || c.engine == nil {
		return nil
	}
	return c.engine.Router()
}

// ServeAPI 启动 HTTP 服务，ctx 取消时优雅关闭。
func (c *Container) ServeAPI(ctx context.Context) error {
	if c == nil || c.engine == nil || c.Cfg == nil || c.Cfg.App.ServiceMode != "api" {
		return errors.New("bootstrap: API server is not configured")
	}
	c.Logger.Info("starting api", "env", c.Cfg.App.Env, "ai_enabled", c.Cfg.AI.Enabled)
	return c.engine.Serve(ctx)
}

// Close 释放所有底层资源。
func (c *Container) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return nil
	}
	c.stopped = true

	var errs []error
	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.DB != nil {
		if err := database.Close(c.DB); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
