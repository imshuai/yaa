package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrorCode 是稳定的 Provider 错误分类。
type ErrorCode string

const (
	ErrCodeUnauthorized   ErrorCode = "unauthorized"
	ErrCodeForbidden      ErrorCode = "forbidden"
	ErrCodeRateLimit      ErrorCode = "rate_limit"
	ErrCodeServer         ErrorCode = "server"
	ErrCodeTimeout        ErrorCode = "timeout"
	ErrCodeInvalidRequest ErrorCode = "invalid_request"
	ErrCodeModelNotFound  ErrorCode = "model_not_found"
	ErrCodeContextLength  ErrorCode = "context_length"
	ErrCodeConnection     ErrorCode = "connection"
	ErrCodeUnknown        ErrorCode = "unknown"
)

// ProviderError 是 Provider 上游失败的稳定分类错误。
type ProviderError struct {
	Code       ErrorCode
	Message    string
	StatusCode int
	Retryable  bool
	RetryAfter time.Duration
	ProviderID string
	Cause      error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "provider: nil error"
	}
	msg := e.Message
	if msg == "" {
		msg = string(e.Code)
	}
	if e.ProviderID != "" {
		return fmt.Sprintf("provider %s: %s: %s", e.ProviderID, e.Code, msg)
	}
	return fmt.Sprintf("provider: %s: %s", e.Code, msg)
}

// Unwrap 返回底层 cause（可能是 nil）。
func (e *ProviderError) Unwrap() error { return e.Cause }

// classifyHTTPStatus 按 HTTP 状态码与错误体推断分类与是否可重试。
func classifyHTTPStatus(providerID string, status int, body string) *ProviderError {
	perr := &ProviderError{StatusCode: status, ProviderID: providerID}
	switch {
	case status == 401:
		perr.Code, perr.Message = ErrCodeUnauthorized, "unauthorized"
	case status == 403:
		perr.Code, perr.Message = ErrCodeForbidden, "forbidden"
	case status == 429:
		perr.Code, perr.Message = ErrCodeRateLimit, "rate limit"
		perr.Retryable = true
	case status == 500 || status == 502 || status == 503 || status == 504:
		perr.Code, perr.Message = ErrCodeServer, fmt.Sprintf("upstream %d", status)
		perr.Retryable = true
	case status == 400:
		lc := strings.ToLower(body)
		switch {
		case strings.Contains(lc, "model") && strings.Contains(lc, "not found"):
			perr.Code, perr.Message = ErrCodeModelNotFound, "model not found"
		case strings.Contains(lc, "context") && (strings.Contains(lc, "length") || strings.Contains(lc, "window")):
			perr.Code, perr.Message = ErrCodeContextLength, "context length exceeded"
		default:
			perr.Code, perr.Message = ErrCodeInvalidRequest, "invalid request"
		}
	case status == 404:
		perr.Code, perr.Message = ErrCodeModelNotFound, "not found"
	default:
		perr.Code, perr.Message = ErrCodeUnknown, fmt.Sprintf("upstream %d", status)
	}
	return perr
}

// parseRetryAfter 解析 Retry-After 头（仅支持整数秒）。
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(h); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 0 // ponytail: 不解析 HTTP-date；Manager 对正数 RetryAfter 取 max(backoff, retryAfter)。
}

// connectionError 把 net/context 层错误分类为 timeout 或 connection。
func connectionError(providerID string, err error) *ProviderError {
	perr := &ProviderError{ProviderID: providerID, Cause: err}
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		perr.Code, perr.Message = ErrCodeTimeout, "request timeout or canceled"
	default:
		m := err.Error()
		if strings.Contains(m, "connection refused") || strings.Contains(m, "no such host") ||
			strings.Contains(m, "i/o timeout") || strings.Contains(m, "network is unreachable") {
			perr.Code, perr.Message = ErrCodeConnection, "connection error"
		} else {
			perr.Code, perr.Message = ErrCodeConnection, "connection error"
		}
		perr.Retryable = true
	}
	return perr
}
