// Package apperr 定义应用层统一错误类型与错误码。
//
// 设计目标：
//   - 让 HTTP 层能从错误推导出 HTTP 状态码与对外错误体，而不把内部细节泄露给客户端。
//   - 让业务层只关心错误语义（例如 ErrNotFound、ErrUnauthorized），不关心 HTTP。
//   - 让日志能携带错误码与堆栈信息，便于排查。
//
// 不在此处依赖 Gin 或任何 HTTP 框架。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code 是对外暴露的错误码字符串，必须稳定。
type Code string

// 预定义错误码。新增时遵循 <域>.<原因> 的命名习惯。
const (
	CodeInternal           Code = "internal"
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeValidation         Code = "validation"
	CodeRateLimited        Code = "rate_limited"
	CodePayloadTooLarge    Code = "payload_too_large"
	CodeUnsupportedMedia   Code = "unsupported_media_type"
	CodeServiceUnavailable Code = "service_unavailable"
	CodeAINotEnabled       Code = "ai_not_enabled"
	CodeAIUnavailable      Code = "ai_unavailable"
)

// AppError 是应用层错误的统一结构。实现 error 与 Unwrap 接口。
type AppError struct {
	Code       Code
	Message    string // 对外可见的安全消息。
	HTTPStatus int    // 推导出的 HTTP 状态码。
	Cause      error  // 内部根因，不会泄露给客户端。
	Details    any    // 可选的额外结构化细节（字段级校验等）。
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

// WithDetails attaches safe, structured response details and returns the same error.
func (e *AppError) WithDetails(details any) *AppError {
	e.Details = details
	return e
}

// New 构造一个无根因的 AppError。
func New(code Code, message string, httpStatus int) *AppError {
	return &AppError{Code: code, Message: message, HTTPStatus: httpStatus}
}

// Wrap 构造一个带根因的 AppError，便于日志记录根因。
func Wrap(code Code, message string, httpStatus int, cause error) *AppError {
	return &AppError{Code: code, Message: message, HTTPStatus: httpStatus, Cause: cause}
}

// --- 常用构造器 ---

func Unauthorized(message string) *AppError {
	if message == "" {
		message = "authentication required"
	}
	return New(CodeUnauthorized, message, http.StatusUnauthorized)
}

func Forbidden(message string) *AppError {
	if message == "" {
		message = "permission denied"
	}
	return New(CodeForbidden, message, http.StatusForbidden)
}

func NotFound(message string) *AppError {
	if message == "" {
		message = "resource not found"
	}
	return New(CodeNotFound, message, http.StatusNotFound)
}

func Conflict(message string) *AppError {
	if message == "" {
		message = "resource conflict"
	}
	return New(CodeConflict, message, http.StatusConflict)
}

func Validation(message string, details any) *AppError {
	return &AppError{
		Code:       CodeValidation,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
		Details:    details,
	}
}

func RateLimited(message string) *AppError {
	if message == "" {
		message = "too many requests"
	}
	return New(CodeRateLimited, message, http.StatusTooManyRequests)
}

func Internal(cause error, message string) *AppError {
	if message == "" {
		message = "an unexpected error occurred"
	}
	return Wrap(CodeInternal, message, http.StatusInternalServerError, cause)
}

func ServiceUnavailable(message string) *AppError {
	if message == "" {
		message = "service temporarily unavailable"
	}
	return New(CodeServiceUnavailable, message, http.StatusServiceUnavailable)
}

func AINotEnabled() *AppError {
	return New(CodeAINotEnabled, "AI question answering is not enabled", http.StatusServiceUnavailable)
}

func AIUnavailable(cause error) *AppError {
	return Wrap(CodeAIUnavailable, "AI question answering is temporarily unavailable", http.StatusServiceUnavailable, cause)
}

// As 将任意 error 解析为 *AppError。
// 链中存在 AppError 时返回它，否则返回 nil。
func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
