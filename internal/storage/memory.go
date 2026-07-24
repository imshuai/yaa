package storage

import (
	"bytes"
	"sort"
	"strings"
	"sync"
	"time"
)

type memItem struct {
	data      []byte
	createdAt int64
	expiresAt *int64
}

func (m memItem) expired(now time.Time) bool {
	return m.expiresAt != nil && now.UnixNano() >= *m.expiresAt
}

// MemoryStorage 是纯内存根 KV 后端，实现同一 Storage 接口；多用于测试和临时运行。
type MemoryStorage struct {
	clock  Clock
	mu     sync.RWMutex
	closed bool
	values map[string]memItem

	stop chan struct{}
	done chan struct{}

	closeOnce sync.Once
	clockOnce sync.Once
	hasClock  bool
}

// NewMemory 构造内存后端，并启动 60s 后台清理 worker。
// clock 可为 nil（使用真实时间）；测试可注入 fakeClock 以可控推进时间。
func NewMemory(clock Clock) (*MemoryStorage, error) {
	m := &MemoryStorage{
		values: make(map[string]memItem),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	if clock != nil {
		m.clock = clock
		m.hasClock = true
	}
	go m.cleanupLoop()
	return m, nil
}

func (s *MemoryStorage) now() time.Time {
	if s.hasClock {
		return s.clock.Now()
	}
	return time.Now()
}

func (s *MemoryStorage) Get(key string) ([]byte, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	item, ok := s.values[key]
	if !ok || item.expired(s.now()) {
		// ponytail: 惰性清理用写锁开销低，这里直接降级一次写锁成本可接受；
		// 但为避免在读锁中调用 delete，本路径升级用独立 removeExpiredKey 逻辑。
		return nil, ErrNotFound
	}
	return bytes.Clone(item.data), nil
}

func (s *MemoryStorage) Set(key string, value []byte, ttl ...time.Duration) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateValue(value); err != nil {
		return err
	}
	exp, err := expiresAt(s.now(), ttl)
	if err != nil {
		return err
	}
	clone := bytes.Clone(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	prev, ok := s.values[key]
	createdAt := time.Now().UTC().UnixNano()
	if ok {
		createdAt = prev.createdAt
	}
	s.values[key] = memItem{data: clone, createdAt: createdAt, expiresAt: exp}
	return nil
}

func (s *MemoryStorage) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	delete(s.values, key)
	return nil
}

func (s *MemoryStorage) Has(key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return false, ErrClosed
	}
	item, ok := s.values[key]
	if !ok || item.expired(s.now()) {
		return false, nil
	}
	return true, nil
}

func (s *MemoryStorage) Keys(prefix string) ([]string, error) {
	if err := validateKey(prefix); err != nil && prefix != "" {
		// prefix 空串表示匹配所有 key；非空时按合法 key 校验。
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	now := s.now()
	keys := make([]string, 0)
	for k, item := range s.values {
		if item.expired(now) {
			continue
		}
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Close 标记 closed、停止并等待 worker。重复调用幂等成功。
func (s *MemoryStorage) Close() error {
	var firstErr error
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
		s.mu.Lock()
		s.closed = true
		s.values = nil
		s.mu.Unlock()
	})
	return firstErr
}

func (s *MemoryStorage) cleanupLoop() {
	defer close(s.done)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for {
				select {
				case <-s.stop:
					return
				default:
				}
				if s.cleanupExpired(1000) < 1000 {
					break
				}
			}
		case <-s.stop:
			return
		}
	}
}

// cleanupExpired 删除至多 batch 个已过期项，返回删除数量。
func (s *MemoryStorage) cleanupExpired(batch int) int {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0
	}
	removed := 0
	for k, item := range s.values {
		if item.expired(now) {
			delete(s.values, k)
			removed++
			if removed >= batch {
				return removed
			}
		}
	}
	return removed
}

// 确保编译期实现 Storage 接口。
var _ Storage = (*MemoryStorage)(nil)
