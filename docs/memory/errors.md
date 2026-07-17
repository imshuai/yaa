# Memory 错误处理

> 文档路径: `docs/memory/errors.md`
> 上级: `docs/memory/README.md` §错误处理
> 依赖: `docs/memory/architecture.md`, `docs/memory/storage.md`

---

## 1. 错误分类

### 1.1 错误类型总表

| 错误类型 | 说明 | 处理方式 |
|---------|------|---------|
| `ErrMemoryNotFound` | 按 key 未找到记忆 | 返回空结果或告知 LLM 无相关记忆 |
| `ErrMemoryAlreadyExists` | Add 时 key 已存在 | 返回给调用方，提示使用 Update |
| `ErrMemoryStoreUnavailable` | 存储后端不可用（DB 连接断开） | 触发降级策略，回退到内存缓存 |
| `ErrMemoryEmbedderFailed` | 向量嵌入失败（Embedder 超时/报错） | 回退到关键词搜索 |
| `ErrMemoryEmbedderUnavailable` | 未配置 Embedder 或服务不可达 | 直接使用关键词搜索 |
| `ErrMemorySearchTimeout` | 检索超时 | 返回已获取的部分结果 + 警告 |
| `ErrMemoryWriteFailed` | 写入失败（磁盘满 / DB 错误） | 记录错误日志，返回错误给调用方 |
| `ErrMemoryDeleteFailed` | 删除失败 | 记录日志，标记为待清理 |
| `ErrMemoryCorrupted` | 记忆数据损坏（反序列化失败） | 跳过该条，记录错误，继续处理其余 |
| `ErrMemoryPermissionDenied` | Agent 无权访问目标 Memory 作用域 | 返回权限错误给调用方 |
| `ErrMemoryQuotaExceeded` | 超出 Agent 记忆配额 | 触发淘汰策略，淘汰低优先级记忆后重试 |
| `ErrMemoryLayerInvalid` | 指定了无效的 MemoryLayer | 返回参数错误给调用方 |

### 1.2 错误严重度分级

| 级别 | 说明 | 示例 | 影响 |
|------|------|------|------|
| **Fatal** | 存储完全不可用 | SQLite 文件丢失且无法重建 | Memory 系统停摆，Agent 降级运行 |
| **Error** | 单条操作失败 | 写入失败、删除失败 | 该条记忆不可用，其余正常 |
| **Warn** | 功能降级 | 向量搜索回退为关键词搜索 | 检索质量下降，功能可用 |
| **Info** | 预期内行为 | 记忆过期清理、晋升触发 | 无负面影响 |

---

## 2. 错误传递策略

### 2.1 传递路径

```text
Memory 操作错误 → Memory Manager → Agent → LLM
                                      │
                                      ├─ 可恢复 → 降级处理，返回降级结果
                                      └─ 不可恢复 → 告知 LLM，LLM 决策
```

### 2.2 Agent 层错误处理

```go
func (a *Agent) recallMemory(query string, limit int) ([]*MemoryItem, error) {
    mem, err := a.memMgr.GetMemory(a.id)
    if err != nil {
        // Memory 实例获取失败 → 降级：不注入记忆
        a.logger.Warn("memory unavailable, skipping recall", "error", err)
        return nil, nil // 返回空，不阻断主流程
    }

    results, err := mem.Search(query, limit)
    if err != nil {
        switch {
        case errors.Is(err, ErrMemoryEmbedderFailed),
             errors.Is(err, ErrMemoryEmbedderUnavailable):
            // 向量搜索不可用 → 降级为关键词搜索
            a.logger.Warn("vector search unavailable, falling back to keyword", "error", err)
            results, err = a.keywordSearchFallback(mem, query, limit)
            if err != nil {
                a.logger.Error("keyword search also failed", "error", err)
                return nil, nil
            }

        case errors.Is(err, ErrMemorySearchTimeout):
            // 超时 → 返回部分结果
            a.logger.Warn("memory search timed out, returning partial results", "error", err)
            // results 可能已包含部分数据

        default:
            a.logger.Error("memory search failed", "error", err)
            return nil, nil
        }
    }

    return results, nil
}
```

### 2.3 错误包装规范

Memory 系统遵循 Go 错误包装惯例，使用 `fmt.Errorf` + `%w` 保留原始错误链：

```go
// 存储层错误包装
if err := s.db.Get(key, &item); err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        return nil, ErrMemoryNotFound
    }
    return nil, fmt.Errorf("memory store get '%s': %w", key, err)
}

// 调用方可通过 errors.Is 判断具体错误类型
if errors.Is(err, ErrMemoryNotFound) {
    // 记忆不存在，非异常行为
    return nil
}
```

