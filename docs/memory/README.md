# Memory 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/memory/` (原计划单文件 `docs/memory.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.6, `docs/session.md`, `docs/context.md`

---

## 1. 概述

### 1.1 什么是 Memory

Memory 系统管理 Agent 的记忆，是 Yaa! Agent 具备"跨对话连续性"的核心能力。

| 层级 | 抽象 | 类比 |
|------|------|------|
| Context | 当前传给 LLM 的上下文窗口 | 工作记忆（短期） |
| **Memory** | **跨轮次、跨会话的持久化记忆** | **长期记忆** |
| Session | 一次交互会话的状态容器 | 对话上下文 |

Memory 与 Context 的关键区别：

| 维度 | Context | Memory |
|------|---------|--------|
| 生命周期 | 单轮对话内 | 跨 Session 持久化 |
| 存储位置 | 内存（构建时组装） | 内存 / SQLite / 向量数据库 |
| 注入方式 | 直接传给 LLM | 检索后按需注入 Context |
| 大小限制 | 受 Token 窗口约束 | 无上限（按需检索） |
| 操作主体 | Context Manager | Memory System |

### 1.2 设计理念

Yaa! 的 Memory 系统设计借鉴了认知科学中人类记忆的分阶模型，并做了工程化适配：

| 特性 | 说明 |
|------|------|
| **三层架构** | Short-term / Long-term / Summary，各司其职 |
| **统一接口** | 所有记忆操作通过 `Memory` interface，屏蔽存储差异 |
| **检索优先** | 长期记忆按需检索注入，而非全量加载 |
| **向量搜索** | 支持语义相似度检索，超越关键词匹配 |
| **生命周期管理** | 记忆有创建、更新、过期、淘汰机制 |
| **Agent 隔离** | 每个 Agent 拥有独立的 Memory 空间 |
| **可观测** | 记忆操作有完整日志、指标和事件 |
| **可扩展** | 存储后端可替换（SQLite / 向量数据库 / 外部服务） |

### 1.3 核心原则

1. **Retrieval Over Loading** — 长期记忆通过检索注入 Context，而非全量加载
2. **Layered Isolation** — 三层记忆相互独立，各自有不同的存储和检索策略
3. **Agent Scoped** — 记忆按 Agent 隔离，不同 Agent 的记忆互不干扰
4. **Progressive Persistence** — 短期记忆可自动晋升为长期记忆
5. **Configurable Backend** — 存储后端通过配置切换，无需修改代码
6. **Graceful Degradation** — 向量搜索不可用时回退到关键词搜索

---

## 2. 核心接口与类型定义

### 2.1 Memory Interface

```go
// Memory 是记忆系统的统一接口。
// 所有记忆操作（增删改查检索）通过此接口完成。
type Memory interface {
    // Add 写入一条记忆。
    // key 是记忆的唯一标识，content 是记忆内容，metadata 是可选元数据。
    // 如果 key 已存在，行为由实现决定（覆盖或报错）。
    Add(key string, content string, metadata map[string]any) error

    // Get 按 key 获取单条记忆。
    // 不存在时返回 ErrMemoryNotFound。
    Get(key string) (*MemoryItem, error)

    // Search 检索记忆。
    // query 是检索查询（自然语言或关键词），limit 限制返回数量。
    // 支持向量语义搜索（如已配置）或关键词搜索（回退）。
    Search(query string, limit int) ([]*MemoryItem, error)

    // Delete 按 key 删除单条记忆。
    // 不存在时返回 ErrMemoryNotFound。
    Delete(key string) error

    // Clear 清除当前作用域内的所有记忆。
    // 作用域由实现决定（Agent 级 / Session 级）。
    Clear() error
}
```

### 2.2 MemoryItem

```go
// MemoryItem 是一条记忆的完整表示。
type MemoryItem struct {
    // Key 是记忆的唯一标识符。
    // 命名规范: 语义化路径，如 "user_preference:theme"。
    Key       string         `json:"key"`

    // Content 是记忆的文本内容。
    // 这是检索和注入 Context 的主要载荷。
    Content   string         `json:"content"`

    // Metadata 是可选的元数据。
    // 用于存储来源、类型、标签、时间戳等结构化信息。
    // 不直接传递给 LLM，但可用于过滤和检索。
    Metadata  map[string]any `json:"metadata,omitempty"`

    // Layer 标识记忆所属的层级。
    Layer     MemoryLayer     `json:"layer"`

    // Score 是检索时的相关性分数（0-1）。
    // 仅在 Search 返回时有意义，Add/Get 时为 0。
    Score     float64         `json:"score,omitempty"`

    // CreatedAt 是记忆创建时间。
    CreatedAt time.Time       `json:"created_at"`

    // UpdatedAt 是记忆最后更新时间。
    UpdatedAt time.Time       `json:"updated_at"`

    // ExpiresAt 是记忆过期时间（可选）。
    // 零值表示永不过期。
    ExpiresAt time.Time       `json:"expires_at,omitempty"`
}
```

