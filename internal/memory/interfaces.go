package memory

import (
	"context"
	"time"
)

// ContentStore 是 Memory 内部接口，不等同于根 storage.Storage KV 接口
// （docs/memory/architecture.md §3）。
//
// 契约见 architecture.md；CommitPut 是唯一 commit point；
// 索引失败不调用 ContentStore 回滚。
type ContentStore interface {
	CommitPut(ctx context.Context, item MemoryItem, victims []ItemRef, now time.Time) (CommitPutResult, error)
	Get(ctx context.Context, scope Scope, key string) (MemoryItem, error)
	Search(ctx context.Context, req SearchRequest, now time.Time) ([]MemoryItem, error)
	List(ctx context.Context, scope Scope, now time.Time) ([]MemoryItem, error)
	Delete(ctx context.Context, scope Scope, key string) (MemoryItem, error)
	Clear(ctx context.Context, scope Scope) ([]MemoryItem, error)
	DeleteExpired(ctx context.Context, before time.Time, limit int) ([]MemoryItem, error)
	Count(ctx context.Context, agentID string, now time.Time) (int, error)
	Ping(ctx context.Context) error
	Close() error
}

// Embedder 是 v1 唯一的嵌入器接口（architecture.md §4）。
// Dimension 在构造时固定；每次 Embed 返回该长度的向量。
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Dimension() int
}

// VectorIndex 是进程内向量检索索引接口（architecture.md §4）。
type VectorIndex interface {
	Upsert(ctx context.Context, ref ItemRef, vector []float32) error
	Delete(ctx context.Context, ref ItemRef) error
	Search(ctx context.Context, req VectorSearchRequest) ([]VectorHit, error)
}

// VectorIndexFactory 每次返回新的非 nil 空 VectorIndex；不可复用当前 index 或跨 Agent 共享
// （architecture.md §2）。
type VectorIndexFactory func() VectorIndex

// Clock 是 Manager 内的唯一时钟源（每次 mutation 从该 Clock 取一次 now）。
type Clock interface {
	Now() time.Time
}

// SystemClock 是默认 Clock 实现，包装 time.Now().UTC()。
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
