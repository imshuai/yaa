package storage

import (
	"errors"
	"fmt"
	"time"

	"unicode/utf8"
)

// MaxValueBytes 是单个 value 的最大字节数（16 MiB）。
const MaxValueBytes = 16 << 20

// 存储层错误。实现必须保证可用 errors.Is 判断。
var (
	ErrNotFound      = errors.New("storage: key not found")
	ErrClosed        = errors.New("storage: already closed")
	ErrInvalidKey    = errors.New("storage: invalid key")
	ErrInvalidTTL    = errors.New("storage: invalid ttl")
	ErrInvalidPath   = errors.New("storage: invalid path")
	ErrValueTooLarge = errors.New("storage: value too large")
)

// Storage 是根 KV 存储的统一抽象。调用开始后不可由调用方取消，因此不带 context。
type Storage interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte, ttl ...time.Duration) error
	Delete(key string) error
	Has(key string) (bool, error)
	Keys(prefix string) ([]string, error)
	Close() error
}

// Clock 提供可注入的当前时间，便于 memory 后端测试 TTL。
type Clock interface {
	Now() time.Time
}

// Stats 是可选观察接口，不作为业务依赖。
type Stats interface {
	Stats() StorageStats
}

// StorageStats 是可选的存储统计快照。
type StorageStats struct {
	KeyCount   int64
	TotalBytes int64
	HitCount   int64
	MissCount  int64
}

// validateKey 校验 key 非空、是合法 UTF-8 且不超过 512 字节。
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if len(key) > 512 {
		return fmt.Errorf("%w: key too long (%d bytes)", ErrInvalidKey, len(key))
	}
	if !isValidUTF8(key) {
		return fmt.Errorf("%w: key is not valid UTF-8", ErrInvalidKey)
	}
	return nil
}

// validateValue 校验 value 不超过 MaxValueBytes。
func validateValue(value []byte) error {
	if len(value) > MaxValueBytes {
		return fmt.Errorf("%w: %d bytes", ErrValueTooLarge, len(value))
	}
	return nil
}

// expiresAt 将可选 TTL 转换为绝对 Unix 纳秒过期时间。
// 未传或 0 表示永不过期；负值或多于一个 TTL 参数返回 ErrInvalidTTL。
func expiresAt(now time.Time, ttl []time.Duration) (*int64, error) {
	if len(ttl) > 1 {
		return nil, fmt.Errorf("%w: at most one ttl, got %d", ErrInvalidTTL, len(ttl))
	}
	if len(ttl) == 0 || ttl[0] == 0 {
		return nil, nil
	}
	if ttl[0] < 0 {
		return nil, fmt.Errorf("%w: negative ttl", ErrInvalidTTL)
	}
	v := now.Add(ttl[0]).UTC().UnixNano()
	return &v, nil
}

func isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}