### 2.3 MemoryLayer

```go
// MemoryLayer 标识记忆的层级。
type MemoryLayer int

const (
    // LayerShortTerm 短期记忆：当前 Session 的消息历史。
    // 存储在内存或 Storage 中，Session 关闭后可能晋升为长期记忆或被清除。
    LayerShortTerm MemoryLayer = iota

    // LayerLongTerm 长期记忆：跨 Session 的持久化记忆。
    // 存储在 SQLite 或向量数据库中，支持语义检索。
    LayerLongTerm

    // LayerSummary 摘要记忆：Session 的压缩摘要。
    // 由 Context Manager 在压缩时生成，存储在 Storage 中。
    LayerSummary
)
```

### 2.4 MemoryManager

```go
// MemoryManager 管理所有 Agent 的 Memory 实例。
type MemoryManager struct {
    instances map[string]Memory          // agentID → Memory 实例
    store     MemoryStore                // 底层存储
    embedder  Embedder                   // 向量嵌入器（可选）
    config    MemoryConfig               // 全局配置
    logger    *slog.Logger
    mu        sync.RWMutex
}

// GetMemory 获取指定 Agent 的 Memory 实例。
// 不存在时自动创建（懒加载）。
func (m *MemoryManager) GetMemory(agentID string) (Memory, error)

// CloseAll 关闭所有 Memory 实例，释放资源。
func (m *MemoryManager) CloseAll() error

// HealthCheck 检查 Memory 系统健康状态。
func (m *MemoryManager) HealthCheck() MemoryHealthReport
```

### 2.5 扩展接口

架构中定义的 `Memory` interface 是最小接口。实际实现中，Yaa! 提供扩展接口供需要更多能力的场景使用：

```go
// MemoryExtended 是 Memory 的扩展接口，提供批量操作、过滤和更新能力。
// 实现者可选择实现此接口，Memory Manager 会做能力检测。
type MemoryExtended interface {
    Memory

    // AddBatch 批量写入记忆。
    AddBatch(items []*MemoryItem) error

    // SearchWithFilter 带元数据过滤的检索。
    SearchWithFilter(query string, filter MemoryFilter, limit int) ([]*MemoryItem, error)

    // Update 更新已有记忆的内容和元数据。
    Update(key string, content string, metadata map[string]any) error

    // ListByLayer 列出指定层级的所有记忆。
    ListByLayer(layer MemoryLayer, limit int, offset int) ([]*MemoryItem, error)

    // Count 返回指定层级的记忆数量。
    Count(layer MemoryLayer) (int64, error)

    // Promote 将短期记忆晋升为长期记忆。
    Promote(key string) error

    // Expire 清理已过期的记忆。
    Expire() (int64, error)
}

// MemoryFilter 是记忆检索的过滤条件。
type MemoryFilter struct {
    Layer    MemoryLayer         // 按层级过滤
    Metadata map[string]any      // 元数据键值匹配
    After    time.Time           // 创建时间下界
    Before   time.Time           // 创建时间上界
    Tags     []string             // 标签过滤
}
```

**能力检测模式：**

```go
// 调用方通过类型断言检测扩展能力
if ext, ok := mem.(MemoryExtended); ok {
    err = ext.Promote(key)
} else {
    // 回退：手动 Delete + Add
}
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [architecture.md](architecture.md) | 三层记忆架构 — Short-term / Long-term / Summary 的详细设计 |
| [storage.md](storage.md) | 记忆存储与检索 — 存储后端、向量搜索、索引策略 |
| [lifecycle.md](lifecycle.md) | 记忆生命周期管理 — 创建、晋升、过期、淘汰 |
| [integration.md](integration.md) | 与 Session / Context / Agent 的集成 |
| [config-ref.md](config-ref.md) | 配置参考 — 全局配置、Agent 级别、存储后端配置 |
| [errors.md](errors.md) | 错误处理 — 错误分类、传递、降级策略 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件、健康检查 |
| [decisions.md](decisions.md) | 设计决策（MM-001 ~ MM-008）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-16*