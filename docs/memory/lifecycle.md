# 记忆生命周期管理

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/memory/lifecycle.md`
> 依赖: `docs/memory/README.md` §2, `docs/memory/architecture.md`, `docs/memory/storage.md`

---

## 1. 概述

记忆生命周期管理是 Memory 系统的核心子系统，负责管理一条记忆从创建到销毁的全过程。Yaa! 的记忆不是永久堆积的——每条记忆都有明确的状态流转、过期机制和淘汰策略，确保记忆库保持高信噪比。

### 1.1 生命周期状态机

```
                         ┌────────────────────────────────────────────┐
                         │                                            │
                         ▼                                            │
                   ┌──────────┐    Promote     ┌──────────┐          │
        Add  ───▶  │ ShortTerm │ ────────────▶ │ LongTerm │          │
                   └──────────┘                └──────────┘          │
                        │                          │                │
                        │ Expire                   │ Expire          │
                        │ / Evict                  │ / Evict         │
                        ▼                          ▼                │
                   ┌──────────┐              ┌──────────┐            │
                   │ Expired  │ ◀─────────── │  Deleted │            │
                   └──────────┘              └──────────┘            │
                        │                          │                │
                        └──────────┬───────────────┘                │
                                   ▼                                 │
                              ┌──────────┐                            │
                              │  Cleared │ (物理删除 / 归档)          │
                              └──────────┘                            │
                                                                   │
                         ┌───────────────────────────────────────────┘
                         │
                         │  Context Manager 压缩
                         ▼
                   ┌──────────┐
                   │ Summary  │ ──▶ (同 LongTerm 的 Expire/Evict 路径)
                   └──────────┘
