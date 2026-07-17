# Memory 存储后端设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/memory/storage.md`
> 依赖: `docs/memory/README.md` §2, `docs/architecture.md` §3.6

---

## 1. 概述

本文档描述 Memory 系统的存储后端实现，包括：

| 组件 | 职责 |
|------|------|
| **MemoryStore** | 存储接口定义，屏蔽底层差异 |
| **SQLite KV Store** | 基于嵌入式 SQLite 的键值存储 |
| **Embedder** | 向量嵌入生成（调用 Embedding 模型） |
| **VectorIndex** | 向量索引与相似度检索 |

### 1.1 存储分层

```
┌─────────────────────────────────────────────┐
│              Memory Interface               │
│         (Add / Get / Search / Delete)        │
├─────────────────────────────────────────────┤
│            MemoryStore (接口)                │
│    PutItem / GetItem / Delete / Scan        │
├──────────────┬──────────────────────────────┤
│  SQLite KV   │     VectorIndex (接口)       │
│  (内容/元数据) │  (Embedding 向量 + 检索)     │
├──────────────┼──────────────────────────────┤
│  sqlite3     │  Embedder (接口)             │
│  driver      │  ┌────────┬────────┐        │
│              │  │ OpenAI │ Ollama │  ...   │
│              │  └────────┴────────┘        │
└──────────────┴──────────────────────────────┘
```

---

## 2. 存储接口定义

### 2.1 MemoryStore

```go
// MemoryStore 是记忆存储的底层接口。
// 所有存储后端（SQLite、外部向量数据库等）均实现此接口。
type MemoryStore interface {
    // PutItem 写入或更新一条记忆。
    // key 已存在时覆盖。
    PutItem(ctx context.Context, item *MemoryItem) error

    // GetItem 按 key 读取单条记忆。
    // 不存在时返回 ErrMemoryNotFound。
    GetItem(ctx context.Context, agentID, key string) (*MemoryItem, error)

    // Delete 按 key 删除单条记忆。
    Delete(ctx context.Context, agentID, key string) error

    // Scan 列出指定 Agent 的记忆，支持分页。
    Scan(ctx context.Context, agentID string, limit, offset int) ([]*MemoryItem, error)

    // Clear 清除指定 Agent 的所有记忆。
    Clear(ctx context.Context, agentID string) error

    // Close 关闭存储，释放资源。
    Close() error
}
```

### 2.2 Embedder

```go
// Embedder 将文本转换为向量表示。
// 支持多种后端：OpenAI、Ollama、本地模型等。
type Embedder interface {
    // Embed 生成单条文本的向量。
    Embed(ctx context.Context, text string) ([]float32, error)

    // EmbedBatch 批量生成向量，提高吞吐。
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

    // Dim 返回向量维度。
    Dim() int
}
```

### 2.3 VectorIndex

```go
// VectorIndex 管理向量索引并支持相似度检索。
type VectorIndex interface {
    // Upsert 插入或更新一条向量。
    Upsert(ctx context.Context, agentID, key string, vector []float32) error

    // Search 按向量相似度检索，返回最相似的 topK 条目。
    Search(ctx context.Context, agentID string, query []float32, topK int) ([]VectorResult, error)

    // Remove 删除一条向量。
    Remove(ctx context.Context, agentID, key string) error

    // Close 关闭索引。
    Close() error
}

// VectorResult 是向量检索结果。
type VectorResult struct {
    Key   string  `json:"key"`
    Score float64 `json:"score"` // 余弦相似度 0-1
}
```

---

## 3. SQLite KV 存储

### 3.1 表结构

```sql
CREATE TABLE IF NOT EXISTS memory_items (
    agent_id    TEXT    NOT NULL,
    key         TEXT    NOT NULL,
    content     TEXT    NOT NULL,
    metadata    TEXT    DEFAULT '{}',   -- JSON
    layer       INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    expires_at  TEXT    DEFAULT '',
    PRIMARY KEY (agent_id, key)
);

CREATE INDEX IF NOT EXISTS idx_memory_layer
    ON memory_items (agent_id, layer);

CREATE INDEX IF NOT EXISTS idx_memory_content
    ON memory_items USING fts5(content);  -- SQLite FTS5 全文索引
```

### 3.2 实现

```go
// SQLiteStore 基于 SQLite 的 MemoryStore 实现。
type SQLiteStore struct {
    db    *sql.DB
    embed Embedder
    vi    VectorIndex
    log   *slog.Logger
}

// NewSQLiteStore 创建 SQLite 存储实例。
func NewSQLiteStore(dsn string, embed Embedder, vi VectorIndex) (*SQLiteStore, error) {
    db, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        return nil, fmt.Errorf("open sqlite: %w", err)
    }
    db.SetMaxOpenConns(1) // SQLite 写串行
    s := &SQLiteStore{db: db, embed: embed, vi: vi}
    if err := s.migrate(); err != nil {
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return s, nil
}

// PutItem 写入记忆，同时更新向量索引。
func (s *SQLiteStore) PutItem(ctx context.Context, item *MemoryItem) error {
    meta, _ := json.Marshal(item.Metadata)
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO memory_items
            (agent_id, key, content, metadata, layer, created_at, updated_at, expires_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(agent_id, key) DO UPDATE SET
            content   = excluded.content,
            metadata  = excluded.metadata,
            updated_at = excluded.updated_at`,
        item.AgentID, item.Key, item.Content, string(meta),
        item.Layer, item.CreatedAt, item.UpdatedAt, item.ExpiresAt)
    if err != nil {
        return fmt.Errorf("put item: %w", err)
    }

    // 同步更新向量索引（如已配置 Embedder）
    if s.embed != nil && s.vi != nil {
        vec, err := s.embed.Embed(ctx, item.Content)
        if err != nil {
            s.log.Warn("embed failed, skipping vector index", "key", item.Key, "err", err)
            return nil // 降级：内容已存储，向量缺失
        }
        return s.vi.Upsert(ctx, item.AgentID, item.Key, vec)
    }
    return nil
}
```

---

## 4. Embedding 生成与存储

### 4.1 流程

```
用户写入记忆 (Add)
      │
      ▼
