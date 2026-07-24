package context

import "errors"

// 错误 sentinel 位于 Context 包；配置校验错误仍使用 config.ValidationError。
var (
	ErrContextBuildFailed      = errors.New("context build failed")
	ErrContextConfigInvalid    = errors.New("context config invalid")
	ErrProviderWindowUnknown   = errors.New("provider model window unknown")
	ErrTokenEstimationFailed   = errors.New("input token estimation failed")
	ErrInvalidMessageSequence  = errors.New("invalid message sequence")
	ErrContextOverflow         = errors.New("context input exceeds budget")
	ErrCompressionFailed       = errors.New("context compression failed")
	ErrCompressionTimeout      = errors.New("context compression timed out")
)
