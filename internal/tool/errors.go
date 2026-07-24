package tool

import (
	"errors"
)

// 稳定 sentinel 错误，由 Agent / Remote 复用错误映射。
var (
	ErrToolNotFound       = errors.New("tool not found")
	ErrToolDisabled       = errors.New("tool disabled")
	ErrPermissionDenied   = errors.New("tool permission denied")
	ErrInvalidParams      = errors.New("invalid tool arguments")
	ErrToolTimeout        = errors.New("tool execution timed out")
	ErrToolAliasCollision = errors.New("tool provider alias collision")
	ErrInvalidToolName    = errors.New("invalid tool name")
	ErrInvalidToolDef     = errors.New("invalid tool definition")
)

// ValidationError 携带脱敏的 JSON Schema 字段路径与失败 keyword。
// Error() 不含被拒绝的原始值。
type ValidationError struct {
	Path    string
	Keyword string
}

func (e *ValidationError) Error() string { return "invalid tool arguments" }
func (e *ValidationError) Unwrap() error { return ErrInvalidParams }

// RetryableError 是 Tool opt-in 表明「尚未副作用、可安全重试」的接口。
type RetryableError interface {
	error
	Retryable() bool
}
