// Package observability 提供结构化日志、请求 ID 与可观测基础设施。
//
// 使用标准库 log/slog，避免引入额外依赖。日志必须结构化、可脱敏，
// 并在每个请求/任务上携带 request_id 或 job_id。
package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/lsy/blog/internal/config"
)

// ctxKey 是包私有 context key 类型，避免与其他包冲突。
type ctxKey struct{}

// requestIDKey 用于在 context 中存取 request_id。
var requestIDKey ctxKey

// Logger 是 slog.Logger 的薄封装，便于后续替换实现或接入 OpenTelemetry。
type Logger struct {
	*slog.Logger
}

// New 根据配置创建日志器。
func New(cfg config.ObservabilityConfig) *Logger {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// 全局默认日志器，便于不显式传 logger 的标准库代码。
	slog.SetDefault(slog.New(handler))
	return &Logger{Logger: slog.New(handler)}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithContext 返回携带 request_id 字段的日志器。
// 若 context 中没有 request_id，则原样返回。
func (l *Logger) WithContext(ctx context.Context) *slog.Logger {
	if l == nil || l.Logger == nil {
		return slog.Default()
	}
	if id, ok := RequestIDFromContext(ctx); ok && id != "" {
		return l.Logger.With("request_id", id)
	}
	return l.Logger
}

// --- request id context ---

// WithRequestID 将 request_id 注入 context。
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext 从 context 取出 request_id。
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}
