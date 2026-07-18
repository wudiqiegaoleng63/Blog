// Package httpserver 封装 Gin 引擎装配、中间件与统一响应。
//
// 仅 transport 层依赖 Gin；业务模块不应 import 本包之外的 Gin 类型。
// 中间件顺序：RequestID -> Recovery -> RequestLog -> CORS -> ErrorHandler(尾部)。
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/lsy/blog/internal/config"
	"github.com/lsy/blog/internal/platform/observability"
	"github.com/lsy/blog/internal/shared/apperr"
)

// Engine 持有装配好的 Gin 引擎与依赖。
type Engine struct {
	cfg     *config.Config
	logger  *observability.Logger
	metrics *observability.Metrics
	router  *gin.Engine
}

// New 创建 Gin 引擎并注册全局中间件。路由由各模块通过 Register 注册。
func New(cfg *config.Config, logger *observability.Logger) (*Engine, error) {
	if cfg == nil {
		return nil, errors.New("httpserver: config is nil")
	}
	if logger == nil {
		return nil, errors.New("httpserver: logger is nil")
	}

	gin.SetMode(ginMode(cfg))
	r := gin.New()

	// 受信任代理：生产必须显式配置，开发环境默认信任本地回环。
	trusted := cfg.HTTP.TrustedProxies
	if len(trusted) == 0 {
		trusted = []string{"127.0.0.0/8", "::1/128"}
	}
	if err := r.SetTrustedProxies(trusted); err != nil {
		return nil, err
	}

	metrics := observability.NewMetrics()
	r.Use(
		RequestID(cfg.Observability.RequestIDHeader),
		Recovery(logger),
		RequestMetrics(logger, metrics),
		RequestTimeout(cfg.App.RequestTimeout),
		CORS(cfg.CORS),
		MaxBody(cfg.HTTP.MaxBodyBytes),
		ErrorHandler(logger),
	)

	r.NoRoute(func(c *gin.Context) {
		c.Error(apperr.NotFound("route not found"))
	})

	return &Engine{cfg: cfg, logger: logger, metrics: metrics, router: r}, nil
}

// Router 返回底层 *gin.Engine，供模块注册路由。
func (e *Engine) Router() *gin.Engine { return e.router }

// Metrics returns the process metrics registry used by HTTP and AI callers.
func (e *Engine) Metrics() *observability.Metrics { return e.metrics }

// Serve 启动 HTTP 服务并在收到 ctx 取消时优雅关闭。
func (e *Engine) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr:         e.cfg.HTTP.Addr,
		Handler:      e.router,
		ReadTimeout:  e.cfg.HTTP.ReadTimeout,
		WriteTimeout: e.cfg.HTTP.WriteTimeout,
		IdleTimeout:  e.cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		e.logger.Info("http server listening", "addr", srv.Addr, "env", e.cfg.App.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), e.cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		e.logger.Info("http server stopped")
		return nil
	case err := <-errCh:
		return err
	}
}

func ginMode(cfg *config.Config) string {
	switch cfg.App.Env {
	case "production":
		return gin.ReleaseMode
	default:
		return gin.DebugMode
	}
}

// --- 中间件 ---

// RequestID 为每个请求生成或复用 request_id，并写入 context 与响应头。
func RequestID(header string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(header)
		if id == "" {
			id = uuid.NewString()
		}
		c.Header(header, id)
		ctx := observability.WithRequestID(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Set("request_id", id)
		c.Next()
	}
}

// Recovery 捕获 panic，记录堆栈，返回统一内部错误，避免进程崩溃。
func Recovery(logger *observability.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.WithContext(c.Request.Context()).Error("panic recovered",
					"error", rec,
					"path", c.Request.URL.Path,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, ErrorResponse{
					Success:   false,
					RequestID: requestID(c),
					Error: ErrorBody{
						Code:    string(apperr.CodeInternal),
						Message: "an unexpected error occurred",
					},
				})
			}
		}()
		c.Next()
	}
}

// RequestMetrics records bounded route metrics and structured request logs.
func RequestMetrics(logger *observability.Logger, metrics *observability.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.ObserveHTTP(c.Request.Method, route, status, time.Since(start))
		logger.WithContext(c.Request.Context()).Info("http request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

// RequestLog is retained as a small compatibility wrapper for callers that
// only need structured request logs.
func RequestLog(logger *observability.Logger) gin.HandlerFunc {
	return RequestMetrics(logger, observability.NewMetrics())
}

// RequestTimeout bounds request-scoped downstream work. It does not forcibly
// stop handlers that ignore context cancellation.
func RequestTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if timeout <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// CORS 处理跨域。生产环境必须使用白名单 Origin。
func CORS(cfg config.CORSConfig) gin.HandlerFunc {
	allowed := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[strings.TrimSpace(o)] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			if cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ", "))
			c.Header("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
			c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
			c.Header("Access-Control-Max-Age", formatSeconds(cfg.MaxAge))
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Header("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ", "))
		c.Next()
	}
}

// MaxBody 限制请求体大小，防止异常大请求耗尽内存。
func MaxBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

// ErrorHandler 作为尾部中间件，统一将 c.Error() 中的 AppError 转换为响应体。
// 非 AppError 的错误统一映射为 internal，不泄露内部细节。
func ErrorHandler(logger *observability.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}
		last := c.Errors.Last().Err

		var ae *apperr.AppError
		if !errors.As(last, &ae) {
			// 记录原始错误用于排查，但对外只返回通用消息。
			logger.WithContext(c.Request.Context()).Error("unhandled error",
				"error", last.Error(),
				"path", c.Request.URL.Path,
			)
			ae = apperr.Internal(last, "")
		} else if ae.Cause != nil {
			logger.WithContext(c.Request.Context()).Error("request failed",
				"code", ae.Code,
				"message", ae.Message,
				"cause", ae.Cause.Error(),
			)
		}

		// 若 Handler 已经写过响应（例如流式），不要覆盖。
		if c.Writer.Written() {
			return
		}

		body := ErrorResponse{
			Success:   false,
			RequestID: requestID(c),
			Error: ErrorBody{
				Code:    string(ae.Code),
				Message: ae.Message,
				Details: ae.Details,
			},
		}
		c.AbortWithStatusJSON(ae.HTTPStatus, body)
	}
}

func formatSeconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}

func requestID(c *gin.Context) string {
	id, _ := c.Get("request_id")
	requestID, _ := id.(string)
	return requestID
}

// --- 响应结构 ---

// ErrorResponse 是对外统一的错误响应体。
type ErrorResponse struct {
	Success   bool      `json:"success"`
	RequestID string    `json:"request_id"`
	Error     ErrorBody `json:"error"`
}

// ErrorBody 描述错误细节。Details 仅用于字段级校验等安全信息。
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// DataResponse 是对外统一的成功响应体。
type DataResponse struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
}

// OK 写入 200 成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, DataResponse{Success: true, Data: data})
}

// Created 写入 201 成功响应。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, DataResponse{Success: true, Data: data})
}

// NoContent 写入 204 空响应。
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
