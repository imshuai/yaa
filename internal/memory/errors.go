package memory

import "errors"

// Memory 包的 17 个 sentinel（docs/memory/errors.md §1）。
// 底层错误以 %w 包装，不转换为空 slice/nil。
var (
	ErrMemoryDisabled           = errors.New("memory: disabled")
	ErrMemoryClosed             = errors.New("memory: closed")
	ErrMemoryNotFound           = errors.New("memory: item not found")
	ErrMemoryInvalidScope       = errors.New("memory: invalid scope")
	ErrMemoryInvalidItem        = errors.New("memory: invalid item")
	ErrMemoryManagedField       = errors.New("memory: managed field is not writable")
	ErrMemoryUnsupportedLayer   = errors.New("memory: unsupported layer")
	ErrMemoryExpiredInput       = errors.New("memory: expiration is in the past")
	ErrMemoryQuota              = errors.New("memory: capacity could not be satisfied")
	ErrMemoryStoreUnavailable   = errors.New("memory: content store unavailable")
	ErrMemoryCorrupt            = errors.New("memory: corrupt content")
	ErrMemoryEmbeddingFailed    = errors.New("memory: embedding failed")
	ErrMemoryEmbeddingDimension = errors.New("memory: embedding dimension mismatch")
	ErrMemoryEmbeddingZero      = errors.New("memory: zero vector")
	ErrMemoryIndexUnavailable   = errors.New("memory: vector index unavailable")
	ErrMemoryIndexDegraded      = errors.New("memory: vector index degraded")
	ErrMemoryReindexFailed      = errors.New("memory: reindex failed")
)
