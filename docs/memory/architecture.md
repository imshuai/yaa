# 三层记忆架构

> 文档路径: `docs/memory/architecture.md`
> 上游: `docs/architecture.md` §3.6, `docs/memory/README.md`
> 下游: `docs/memory/storage.md`, `docs/memory/lifecycle.md`, `docs/memory/integration.md`

---

## 1. 架构总览

Yaa! 的 Memory 系统采用**三层架构**，借鉴认知科学中人类记忆的分阶模型，并做了工程化适配。三层各自独立，拥有不同的存储策略、生命周期和检索方式，又通过统一接口协同工作。

```text
┌─────────────────────────────────────────────────────────────┐
│                      Memory System                          │
│                                                             │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            Layer 1: Short-term (短期记忆)               │  │
│  │  当前 Session 的消息历史，内存中，随会话结束而淘汰或晋升   │  │
│  └──────────────────────────┬────────────────────────────┘  │
│                             │ 自动晋升 (Promote)             │
│                             ▼                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            Layer 2: Long-term (长期记忆)                │  │
│  │  跨 Session 持久化，SQLite / 向量数据库，支持语义检索    │  │
│  └──────────────────────────┬────────────────────────────┘  │
│                             │ 压缩摘要 (Summarize)           │
│                             ▼                                │
│  ┌───────────────────────────────────────────────────────┐  │
│  │            Layer 3: Summary (摘要记忆)                  │  │
│  │  Session 的压缩摘要，Storage 持久化，用于上下文恢复      │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

### 1.1 三层对比

| 维度 | Short-term | Long-term | Summary |
|------|------------|-----------|---------|
| **类比** | 工作记忆 | 长期记忆 | 情节摘要 |
| **生命周期** | 单 Session | 跨 Session 永久 | 跨 Session 永久 |
| **存储方式** | 内存 / Storage | SQLite + 向量索引 | Storage (KV) |
| **容量** | 受 Token 窗口约束 | 无上限（按需检索） | 极小（压缩文本） |
| **检索方式** | 直接加载 | 向量语义搜索 / 关键词 | 按 Session ID 加载 |
| **生成时机** | 每条消息实时 | 主动写入 / 自动晋升 | Context 压缩时 |
| **注入方式** | 直接进入 Context | 检索后注入 Context | 替换历史消息 |
| **操作主体** | Context Manager | Memory System | Context Manager |

---

## 2. Layer 1: Short-term Memory（短期记忆）

### 2.1 职责

短期记忆保存当前 Session 的消息历史，是 LLM "即时上下文" 的来源。它在内存中维护，随 Session 创建而生成，随 Session 关闭而淘汰或晋升。

### 2.2 接口定义

```go
// ShortTermMemory 管理当前 Session 的短期记忆。
type ShortTermMemory struct {
    sessionID string
    items     []*MemoryItem   // 有序消息列表
    maxItems  int             // 最大保留条数（滑动窗口）
    mu        sync.RWMutex
}

// Add 追加一条短期记忆（消息）。
func (s *ShortTermMemory) Add(key string, content string, metadata map[string]any) error

// Search 在短期记忆中按关键词检索。
func (s *ShortTermMemory) Search(query string, limit int) ([]*MemoryItem, error)

// Recent 返回最近 N 条记忆（用于构建 Context）。
func (s *ShortTermMemory) Recent(n int) []*MemoryItem

// Promote 将指定记忆晋升为长期记忆。
func (s *ShortTermMemory) Promote(key string, longTerm Memory) error
```

### 2.3 存储方式

| 场景 | 存储 | 说明 |
|------|------|------|
| 默认 | 内存（`[]MemoryItem`） | 快速访问，无持久化开销 |
| Session 持久化 | Storage (SQLite KV) | Session 恢复时从 Storage 加载 |

### 2.4 适用场景

- 多轮对话中的上下文保持
- 当前 Session 内的消息回溯
- Tool 调用结果的临时缓存
- Context 压缩前的原始消息池

---

## 3. Layer 2: Long-term Memory（长期记忆）

### 3.1 职责

长期记忆是跨 Session 的持久化记忆，支持语义检索。Agent 可以主动写入，也可以由短期记忆自动晋升而来。它是 Agent 具备"记忆连续性"的核心。

### 3.2 接口定义

```go
// LongTermMemory 管理跨 Session 的持久化记忆。
type LongTermMemory struct {
    agentID  string
    store    MemoryStore       // 底层存储（SQLite / 向量数据库）
    embedder Embedder          // 向量嵌入器（可选）
    config   LongTermConfig
    mu       sync.RWMutex
}

