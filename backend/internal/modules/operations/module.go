// Package operations 提供运维相关 HTTP 端点：健康检查与指标占位。
//
// /health/live 只检查进程存活；/health/ready 检查关键依赖可用性。
// 核心博客 Ready 不应被 Milvus 或 AI 服务故障拖垮。
package operations

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/httpserver"
	"github.com/lsy/blog/internal/shared/apperr"
)

// dependencyChecker 是 readiness 所需的最小依赖检查接口。
type dependencyChecker interface {
	Ping(context.Context) error
}

// Module 装配运维路由。
type Module struct {
	cfg     *config.Config
	mysql   dependencyChecker
	redis   dependencyChecker
	metrics http.Handler
}

type mysqlChecker struct {
	db *gorm.DB
}

func (c mysqlChecker) Ping(ctx context.Context) error {
	if c.db == nil {
		return errors.New("mysql client is nil")
	}
	sqlDB, err := c.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

type redisChecker struct {
	client *redis.Client
}

func (c redisChecker) Ping(ctx context.Context) error {
	if c.client == nil {
		return errors.New("redis client is nil")
	}
	return c.client.Ping(ctx).Err()
}

// NewModule 构造运维模块。
func NewModule(cfg *config.Config, db *gorm.DB, redisClient *redis.Client) *Module {
	return newModule(cfg, mysqlChecker{db: db}, redisChecker{client: redisClient})
}

func NewModuleWithMetrics(cfg *config.Config, db *gorm.DB, redisClient *redis.Client, metrics http.Handler) *Module {
	module := newModule(cfg, mysqlChecker{db: db}, redisChecker{client: redisClient})
	module.metrics = metrics
	return module
}

func newModule(cfg *config.Config, mysql, redis dependencyChecker) *Module {
	return &Module{cfg: cfg, mysql: mysql, redis: redis}
}

// Register 在指定路由组上注册运维端点。
func (m *Module) Register(r *gin.Engine) {
	r.GET("/health/live", m.live)
	r.GET("/health/ready", m.ready)
	if m.metrics != nil {
		r.GET("/metrics", gin.WrapH(m.metrics))
	}
}

func (m *Module) live(c *gin.Context) {
	httpserver.OK(c, gin.H{"status": "alive"})
}

func (m *Module) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	var mysqlErr, redisErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if m.mysql == nil {
			mysqlErr = errors.New("mysql checker is nil")
			return
		}
		mysqlErr = m.mysql.Ping(ctx)
	}()
	go func() {
		defer wg.Done()
		if m.redis == nil {
			redisErr = errors.New("redis checker is nil")
			return
		}
		redisErr = m.redis.Ping(ctx)
	}()
	wg.Wait()

	status := http.StatusOK
	checks := gin.H{}

	// MySQL 是硬依赖：不可用则 ready 失败。
	if mysqlErr != nil {
		checks["mysql"] = "down"
		status = http.StatusServiceUnavailable
	} else {
		checks["mysql"] = "ok"
	}

	// Redis 是软依赖：不可用标记 degraded，但不阻塞 ready。
	if redisErr != nil {
		checks["redis"] = "degraded"
	} else {
		checks["redis"] = "ok"
	}

	// AI 依赖由 AI_ENABLED 控制；未启用时标记 disabled。
	if m.cfg.AI.Enabled {
		checks["ai"] = "enabled"
	} else {
		checks["ai"] = "disabled"
	}

	if status != http.StatusOK {
		c.Error(apperr.ServiceUnavailable("required dependencies are unavailable").WithDetails(gin.H{
			"status": "degraded",
			"checks": checks,
		}))
		return
	}
	httpserver.OK(c, gin.H{"status": "ready", "checks": checks})
}
