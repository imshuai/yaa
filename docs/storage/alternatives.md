# 存储替代方案：BoltDB 与内存存储

> 本文档描述 Yaa! Storage 接口的两种可选实现：BoltDB (bbolt) 和内存存储。
> SQLite 仍为默认实现，这两种方案适用于不同场景。

---

## 1. Storage 接口回顾

```go
type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte, ttl ...time.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Keys(prefix string) ([]string, error)
}
```

所有实现需满足该接口，通过配置 `runtime.storage.type` 切换。

---

## 2. BoltDB (bbolt) 实现

### 2.1 概述

BoltDB（社区维护分支 [bbolt](https://github.com/etcd-io/bbolt)）是一个纯 Go 编写的
嵌入式 Key-Value 数据库，数据存储在单个文件中，支持 ACID 事务。

### 2.2 适用场景

- 嵌入式 / 边缘部署，存储以 KV 为主
- 不需要 SQL 查询，只需简单 KV 存取
- 写多读少场景（BoltDB 写性能优于 SQLite）
- 追求极简二进制，不想引入 SQLite 依赖

### 2.3 实现

```go
package storage

import (
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
)

var _ Storage = (*BoltStorage)(nil)

type BoltStorage struct {
	db   *bolt.DB
	opts Options
}

type Options struct {
	Path       string
	BucketName string
	Timeout    time.Duration
}

func NewBoltStorage(opts Options) (*BoltStorage, error) {
	if opts.Path == "" {
		return nil, errors.New("boltdb path is required")
	}
	if opts.BucketName == "" {
		opts.BucketName = "default"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	db, err := bolt.Open(opts.Path, 0o600, &bolt.Options{Timeout: opts.Timeout})
	if err != nil {
		return nil, err
	}
	// 创建默认 Bucket
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(opts.BucketName))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &BoltStorage{db: db, opts: opts}, nil
}

func (s *BoltStorage) Get(key string) ([]byte, error) {
	var val []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.opts.BucketName))
		v := b.Get([]byte(key))
		if v == nil {
			return ErrNotFound
		}
		val = make([]byte, len(v))
		copy(val, v) // 必须拷贝，bolt 事务结束后 v 失效
		return nil
	})
	return val, err
}

func (s *BoltStorage) Set(key string, value []byte, ttl ...time.Duration) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.opts.BucketName))
		return b.Put([]byte(key), value)
	})
}

func (s *BoltStorage) Delete(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.opts.BucketName))
		return b.Delete([]byte(key))
	})
}

func (s *BoltStorage) Has(key string) (bool, error) {
	var exists bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.opts.BucketName))
		exists = b.Get([]byte(key)) != nil
		return nil
	})
	return exists, err
}

func (s *BoltStorage) Keys(prefix string) ([]string, error) {
	var keys []string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(s.opts.BucketName))
		c := b.Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && len(prefix) == 0 || hasPrefix(k, prefix); k, _ = c.Next() {
			keys = append(keys, string(k))
		}
		return nil
	})
	return keys, err
}

func (s *BoltStorage) Close() error {
	return s.db.Close()
}

func hasPrefix(k []byte, prefix string) bool {
	if len(k) < len(prefix) {
		return false
	}
	return string(k[:len(prefix)]) == prefix
}
```

> **注意**：BoltDB 不支持原生 TTL，需在应用层实现过期逻辑（如后台清理协程）。

---

## 3. 内存存储实现

### 3.1 概述

内存存储将所有数据保存在内存中，进程退出即丢失。主要用于单元测试和临时运行场景。

### 3.2 适用场景

- 单元测试 / 集成测试
- 短期临时任务（CLI 工具、一次性脚本）
- 开发调试时快速验证

### 3.3 实现

```go
package storage

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var _ Storage = (*MemoryStorage)(nil)

type memItem struct {
	value   []byte
	expires time.Time // zero 表示无过期
}

type MemoryStorage struct {
	mu   sync.RWMutex
	data map[string]*memItem
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{data: make(map[string]*memItem)}
}

func (s *MemoryStorage) Get(key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.data[key]
	if !ok || item.expired() {
		return nil, ErrNotFound
	}
	return item.value, nil
}

func (s *MemoryStorage) Set(key string, value []byte, ttl ...time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := &memItem{value: value}
	if len(ttl) > 0 && ttl[0] > 0 {
		item.expires = time.Now().Add(ttl[0])
	}
	s.data[key] = item
	return nil
}

func (s *MemoryStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *MemoryStorage) Has(key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.data[key]
	return ok && !item.expired(), nil
}

func (s *MemoryStorage) Keys(prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var keys []string
	for k, item := range s.data {
		if item.expired() {
			continue
		}
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *memItem) expired() bool {
	return !m.expires.IsZero() && time.Now().After(m.expires)
}
```

---

## 4. 三种实现对比

| 特性 | SQLite | BoltDB (bbolt) | 内存存储 |
|------|--------|-----------------|----------|
| **存储介质** | 磁盘文件 | 磁盘文件 | 内存 |
| **数据持久化** | ✅ | ✅ | ❌ |
| **查询能力** | SQL 全功能 | 仅 KV | 仅 KV |
| **事务支持** | ✅ | ✅ ACID | ❌ |
| **TTL 原生支持** | ❌（需应用层） | ❌（需应用层） | ✅（内置） |
| **并发模型** | 文件锁 + WAL | 读写锁 | 读写锁 |
| **读性能** | 高 | 高 | 极高 |
| **写性能** | 中 | 高 | 极高 |
| **二进制增量** | ~5MB | ~1MB | 0 |
| **CGO 依赖** | 无 (modernc.org/sqlite) | 无 | 无 |
| **Windows 7 兼容** | ✅ | ✅ | ✅ |
| **适用规模** | 中大型 | 中小型 | 极小 |
| **推荐场景** | 通用默认 | KV 密集 / 极简部署 | 测试 / 临时任务 |

---

## 5. 配置方式

```yaml
runtime:
  storage:
    # SQLite（默认）
    type: sqlite
    path: ./data/yaa.db

    # BoltDB
    # type: boltdb
    # path: ./data/yaa.bolt
    # bucket: default
    # timeout: 5s

    # 内存存储
    # type: memory
```

```go
// config/storage.go
type StorageConfig struct {
	Type   string `yaml:"type"`
	Path   string `yaml:"path"`
	Bucket string `yaml:"bucket"`
	Timeout time.Duration `yaml:"timeout"`
}

func NewStorage(cfg StorageConfig) (Storage, error) {
	switch cfg.Type {
	case "sqlite":
		return NewSQLiteStorage(cfg)
	case "boltdb":
		return NewBoltStorage(Options{
			Path:       cfg.Path,
			BucketName: cfg.Bucket,
			Timeout:    cfg.Timeout,
		})
	case "memory":
		return NewMemoryStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
```

---

## 6. 选型建议

| 场景 | 推荐 |
|------|------|
| 通用 Runtime 部署 | SQLite（默认） |
| 嵌入式 / IoT / 极简二进制 | BoltDB |
| 单元测试 / CI | 内存存储 |
| 需要复杂查询（JOIN / 聚合） | SQLite |
| 纯 KV 存储，高写入吞吐 | BoltDB |
| 临时 CLI 工具 | 内存存储 |

所有实现共享同一 `Storage` 接口，切换只需修改配置，无需改代码。