// Add 写入长期记忆，自动生成向量嵌入。
func (l *LongTermMemory) Add(key string, content string, metadata map[string]any) error {
    item := &MemoryItem{
        Key:       key,
        Content:   content,
        Metadata:  metadata,
        Layer:     LayerLongTerm,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    // 如果 Embedder 可用，生成向量嵌入
    if l.embedder != nil {
        emb, err := l.embedder.Embed(content)
        if err != nil {
            return fmt.Errorf("embed failed: %w", err)
        }
        item.Metadata["embedding"] = emb
    }
    return l.store.Put(item)
}

// Search 语义检索长期记忆，向量搜索不可用时回退到关键词搜索。
func (l *LongTermMemory) Search(query string, limit int) ([]*MemoryItem, error) {
    if l.embedder != nil {
        queryEmb, err := l.embedder.Embed(query)
        if err == nil {
            results, err := l.store.VectorSearch(queryEmb, limit)
            if err == nil && len(results) > 0 {
                return results, nil
            }
            // 向量搜索失败，降级到关键词搜索
        }
    }
    return l.store.KeywordSearch(query, limit)
}
```

### 3.3 存储方式

| 后端 | 说明 | 适用场景 |
|------|------|----------|
| SQLite + 向量扩展 | 纯 Go，零 CGO，支持向量索引 | 默认方案，单机部署 |
| 外部向量数据库 | Milvus / Qdrant / pgvector | 大规模记忆，高并发检索 |
| 内存回退 | 纯内存 Map | 测试 / 向量不可用时降级 |

### 3.4 适用场景

- 用户偏好与画像（"用户喜欢简洁回答"）
- 跨 Session 的事实记忆（"用户的项目叫 Yaa!"）
- 知识库片段注入（RAG 场景）
- 长期任务状态跟踪

---

## 4. Layer 3: Summary Memory（摘要记忆）

### 4.1 职责

摘要记忆是 Session 的压缩摘要，由 Context Manager 在上下文超出 Token 限制时自动生成。它用极小的 Token 开销保留历史会话的关键信息，用于上下文恢复。

### 4.2 接口定义

```go
// SummaryMemory 管理 Session 的压缩摘要。
type SummaryMemory struct {
    sessionID string
    store     MemoryStore
    config    SummaryConfig
    mu        sync.RWMutex
}

// Generate 对指定消息列表生成摘要，并存储。
func (s *SummaryMemory) Generate(messages []*MemoryItem) (*MemoryItem, error) {
    // 调用 LLM 对消息列表进行摘要压缩
    summaryText, err := s.config.Summarizer.Summarize(messages)
    if err != nil {
        return nil, fmt.Errorf("summarize failed: %w", err)
    }
    item := &MemoryItem{
        Key:       fmt.Sprintf("summary:%s:%d", s.sessionID, time.Now().Unix()),
        Content:   summaryText,
        Layer:     LayerSummary,
        Metadata:  map[string]any{
            "session_id":  s.sessionID,
            "msg_count":   len(messages),
            "generated_at": time.Now(),
        },
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }
    if err := s.store.Put(item); err != nil {
        return nil, err
    }
    return item, nil
}

// LoadLatest 加载当前 Session 最近一次的摘要。
func (s *SummaryMemory) LoadLatest() (*MemoryItem, error)
```

### 4.3 存储方式

| 场景 | 存储 | 说明 |
|------|------|------|
| 默认 | Storage (SQLite KV) | 按 `session_id` 索引，持久化 |
| 级联摘要 | Storage + 前序摘要引用 | 长对话中多次压缩，逐级概括 |

### 4.4 适用场景

- 长对话的上下文压缩（避免 Token 爆炸）
- Session 恢复时的快速上下文重建
- 跨 Session 的会话回顾（"上次我们聊到了…"）

---

## 5. 三层协作流程

```text
用户消息进入
    │
    ▼
┌─────────────────┐
│  Short-term     │ ← 每条消息实时写入
│  (短期记忆)      │
└────────┬────────┘
         │
         │ Context 超出 Token 限制？
         ├─────────────────── 否 ──→ 直接构建 Context → 调用 LLM
         │
         │ 是
         ▼
┌─────────────────┐
│  Summary        │ ← 压缩旧消息为摘要
│  (摘要记忆)      │
└────────┬────────┘
         │ 摘要替换旧消息，释放 Token 空间
         ▼
    构建 Context → 调用 LLM
         │
         │ LLM 返回结果，判断是否值得长期保存
         ▼
┌─────────────────┐
│  Long-term      │ ← 主动写入 / 自动晋升
│  (长期记忆)      │
└─────────────────┘
         │
         │ 下次新 Session 开始时
         ▼
    检索 Long-term → 注入相关记忆到 Context
```

### 5.1 晋升机制

短期记忆不会永久存在。当满足以下条件时，短期记忆会被自动晋升为长期记忆：

| 触发条件 | 说明 |
|----------|------|
| 用户显式标记 | "记住这个" / metadata 中 `persist=true` |
| 重要性评分超阈值 | LLM 评估消息重要性 ≥ 0.7 |
| Session 关闭策略 | `on_close: promote_important` 配置项 |

```go
// 自动晋升示例
func (s *ShortTermMemory) autoPromote(longTerm Memory) {
    for _, item := range s.items {
        if score, ok := item.Metadata["importance"].(float64); ok && score >= 0.7 {
            _ = s.Promote(item.Key, longTerm)
        }
    }
}
```

---

## 6. 统一接口与分层实现

三层记忆均实现 `Memory` 接口，但各自有专属方法。Memory Manager 通过组合模式统一管理：

```go
// AgentMemory 组合三层记忆，对上层提供统一视图。
type AgentMemory struct {
    AgentID   string
    ShortTerm *ShortTermMemory
    LongTerm  *LongTermMemory
    Summary   *SummaryMemory
}

// Search 统一检索：先查短期，再查长期，最后查摘要。
func (a *AgentMemory) Search(query string, limit int) ([]*MemoryItem, error) {
    var results []*MemoryItem

    // 1. 短期记忆（精确匹配，速度快）
    if st, _ := a.ShortTerm.Search(query, limit); len(st) > 0 {
        results = append(results, st...)
    }

    // 2. 长期记忆（语义检索，相关性高）
    if len(results) < limit {
        remaining := limit - len(results)
        if lt, _ := a.LongTerm.Search(query, remaining); len(lt) > 0 {
            results = append(results, lt...)
        }
    }

    // 3. 摘要记忆（补充上下文）
    if len(results) < limit {
        remaining := limit - len(results)
        if sm, _ := a.Summary.Search(query, remaining); len(sm) > 0 {
            results = append(results, sm...)
        }
    }

    return results, nil
}
```

---

## 7. 设计约束

| 约束 | 说明 |
|------|------|
| **零 CGO** | 向量计算使用纯 Go 库，保证 Windows 7 兼容 |
| **优雅降级** | 向量搜索不可用时回退到关键词搜索 |
| **Agent 隔离** | 三层记忆均以 `agentID` 做命名空间隔离 |
| **Token 预算** | 记忆注入 Context 时有 Token 预算限制，不挤占对话空间 |
| **并发安全** | 所有层均使用 `sync.RWMutex` 保护读写操作 |

---

*最后更新: 2026-07-17*