```

### 1.2 状态定义

| 状态 | 说明 | 触发条件 |
|------|------|----------|
| **ShortTerm** | 短期记忆，存在于当前 Session 内 | `Add()` 写入，Layer 为 `LayerShortTerm` |
| **LongTerm** | 长期记忆，跨 Session 持久化 | `Promote()` 晋升或直接 `Add()` 到长期层 |
| **Summary** | 摘要记忆，Session 压缩产物 | Context Manager 触发压缩时生成 |
| **Expired** | 已过期，等待清理 | `ExpiresAt` 时间到达 |
| **Deleted** | 已标记删除 | `Delete()` 手动删除 |
| **Cleared** | 物理删除或归档 | `Expire()` 清理任务或 `Clear()` 批量清除 |

---

## 2. 创建 (Create)

### 2.1 创建流程

记忆创建通过 `Memory.Add()` 完成。创建时需要指定层级、内容和可选的 TTL。

```go
// AddWithTTL 创建一条带过期时间的记忆。
// ttl 为 0 表示永不过期。
func (m *memoryImpl) AddWithTTL(key, content string, layer MemoryLayer, ttl time.Duration, metadata map[string]any) error {
    now := time.Now()
    item := &MemoryItem{
        Key:       key,
        Content:   content,
        Metadata:  metadata,
        Layer:     layer,
        CreatedAt: now,
        UpdatedAt: now,
    }
    if ttl > 0 {
        item.ExpiresAt = now.Add(ttl)
    }

    // 写入存储
    if err := m.store.Put(item); err != nil {
        return fmt.Errorf("memory add failed: %w", err)
    }

    // 异步生成向量嵌入（如果配置了 Embedder）
    if m.embedder != nil {
        go m.embedAndIndex(key, content, layer)
    }

    m.logger.Info("memory added",
        "key", key,
        "layer", layer,
        "ttl", ttl,
    )
    m.metrics.AddCounter("memory_add_total", 1)
    return nil
}
```

### 2.2 TTL 配置

| 层级 | 默认 TTL | 说明 |
|------|----------|------|
| ShortTerm | Session 生命周期 | Session 关闭时晋升或清除 |
| LongTerm | 永不过期（零值） | 依赖淘汰策略控制容量 |
| Summary | 30 天 | 可按 Agent 配置覆盖 |

```go
type TTLConfig struct {
    ShortTermTTL time.Duration `json:"short_term_ttl"` // 默认 0（随 Session）
    LongTermTTL  time.Duration `json:"long_term_ttl"`  // 默认 0（永不过期）
    SummaryTTL   time.Duration `json:"summary_ttl"`    // 默认 720h (30天)
}
```

---

## 3. 检索 (Retrieve)

检索是记忆生命周期中最频繁的读操作。详见 `storage.md` 的向量搜索设计，此处仅列出与生命周期相关的要点。

```go
// Search 检索记忆，自动过滤已过期项。
func (m *memoryImpl) Search(query string, limit int) ([]*MemoryItem, error) {
    results, err := m.store.Search(query, limit)
    if err != nil {
        // 降级：向量搜索失败 → 关键词搜索
        m.logger.Warn("vector search failed, falling back to keyword", "err", err)
        results, err = m.store.KeywordSearch(query, limit)
        if err != nil {
            return nil, fmt.Errorf("memory search failed: %w", err)
        }
    }

    // 过滤已过期项（惰性过期检查）
    now := time.Now()
    filtered := make([]*MemoryItem, 0, len(results))
    for _, item := range results {
        if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
            m.scheduleExpiry(item.Key)
            continue
        }
        filtered = append(filtered, item)
    }
    return filtered, nil
}
```

---

## 4. 更新 (Update)

更新操作修改已有记忆的内容和元数据，同时刷新 `UpdatedAt`。

```go
// Update 更新已有记忆的内容和元数据。
func (m *memoryImpl) Update(key string, content string, metadata map[string]any) error {
    item, err := m.store.Get(key)
    if err != nil {
        return err // ErrMemoryNotFound
    }

    item.Content = content
    if metadata != nil {
        // 合并元数据，而非覆盖
        for k, v := range metadata {
            item.Metadata[k] = v
        }
    }
    item.UpdatedAt = time.Now()

    if err := m.store.Put(item); err != nil {
        return fmt.Errorf("memory update failed: %w", err)
    }

    // 内容变更后重新嵌入
    if m.embedder != nil {
        go m.embedAndIndex(key, content, item.Layer)
    }

    m.logger.Info("memory updated", "key", key)
    return nil
}
```

---

## 5. 晋升 (Promote)

短期记忆在 Session 关闭或达到条件时，可晋升为长期记忆。

### 5.1 晋升条件

| 条件 | 说明 |
|------|------|
| Session 关闭 | 默认策略：用户偏好、关键事实自动晋升 |
| 手动触发 | 调用 `Promote(key)` 显式晋升 |
| 重要性评分 | 基于访问频率和内容长度自动评估 |
| 显式标记 | Metadata 中 `promote: true` 的记忆自动晋升 |

```go
// Promote 将短期记忆晋升为长期记忆。
func (m *memoryImpl) Promote(key string) error {
    item, err := m.store.Get(key)
    if err != nil {
        return err
    }

    if item.Layer == LayerLongTerm {
        return nil // 已经是长期记忆，幂等返回
    }

    item.Layer = LayerLongTerm
    item.UpdatedAt = time.Now()
    // 晋升后清除过期时间（长期记忆默认永不过期）
    item.ExpiresAt = time.Time{}

    if err := m.store.Put(item); err != nil {
        return fmt.Errorf("memory promote failed: %w", err)
    }

    m.logger.Info("memory promoted", "key", key, "from", item.Layer)
    m.metrics.AddCounter("memory_promote_total", 1)
    m.emitEvent("memory.promoted", key)
    return nil
}
```

---

## 6. 遗忘 (Forget)

遗忘机制是记忆系统的"垃圾回收"，防止记忆库无限膨胀。

### 6.1 过期清理

过期清理通过后台定时任务执行，扫描并删除 `ExpiresAt` 已到期的记忆。

```go
// expirySweeper 后台过期清理任务。
func (m *MemoryManager) expirySweeper(ctx context.Context) {
    ticker := time.NewTicker(m.config.ExpiryInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.sweepExpired()
        }
    }
}

// sweepExpired 执行一轮过期清理。
func (m *MemoryManager) sweepExpired() {
    now := time.Now()
    batch := m.config.ExpiryBatchSize // 默认 100

    for agentID, mem := range m.instances {
        ext, ok := mem.(MemoryExtended)
        if !ok {
            continue
        }

        // 列出所有层级的记忆，检查过期
        for _, layer := range []MemoryLayer{LayerShortTerm, LayerLongTerm, LayerSummary} {
            items, err := ext.ListByLayer(layer, batch, 0)
            if err != nil {
                m.logger.Warn("list memories for expiry failed", "agent", agentID, "err", err)
                continue
            }

            removed := int64(0)
            for _, item := range items {
                if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
                    if err := mem.Delete(item.Key); err == nil {
                        removed++
                    }
                }
            }

            if removed > 0 {
                m.logger.Info("expired memories swept",
                    "agent", agentID,
                    "layer", layer,
                    "removed", removed,
                )
                m.metrics.AddCounter("memory_expire_total", removed)
            }
        }
    }
}
```

### 6.2 淘汰策略

当记忆数量超过容量上限时，触发淘汰策略。

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| **LRU** | 淘汰最近最少访问的记忆 | 默认策略 |
| **TTL** | 淘汰最早过期的记忆 | 配合 TTL 使用 |
| **FIFO** | 先进先出，淘汰最早创建的记忆 | Summary 层 |
| **Score** | 淘汰检索相关性分数最低的记忆 | 向量搜索场景 |

```go
type EvictionConfig struct {
    MaxItems       int            `json:"max_items"`        // 容量上限，默认 10000
    EvictionPolicy EvictionPolicy `json:"eviction_policy"`  // 默认 LRU
    EvictBatch     int            `json:"evict_batch"`      // 每次淘汰数量，默认 100
}