┌─ SQLiteStore.PutItem ──┐
│  1. 内容写入 SQLite    │
│  2. Embedder.Embed     │──→ 向量
│  3. VectorIndex.Upsert │──→ 向量索引
└────────────────────────┘
      │
      ▼
   写入完成
```

### 4.2 OpenAI Embedder 示例

```go
// OpenAIEmbedder 调用 OpenAI Embedding API。
type OpenAIEmbedder struct {
    apiKey string
    model  string // "text-embedding-3-small"
    client *http.Client
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
    reqBody, _ := json.Marshal(map[string]any{
        "model": e.model,
        "input": text,
    })
    req, _ := http.NewRequestWithContext(ctx, "POST",
        "https://api.openai.com/v1/embeddings", bytes.NewReader(reqBody))
    req.Header.Set("Authorization", "Bearer "+e.apiKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := e.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("embed request: %w", err)
    }
    defer resp.Body.Close()

    var result struct {
        Data []struct {
            Embedding []float32 `json:"embedding"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode embedding: %w", err)
    }
    if len(result.Data) == 0 {
        return nil, ErrEmptyEmbedding
    }
    return result.Data[0].Embedding, nil
}

func (e *OpenAIEmbedder) Dim() int { return 1536 } // text-embedding-3-small
```

---

## 5. 向量索引 — 内存 HNSW

### 5.1 设计取舍

| 方案 | 优点 | 缺点 | Yaa! 选择 |
|------|------|------|-----------|
| 内存 HNSW | 检索快、零依赖 | 内存占用高 | ✅ 默认 |
| SQLite + sqlite-vec | 持久化、轻量 | 需扩展模块 | 可选 |
| 外部向量数据库 | 可扩展、分布式 | 运维复杂 | 可选 |

### 5.2 内存 HNSW 实现

```go
// HNSWIndex 基于 hnswlib 的内存向量索引。
type HNSWIndex struct {
    mu       sync.RWMutex
    indexes  map[string]*hnsw.Index // agentID → index
    dim      int
    maxElem  int
    efSearch int
}

func NewHNSWIndex(dim, maxElem, efSearch int) *HNSWIndex {
    return &HNSWIndex{
        indexes:  make(map[string]*hnsw.Index),
        dim:      dim, maxElem: maxElem, efSearch: efSearch,
    }
}

func (h *HNSWIndex) Upsert(ctx context.Context, agentID, key string, vec []float32) error {
    h.mu.Lock()
    defer h.mu.Unlock()
    idx, ok := h.indexes[agentID]
    if !ok {
        idx = hnsw.New(h.dim, h.maxElem, hnsw.M(16), hnsw.EfConstruction(200))
        h.indexes[agentID] = idx
    }
    return idx.Add(key, vec)
}

func (h *HNSWIndex) Search(ctx context.Context, agentID string, query []float32, topK int) ([]VectorResult, error) {
    h.mu.RLock()
    idx, ok := h.indexes[agentID]
    h.mu.RUnlock()
    if !ok {
        return nil, nil // 无记忆，空结果
    }
    return idx.Search(query, topK, h.efSearch)
}
```

### 5.3 检索降级策略

```
Search 请求
    │
    ├─ Embedder 可用？ ──→ 是 ──→ 生成 query 向量
    │                              │
    │                              ├─ VectorIndex.Search 成功 ──→ 返回语义结果
    │                              │
    │                              └─ VectorIndex.Search 失败 ──→ 降级 FTS
    │
    └─ Embedder 不可用 ──→ SQLite FTS5 全文检索（关键词匹配）
```

```go
// Search 实现：优先向量检索，失败降级全文搜索。
func (s *SQLiteStore) Search(ctx context.Context, agentID, query string, limit int) ([]*MemoryItem, error) {
    // 1. 尝试向量语义检索
    if s.embed != nil && s.vi != nil {
        qVec, err := s.embed.Embed(ctx, query)
        if err == nil {
            results, err := s.vi.Search(ctx, agentID, qVec, limit)
            if err == nil && len(results) > 0 {
                return s.loadByKeys(ctx, agentID, results)
            }
        }
        s.log.Warn("vector search failed, falling back to FTS", "err", err)
    }
    // 2. 降级：SQLite FTS5 全文检索
    rows, err := s.db.QueryContext(ctx, `
        SELECT key, content, metadata, layer, created_at, updated_at
        FROM memory_items
        WHERE agent_id = ? AND memory_items MATCH ?
        ORDER BY rank LIMIT ?`,
        agentID, query, limit)
    if err != nil {
        return nil, fmt.Errorf("fts search: %w", err)
    }
    defer rows.Close()
    return scanItems(rows)
}
```

---

## 6. 配置参考

```yaml
memory:
  store: sqlite                    # sqlite | external
  sqlite:
    dsn: "data/yaa_memory.db"
    journal_mode: WAL
  embedder:
    provider: openai               # openai | ollama | none
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    batch_size: 64
  vector_index:
    type: hnsw                     # hnsw | sqlite-vec | external
    dim: 1536
    max_elements: 100000
    ef_search: 50
    ef_construction: 200
```

---

*最后更新: 2026-07-17*