---

## 3. 降级策略

### 3.1 降级层次

| 层次 | 触发条件 | 降级行为 | 影响范围 |
|------|---------|---------|---------|
| L0 | 向量嵌入失败 | 向量搜索 → 关键词搜索 | 检索精度下降 |
| L1 | 存储后端不可用 | 持久化存储 → 内存缓存（临时） | 重启后丢失新记忆 |
| L2 | Memory 系统完全不可用 | 跳过记忆注入，Agent 无记忆运行 | 仅当前 Session 受影响 |
| L3 | 配额超限 | 淘汰低优先级记忆后重试写入 | 旧记忆被清理 |

### 3.2 向量搜索降级实现

```go
func (m *MemoryManager) searchWithFallback(mem Memory, query string, limit int) ([]*MemoryItem, error) {
    results, err := mem.Search(query, limit)
    if err == nil {
        return results, nil
    }

    // 降级 L0：向量搜索失败 → 关键词搜索
    if errors.Is(err, ErrMemoryEmbedderFailed) ||
       errors.Is(err, ErrMemoryEmbedderUnavailable) {
        m.logger.Warn("vector search degraded to keyword search",
            "agent_id", mem.AgentID(),
            "error", err,
        )
        m.metrics.IncCounter("memory_search_degraded_total")

        if kwMem, ok := mem.(KeywordSearchable); ok {
            return kwMem.KeywordSearch(query, limit)
        }
    }

    // 降级 L2：搜索完全失败 → 返回空结果
    m.logger.Error("memory search completely failed, returning empty", "error", err)
    return nil, nil
}
```

### 3.3 存储降级实现

```go
func (m *MemoryManager) writeWithFallback(agentID, key, content string, meta map[string]any) error {
    mem, err := m.GetMemory(agentID)
    if err != nil {
        return fmt.Errorf("get memory instance: %w", err)
    }

    if err := mem.Add(key, content, meta); err != nil {
        if errors.Is(err, ErrMemoryStoreUnavailable) {
            // 降级 L1：持久化存储不可用 → 内存缓存
            m.logger.Warn("store unavailable, caching in memory temporarily",
                "agent_id", agentID, "key", key)
            m.fallbackCache.Set(agentID+":"+key, &MemoryItem{
                Key: key, Content: content, Metadata: meta,
                CreatedAt: time.Now(),
            })
            m.metrics.IncCounter("memory_write_fallback_total")
            return nil
        }

        if errors.Is(err, ErrMemoryQuotaExceeded) {
            // 降级 L3：配额超限 → 淘汰后重试
            m.logger.Warn("quota exceeded, evicting low-priority memories", "agent_id", agentID)
            if evictErr := m.evict(agentID, 5); evictErr != nil {
                return fmt.Errorf("evict for quota: %w", evictErr)
            }
            return mem.Add(key, content, meta) // 重试一次
        }

        return err
    }
    return nil
}
```

---

## 4. 重试策略

| 操作 | 重试条件 | 重试次数 | 退避策略 |
|------|---------|---------|---------|
| 向量嵌入 | Embedder 超时 | 2 次 | 固定 500ms |
| 存储写入 | DB 临时错误 | 3 次 | 指数退避 200ms / 400ms / 800ms |
| 存储读取 | DB 临时错误 | 2 次 | 指数退避 100ms / 200ms |
| 配额淘汰后写入 | 淘汰成功 | 1 次 | 无 |
| 连接重建 | 存储连接断开 | 5 次 | 指数退避 1s / 2s / 4s / 8s / 16s |

---

## 5. 可观测性集成

### 5.1 错误指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `memory_errors_total` | Counter | type, agent_id | 错误总数按类型 |
| `memory_search_degraded_total` | Counter | agent_id, reason | 搜索降级次数 |
| `memory_write_fallback_total` | Counter | agent_id | 写入降级到内存缓存次数 |
| `memory_eviction_total` | Counter | agent_id, layer | 记忆淘汰次数 |
| `memory_retry_total` | Counter | operation, agent_id | 重试次数 |

### 5.2 Remote API 事件

| 事件 | 触发时机 | Payload |
|------|---------|---------|
| `memory.error` | Memory 操作发生错误 | agent_id, key, error_type, message |
| `memory.degraded` | 搜索/写入触发降级 | agent_id, operation, fallback_level |
| `memory.evicted` | 记忆被淘汰 | agent_id, key, layer, reason |
| `memory.store_recovered` | 存储从不可用恢复 | agent_id, duration |

---

*最后更新: 2026-07-17*