type EvictionPolicy string

const (
    EvictionLRU   EvictionPolicy = "lru"
    EvictionTTL   EvictionPolicy = "ttl"
    EvictionFIFO  EvictionPolicy = "fifo"
    EvictionScore EvictionPolicy = "score"
)

// evict 执行淘汰策略。
func (m *memoryImpl) evict() error {
    count, err := m.countAll()
    if err != nil {
        return err
    }
    if count <= int64(m.config.Eviction.MaxItems) {
        return nil // 未超限，无需淘汰
    }

    evictNum := int(count - int64(m.config.Eviction.MaxItems))
    if evictNum < m.config.Eviction.EvictBatch {
        evictNum = m.config.Eviction.EvictBatch
    }

    candidates, err := m.store.ListForEviction(m.config.Eviction.EvictionPolicy, evictNum)
    if err != nil {
        return fmt.Errorf("eviction list failed: %w", err)
    }

    for _, item := range candidates {
        if err := m.store.Delete(item.Key); err != nil {
            m.logger.Warn("evict delete failed", "key", item.Key, "err", err)
            continue
        }
        m.logger.Info("memory evicted",
            "key", item.Key,
            "policy", m.config.Eviction.EvictionPolicy,
        )
    }
    return nil
}
```

---

## 7. 完整生命周期示例

```go
// 示例：一条记忆的完整生命周期
func lifecycleExample(mem Memory) {
    // 1. 创建（短期，TTL 1 小时）
    err := mem.Add("task:buy_groceries", "买牛奶和鸡蛋", map[string]any{
        "source": "user",
        "promote": true,
    })
    if err != nil {
        log.Fatal(err)
    }

    // 2. 检索
    results, _ := mem.Search("买什么", 5)
    fmt.Printf("检索到 %d 条相关记忆\n", len(results))

    // 3. 更新
    if ext, ok := mem.(MemoryExtended); ok {
        _ = ext.Update("task:buy_groceries", "买牛奶、鸡蛋和面包", map[string]any{
            "updated_by": "user",
        })

        // 4. 晋升为长期记忆
        _ = ext.Promote("task:buy_groceries")
    }

    // 5. 过期清理（后台自动执行，也可手动触发）
    if ext, ok := mem.(MemoryExtended); ok {
        removed, _ := ext.Expire()
        fmt.Printf("清理了 %d 条过期记忆\n", removed)
    }

    // 6. 手动删除
    _ = mem.Delete("task:buy_groceries")

    // 7. 清空所有记忆
    _ = mem.Clear()
}
```

---

## 8. 配置参考

```yaml
memory:
  # 过期清理
  expiry_interval: 5m        # 清理任务执行间隔
  expiry_batch_size: 100     # 每轮清理扫描数量

  # 淘汰策略
  max_items: 10000           # 容量上限
  eviction_policy: lru      # lru / ttl / fifo / score
  evict_batch: 100           # 每次淘汰数量

  # TTL 默认值
  ttl:
    short_term: 0            # 0 = 随 Session
    long_term: 0             # 0 = 永不过期
    summary: 720h            # 30 天
```

---

## 9. 可观测性

| 事件 | 类型 | 说明 |
|------|------|------|
| `memory.added` | SSE | 记忆创建 |
| `memory.updated` | SSE | 记忆更新 |
| `memory.promoted` | SSE | 记忆晋升 |
| `memory.expired` | SSE | 记忆过期清理 |
| `memory.evicted` | SSE | 记忆被淘汰 |
| `memory.deleted` | SSE | 记忆手动删除 |

| 指标 | 类型 | 说明 |
|------|------|------|
| `memory_expire_total` | Counter | 过期清理总数 |
| `memory_promote_total` | Counter | 晋升总数 |
| `memory_evict_total` | Counter | 淘汰总数 |

---

*最后更新: 2025-07-17*
