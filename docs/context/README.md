# Context 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/context/`（原计划单文件 `docs/context.md`，拆分为多文件）
> 依赖: `docs/architecture.md` §3.4 Context、§4.1 对话流程

---

## 1. 概述

### 1.1 什么是 Context

Context 是 Yaa! 中**传给 LLM 的上下文窗口的完整描述**。

它不是一段简单的文本，而是由多个来源经过收集、组装、压缩、截断后
形成的结构化消息集合，最终以 Provider 能理解的格式发送给 LLM。

```text
System Prompt  ──┐
Session 消息    ──┤──→  Context Manager  ──→  Context  ──→  Provider
Memory 摘要     ──┤     (Build / Compress / Truncate)
Tool 结果       ──┘
```

### 1.2 Context 与 Session / Memory 的区别

三者容易混淆，但职责完全不同：

| 维度 | Session | Context | Memory |
|------|---------|---------|--------|
| **本质** | 一次交互的**完整历史** | 一次 LLM 调用的**输入窗口** | 跨 Session 的**持久记忆** |
| **生命周期** | 创建→活跃→关闭 | 每次调用前构建，用后即弃 | 永久（或手动清除） |
| **存储** | Storage 持久化 | 内存临时对象 | SQLite / 向量数据库 |
| **大小** | 无上限（持续增长） | 受 Token 限制约束 | 无上限 |
| **是否截断** | ❌ 不截断 | ✅ 必须截断/压缩 | ❌ 不截断 |
| **类比** | 聊天记录 | 当前发给模型的那段话 | 长期记忆库 |

**一句话总结：** Session 是"发生过什么"，Context 是"这次告诉模型什么"，
Memory 是"模型记住什么"。

### 1.3 核心职责

Context Manager 负责将 Session 中的消息历史转化为
Provider 能接受、Token 限制允许的上下文窗口：

1. **收集** — System Prompt + 历史消息 + Memory 摘要 + Tool 结果
2. **压缩** — 当超出 Token 限制时，对旧消息进行摘要压缩
3. **截断** — 压缩后仍超限时，按策略截断最旧的消息
4. **格式化** — 转换为目标 Provider 的消息格式

---

## 2. 设计理念

| 原则 | 说明 |
|------|------|
| **Token-Aware** | 始终感知 Token 数量，不超限发送 |
| **Strategy-Pluggable** | 压缩/截断策略可插拔，默认提供滑动窗口 + 摘要压缩 |
| **Lossless First** | 优先无损截断（丢弃最旧消息），有损压缩作为最后手段 |
| **Provider-Aware** | 不同 Provider Token 计算方式不同，支持自定义 Tokenizer |
| **Transparent** | Context 构建过程可观测，可追踪每条消息的去留 |
| **Config-Driven** | 策略选择通过配置完成，无需修改代码 |

---

## 3. 核心接口定义

### 3.1 ContextManager

```go
// ContextManager 管理传给 LLM 的上下文窗口。
// 负责构建、压缩、截断上下文，确保不超出 Provider 的 Token 限制。
type ContextManager interface {
    // Build 从 Session 构建完整 Context。
    // 收集 System Prompt、历史消息、Memory 摘要、Tool 结果，
    // 根据 opts 调整策略，返回可直接发送给 Provider 的 Context。
    Build(session *session.Session, opts ...ContextOption) (*Context, error)

    // Compress 对 Context 进行摘要压缩。
    // 将较旧的消息通过 LLM 生成摘要，减少 Token 占用。
    // 压缩后仍保留最近 N 条原始消息。
    Compress(ctx *Context) (*Context, error)

    // Truncate 按 Token 限制截断 Context。
    // 从最旧的消息开始移除，直到总 Token 数 <= maxTokens。
    // System Prompt 和最近的 Tool 结果不会被截断。
    Truncate(ctx *Context, maxTokens int) (*Context, error)
}
```

### 3.2 Context 结构体

```go
// Context 是构建完成后、可直接发送给 Provider 的上下文。
type Context struct {
    SystemPrompt string            // 系统提示词
    Messages    []ContextMessage  // 有序消息列表
    TokenCount  int               // 当前总 Token 数
    TokenLimit  int               // Token 上限（来自 Provider）
    Metadata    map[string]any    // 构建元信息（来源、压缩记录等）
}

// ContextMessage 是 Context 中的单条消息。
type ContextMessage struct {
    Role       string            // system / user / assistant / tool
    Content    string            // 消息内容
    ToolCalls  []ToolCall        // assistant 消息中的 Tool 调用
    ToolCallID string            // tool 角色消息的关联 ID
    TokenCount int               // 该消息的 Token 数
    Source     MessageSource     // 消息来源
    Compressed bool              // 是否经过摘要压缩
}

// MessageSource 标识消息来源。
type MessageSource int

const (
    SourceSession  MessageSource = iota // 来自 Session 历史
    SourceMemory                        // 来自 Memory 摘要
    SourceTool                          // 来自 Tool 执行结果
    SourceSystem                        // 来自 System Prompt
)
```

### 3.3 ContextOption

```go
// ContextOption 用于在 Build 时调整 Context 构建行为。
type ContextOption func(*contextConfig)

type contextConfig struct {
    maxTokens     int           // Token 上限，默认取 Provider 限制
    strategy      Strategy      // 压缩/截断策略
    keepRecent    int           // 压缩时保留的最近消息数
    memorySummary string        // 注入的 Memory 摘要
    toolResults   []ContextMessage // 额外注入的 Tool 结果
}

// 常用 Option 函数
func WithMaxTokens(n int) ContextOption { ... }
func WithStrategy(s Strategy) ContextOption { ... }
func WithKeepRecent(n int) ContextOption { ... }
func WithMemorySummary(s string) ContextOption { ... }
func WithToolResults(msgs ...ContextMessage) ContextOption { ... }
```

### 3.4 压缩/截断策略

```go
// Strategy 定义 Context 压缩和截断的策略。
type Strategy int

const (
    // StrategySlideWindow 滑动窗口：仅保留最近 N 条消息，丢弃更早的。
    StrategySlideWindow Strategy = iota

    // StrategySummary 摘要压缩：将旧消息通过 LLM 生成摘要后替换。
    StrategySummary

    // StrategyHybrid 混合策略：先摘要压缩，仍超限再滑动窗口截断。
    StrategyHybrid

    // StrategyNone 不压缩不截断（调试用，可能超出 Token 限制）。
    StrategyNone
)
```

---

## 4. 工作流程

### 4.1 Context 构建流程

```text
Session 消息历史
  │
  ▼
┌─────────────────────────────────────────┐
│  1. 收集                                  │
│     System Prompt + Messages + Memory    │
│     + Tool Results                       │
└──────────────────┬──────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────┐
│  2. 计算 Token                            │
│     遍历所有消息，调用 Tokenizer 统计      │
│     总 Token 数 vs Provider Token 限制    │
└──────────────────┬──────────────────────┘
                   │
          Token 超限？──否──→ 返回 Context
                   │
                   是
                   ▼
┌──────────────────┬──────────────────────┐
│  3a. 压缩 (可选)  │                      │
│  StrategySummary │  3b. 截断             │
│  或 StrategyHybrid│  StrategySlideWindow │
│  LLM 生成旧消息   │  从最旧消息开始移除    │
│  摘要，替换原文    │  保留 System + Recent │
└──────────────────┴──────────┬───────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  4. 返回 Context  │
                    │  TokenCount ≤ Limit│
                    └─────────────────┘
```

### 4.2 在对话流程中的位置

```text
Session Manager（追加用户消息）
  │
  ▼
Context Manager ← Build()
  │
  │  构建 Context → 检查 Token → 压缩/截断
  │
  ▼
Agent → Provider（发送 Context）
  │
  │  LLM 返回 Tool Call？
  │  → 执行 Tool → 结果注入 Context → 再次 Build
  │
  ▼
Agent → Provider（最终回复）
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [manager.md](manager.md) | Context Manager 详解 — Build / Compress / Truncate 实现、ContextOption、Context 结构体、Go 代码示例 |
| [config-ref.md](config-ref.md) | 配置参考 — 全局 Context 策略、Agent 级覆盖、Token 预算分配 |
| [errors.md](errors.md) | 错误处理 — 错误分类、降级策略、重试 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策（CD-001 ~ CD-008）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-16*
