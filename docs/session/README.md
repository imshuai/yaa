# Session 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/session/` (原计划单文件 `docs/session.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.3 (Session), §3.11 (Remote API), §5 (并发模型)

---

## 1. 概述

### 1.1 什么是 Session

Session 是 Yaa! 中**一次完整交互上下文**的管理单元。

| 层级 | 抽象 | 类比 |
|------|------|------|
| Agent | 人格 + 能力集合 + Memory | 用户账号 |
| **Session** | **一次交互上下文 + 消息历史 + 状态** | **一个对话窗口** |
| Message | 一条交互记录 | 一条消息 |

一个 Session 封装了：
- **消息历史** — 完整的多轮对话记录（user / assistant / tool）
- **状态机** — Active / Paused / Closed 三态生命周期
- **隔离边界** — Session 之间状态完全隔离
- **持久化单元** — 可保存到 Storage，重启后恢复
- **元数据** — 自定义键值对，支持业务扩展

### 1.2 设计理念

Yaa! 的 Session 系统遵循以下设计原则：

| 设计原则 | 说明 |
|----------|------|
| **隔离优先** | 每个 Session 拥有独立的消息历史和状态，互不干扰 |
| **串行处理** | 同一 Session 内消息串行处理，避免并发导致状态混乱 |
| **可恢复** | Session 可持久化，Runtime 重启后自动恢复 |
| **多 Session 并发** | 一个 Agent 可同时管理多个 Session，各 Session 间并行 |
| **Remote API 驱动** | Session 的创建、查询、恢复、关闭均通过 Remote API |
| **生命周期明确** | 三态状态机，状态转换有清晰的前置条件和副作用 |
| **元数据可扩展** | 通过 `Metadata` 字段支持业务自定义数据，无需修改核心结构 |

### 1.3 核心特性

```text
┌─────────────────────────────────────────────────────────┐
│                      Agent                               │
│                                                           │
│   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│   │  Session A   │  │  Session B   │  │  Session C   │    │
│   │  (Active)    │  │  (Paused)    │  │  (Closed)    │    │
│   │              │  │              │  │              │    │
│   │  Messages    │  │  Messages    │  │  Messages    │    │
│   │  [user, asst, │  │  [user, asst]│  │  [archived]  │    │
│   │   tool, ...]  │  │              │  │              │    │
│   │              │  │              │  │              │    │
│   │  Metadata    │  │  Metadata    │  │  Metadata    │    │
│   └──────┬───────┘  └──────┬───────┘  └──────────────┘    │
│          │                 │                              │
│          │     状态隔离     │                              │
│          │◄─ ─ ─ ─ ─ ─ ─ ──┤                              │
│                                                           │
└─────────────────────────────────────────────────────────────┘
                    │
                    ▼
              Session Manager
              (创建/查询/恢复/关闭/持久化)
```

**关键能力：**

1. **多 Session 并发** — Agent 同时服务多个客户端，每个客户端拥有独立 Session
2. **状态隔离** — Session A 的消息、元数据、状态变更不影响 Session B
3. **持久化与恢复** — Session 数据写入 Storage，Runtime 重启后自动加载
4. **Remote API 全覆盖** — 创建、列表、详情、关闭均有对应端点
5. **流式对话** — WebSocket / SSE 支持实时流式输出，绑定到特定 Session

---

## 2. 核心接口与类型定义

### 2.1 Session

```go
// Session 管理一次完整的交互上下文。
type Session struct {
    ID        string         // 唯一标识符，格式: "sess_" + ULID
    AgentID   string         // 所属 Agent ID
    Messages  []Message      // 消息历史（有序）
    State     SessionState   // 当前状态: Created / Active / Paused / Closed
    CreatedAt time.Time      // 创建时间
    UpdatedAt time.Time      // 最后更新时间
    Metadata  map[string]any // 自定义元数据
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 全局唯一，ULID 生成，前缀 `sess_` |
| `AgentID` | `string` | 绑定的 Agent，创建时指定，不可变更 |
| `Messages` | `[]Message` | 有序消息列表，append-only（详见 §2.3） |
| `State` | `SessionState` | 生命周期状态枚举 |
| `CreatedAt` | `time.Time` | 创建时自动设置 |
| `UpdatedAt` | `time.Time` | 每次消息追加或状态变更时更新 |
| `Metadata` | `map[string]any` | 业务自定义数据，如 `title`、`tags`、`source` |

### 2.2 SessionState

```go
// SessionState 表示 Session 的生命周期状态。
type SessionState int

const (
    // SessionStateCreated 刚创建，尚未接收首条消息。
    SessionStateCreated SessionState = iota

    // SessionStateActive 活跃状态，可接收消息、执行对话。
    SessionStateActive

    // SessionStatePaused 暂停状态，拒绝新消息，但可恢复。
    SessionStatePaused

    // SessionStateClosed 已关闭，不可恢复，消息已归档。
    SessionStateClosed
)

func (s SessionState) String() string {
    switch s {
    case SessionStateCreated:
        return "created"
    case SessionStateActive:
        return "active"
    case SessionStatePaused:
        return "paused"
    case SessionStateClosed:
        return "closed"
    default:
        return "unknown"
    }
}
```

**状态转换图：**

```text
            Create()
               │
               ▼
         ┌──────────┐  首条消息
         │ Created   │─────────────┐
         └─────┬────┘              │
               │                   ▼
          Close()            ┌──────────┐
               │             │ Active    │◄────────┐
               ▼             └─────┬────┘          │
         ┌──────────┐       Pause()  │             │ Resume()
         │ Closed    │             │             │
         └──────────┘             ▼             │
                           ┌──────────┐          │
                           │ Paused    │─────────┘
                           └─────┬────┘
                                 │
                            Close()
                                 │
                                 ▼
                           ┌──────────┐
                           │ Closed    │  (终态，不可转换)
                           └──────────┘
```

**状态转换规则：**

| 从 | 到 | 触发方式 | 前置条件 |
|----|----|---------|---------|
| (创建) | Created | `Create()` | AgentID 有效 |
| Created | Active | 首条 `AppendMessage()` | State == Created |
| Created | Closed | `Close()` | — |
| Active | Paused | `Pause()` | — |
| Active | Closed | `Close()` | — |
| Paused | Active | `Resume()` | — |
| Paused | Closed | `Close()` | — |
| Closed | * | — | **不允许任何转换** |

### 2.3 Message

```go
// Message 表示 Session 中的一条消息。
type Message struct {
    ID              string         // 消息唯一标识，格式: "msg_" + ULID
    Role            MessageRole    // 消息角色: user / assistant / tool / system
    Content         string         // 消息文本内容
    ReasoningContent string        // 思维链内容（深度思考模式，见 provider.md §13）
    ToolCalls       []ToolCall     // 助手消息中的 Tool 调用请求（仅 Role=assistant）
    ToolCallID      string         // Tool 结果对应的调用 ID（仅 Role=tool）
    Name            string         // Tool 名称（仅 Role=tool）
    CreatedAt       time.Time      // 消息创建时间
    Metadata        map[string]any // 消息级元数据
}

// MessageRole 消息角色。
type MessageRole string

const (
    RoleUser      MessageRole = "user"
    RoleAssistant MessageRole = "assistant"
    RoleTool      MessageRole = "tool"
    RoleSystem    MessageRole = "system"
)

// ToolCall 表示 LLM 发起的 Tool 调用请求。
type ToolCall struct {
    ID       string         // 调用 ID，用于关联 Tool 结果
    Name     string         // Tool 名称
    Arguments map[string]any // 调用参数
}
```

**消息类型与用途：**

| Role | 说明 | 生成方 | 是否持久化 |
|------|------|--------|-----------|
| `user` | 用户输入 | Remote API Client | ✅ |
| `assistant` | LLM 响应 | Provider 返回 | ✅ |
| `assistant` (含 ToolCalls) | LLM 请求调用 Tool | Provider 返回 | ✅ |
| `tool` | Tool 执行结果 | Tool Manager | ✅ |
| `system` | 系统消息（Skill Prompt 注入等） | Skill Manager / Context Manager | ❌（不持久化到 Session） |

**Message 与 Provider 类型的关系：**

`pkg/types/message.go` 中定义的 `Message` 是公共类型，Session 内部使用同一类型。Provider 层负责将 `Message` 转换为各厂商的 API 格式。

```go
// pkg/types/message.go 中的公共 Message 类型
// 与 session.Message 保持一致，避免类型转换开销
type Message = session.Message
```

### 2.4 SessionManager

```go
// Manager 管理 Session 的创建、查询、生命周期和持久化。
type Manager struct {
    sessions  map[string]*Session      // sessionID → Session（内存索引）
    agentIdx  map[string][]string       // agentID → []sessionID（Agent 索引）
    store     SessionStore             // 持久化存储
    logger    *slog.Logger
    mu        sync.RWMutex              // 保护 sessions 和 agentIdx
}
```

**核心方法签名：**

```go
// Create 创建新 Session。
func (m *Manager) Create(agentID string, opts ...SessionOption) (*Session, error)

// Get 获取 Session 详情。
func (m *Manager) Get(sessionID string) (*Session, error)

// List 列出指定 Agent 的所有 Session。
func (m *Manager) List(agentID string) ([]*Session, error)

// ListByState 列出指定状态的 Session。
func (m *Manager) ListByState(agentID string, state SessionState) ([]*Session, error)

// Pause 暂停 Session。
func (m *Manager) Pause(sessionID string) error

// Resume 恢复暂停的 Session。
func (m *Manager) Resume(sessionID string) error

// Close 关闭 Session。
func (m *Manager) Close(sessionID string) error

// AppendMessage 向 Session 追加消息。
func (m *Manager) AppendMessage(sessionID string, msg Message) error

// GetMessages 获取 Session 的消息历史。
func (m *Manager) GetMessages(sessionID string, opts ...MessageQueryOption) ([]Message, error)

// UpdateMetadata 更新 Session 元数据。
func (m *Manager) UpdateMetadata(sessionID string, metadata map[string]any) error

// Delete 删除 Session（从存储中彻底移除）。
func (m *Manager) Delete(sessionID string) error

// RestoreAll 从存储恢复所有 Session（Runtime 启动时调用）。
func (m *Manager) RestoreAll() error
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [lifecycle.md](lifecycle.md) | Session 生命周期管理 — 创建、激活、暂停、恢复、关闭 |
| [persistence.md](persistence.md) | Session 持久化与恢复 — 存储格式、序列化、启动恢复 |
| [messaging.md](messaging.md) | 消息管理 — 消息类型、历史查询、流式输出 |
| [concurrency.md](concurrency.md) | 并发模型 — 串行处理、消息队列、锁策略 |
| [integration.md](integration.md) | 与 Agent / Context / Memory 的集成 |
| [config-ref.md](config-ref.md) | 配置参考 — 全局配置、Agent 级别覆盖 |
| [errors.md](errors.md) | 错误处理 — 错误分类、传递策略 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策（SSD-001 ~ SSD-NNN）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |