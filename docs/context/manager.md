# Context Manager 详解

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/context/manager.md`
> 依赖: `docs/context/README.md`、`docs/architecture.md` §3.4

---

## 1. 概述

Context Manager 是 Context 系统的核心组件，负责将 Session 中的
原始消息历史转化为 Provider 可接受、Token 限制允许的结构化上下文。

本文档详细描述 `ContextManager` 接口的三个核心方法——
`Build`、`Compress`、`Truncate`——的内部实现逻辑，
以及 `ContextOption`、`Context` 结构体的完整定义。

---

## 2. 数据结构

### 2.1 Context

```go
// Context 是构建完成后、可直接发送给 Provider 的上下文。
type Context struct {
    SystemPrompt string             // 系统提示词
    Messages    []ContextMessage   // 有序消息列表（不含 system）
    TokenCount  int                // 当前总 Token 数（含 system）
    TokenLimit  int                // Token 上限（来自 Provider 配置）
    Metadata    *ContextMetadata   // 构建过程的元信息
}

// ContextMetadata 记录 Context 构建过程的关键信息。
type ContextMetadata struct {
    SourceSessionID  string         // 来源 Session ID
    OriginalCount    int            // 原始消息数
    FinalCount       int            // 最终消息数
    CompressedCount  int            // 被压缩的消息数
    TruncatedCount   int            // 被截断的消息数
    Strategy         Strategy      // 使用的策略
    BuildDuration    time.Duration  // 构建耗时
}
```

### 2.2 ContextMessage

```go
// ContextMessage 是 Context 中的单条消息。
type ContextMessage struct {
    Role       string      // user / assistant / tool
    Content    string      // 消息文本内容
    ToolCalls  []ToolCall  // assistant 的 Tool 调用（可空）
    ToolCallID string      // tool 角色消息的关联 ID
    TokenCount int         // 该消息的 Token 数
    Source     MessageSource // 消息来源
    Compressed bool        // 是否经过摘要压缩
}

type ToolCall struct {
    ID       string
    Name     string
    Arguments string  // JSON 格式的参数
}
```

### 2.3 ContextOption

```go
// ContextOption 用于在 Build 时调整构建行为。
type ContextOption func(*contextConfig)

type contextConfig struct {
    maxTokens     int              // Token 上限，0 = 取 Provider 默认
    strategy      Strategy         // 压缩/截断策略
    keepRecent    int              // 压缩时保留的最近消息数
    memorySummary string           // 注入的 Memory 摘要文本
    toolResults   []ContextMessage // 额外注入的 Tool 结果
}

func WithMaxTokens(n int) ContextOption {
    return func(c *contextConfig) { c.maxTokens = n }
}

func WithStrategy(s Strategy) ContextOption {
    return func(c *contextConfig) { c.strategy = s }
}

func WithKeepRecent(n int) ContextOption {
    return func(c *contextConfig) { c.keepRecent = n }
}

func WithMemorySummary(s string) ContextOption {
    return func(c *contextConfig) { c.memorySummary = s }
}

func WithToolResults(msgs ...ContextMessage) ContextOption {
    return func(c *contextConfig) { c.toolResults = msgs }
}
```

---

## 3. Build 方法

`Build` 是 Context Manager 的入口，从 Session 构建完整 Context。

### 3.1 实现逻辑

```go
func (m *Manager) Build(
    sess *session.Session,
    opts ...ContextOption,
) (*Context, error) {
    start := time.Now()

    // 1. 应用 Options
    cfg := m.defaultConfig(sess)
    for _, opt := range opts {
        opt(cfg)
    }

    // 2. 收集消息：System Prompt + Session 消息 + Memory + Tool 结果
    ctx := &Context{
        SystemPrompt: sess.SystemPrompt,
        TokenLimit:   cfg.maxTokens,
        Metadata: &ContextMetadata{
            SourceSessionID: sess.ID,
            Strategy:        cfg.strategy,
        },
    }

    // 2a. 注入 Memory 摘要（如果有）
    if cfg.memorySummary != "" {
        ctx.Messages = append(ctx.Messages, ContextMessage{
            Role:    "system",
            Content: "[Memory Summary]\n" + cfg.memorySummary,
            Source:  SourceMemory,
        })
    }

    // 2b. 转换 Session 消息为 ContextMessage
    for _, msg := range sess.Messages {
        ctx.Messages = append(ctx.Messages, toContextMessage(msg))
    }
    ctx.Metadata.OriginalCount = len(ctx.Messages)

    // 2c. 注入额外 Tool 结果
    ctx.Messages = append(ctx.Messages, cfg.toolResults...)

    // 3. 计算 Token
    ctx.TokenCount = m.countTokens(ctx)

    // 4. 超限处理：根据策略压缩或截断
    if ctx.TokenCount > ctx.TokenLimit {
        switch cfg.strategy {
        case StrategySummary:
            ctx, _ = m.Compress(ctx)
        case StrategySlideWindow:
            ctx, _ = m.Truncate(ctx, ctx.TokenLimit)
        case StrategyHybrid:
            ctx, _ = m.Compress(ctx)       // 先压缩
            if ctx.TokenCount > ctx.TokenLimit {
                ctx, _ = m.Truncate(ctx, ctx.TokenLimit) // 仍超限再截断
            }
        case StrategyNone:
            // 不处理，直接返回（可能超限）
        }
    }

    // 5. 记录元信息
    ctx.Metadata.FinalCount = len(ctx.Messages)
    ctx.Metadata.BuildDuration = time.Since(start)

    return ctx, nil
}
```

### 3.2 Build 流程图

```text
         Session + Options
               │
               ▼
    ┌────────────────────┐
    │  应用 ContextOption  │
    └────────┬───────────┘
             │
             ▼
    ┌────────────────────┐
    │  收集消息             │
    │  System + History   │
    │  + Memory + Tool    │
    └────────┬───────────┘
             │
             ▼
    ┌────────────────────┐
    │  计算 Token 总数     │
    └────────┬───────────┘
             │
      超限？─否─→ 返回 Context
             │是
             ▼
    ┌────────┬───────────┐
    │Summary │SlideWindow│
    │ 压缩   │  截断      │
    └────────┴────┬──────┘
                  │
          Hybrid？─是─→ 压缩后再截断
                  │否
                  ▼
           返回 Context
```

---

## 4. Compress 方法

`Compress` 将较旧的消息通过 LLM 生成摘要，替换原始消息以减少 Token。

### 4.1 实现逻辑

```go
func (m *Manager) Compress(ctx *Context) (*Context, error) {
    cfg := m.compressConfig // 全局压缩配置
    keepRecent := cfg.keepRecent
    if keepRecent == 0 {
        keepRecent = 10 // 默认保留最近 10 条
    }

    // 1. 分区：需要压缩的消息 vs 保留的消息
    total := len(ctx.Messages)
    if total <= keepRecent {
        return ctx, nil // 消息太少，无需压缩
    }

    toCompress := ctx.Messages[:total-keepRecent]
    toKeep := ctx.Messages[total-keepRecent:]

    // 2. 将待压缩消息拼接为摘要请求
    transcript := messagesToTranscript(toCompress)
    summary, err := m.summarize(transcript)
    if err != nil {
        // 压缩失败，回退为截断
        return m.Truncate(ctx, ctx.TokenLimit)
    }

    // 3. 用摘要消息替换被压缩的部分
    summaryMsg := ContextMessage{
        Role:       "system",
        Content:    "[Previous Conversation Summary]\n" + summary,
        Source:     SourceSession,
        Compressed: true,
    }

    // 4. 重组 Context
    ctx.Messages = append([]ContextMessage{summaryMsg}, toKeep...)
    ctx.Metadata.CompressedCount = len(toCompress)

    // 5. 重新计算 Token
    ctx.TokenCount = m.countTokens(ctx)

    return ctx, nil
}

// summarize 调用 LLM 生成对话摘要。
func (m *Manager) summarize(transcript string) (string, error) {
    prompt := fmt.Sprintf(`请将以下对话历史压缩为简洁的摘要，
保留关键信息、决策和未完成的任务：

%s`, transcript)

    resp, err := m.provider.Chat(context.Background(), &ChatRequest{
        Messages: []ChatMessage{
            {Role: "user", Content: prompt},
        },
    })
    if err != nil {
        return "", err
    }
    return resp.Content, nil
}
```

### 4.2 压缩示意

```text
压缩前（25 条消息，8000 tokens）：
┌──────────────────────────────────┐
│ msg 1  msg 2  msg 3 ... msg 15  │ ← 旧消息（待压缩）
├──────────────────────────────────┤
│ msg 16 msg 17 ... msg 25        │ ← 最近 10 条（保留）
└──────────────────────────────────┘

压缩后（11 条消息，3000 tokens）：
┌──────────────────────────────────┐
│ [Summary] msg1~15 的摘要          │ ← 1 条摘要消息
├──────────────────────────────────┤
│ msg 16 msg 17 ... msg 25        │ ← 原样保留
└──────────────────────────────────┘
```

---

## 5. Truncate 方法

`Truncate` 从最旧的消息开始移除，直到 Token 数不超过限制。
System Prompt 和最近的 Tool 结果不会被截断。

### 5.1 实现逻辑

```go
func (m *Manager) Truncate(
    ctx *Context,
    maxTokens int,
) (*Context, error) {
    // System Prompt 的 Token 数始终计入，不可移除
    systemTokens := m.tokenizer.Count(ctx.SystemPrompt)
    budget := maxTokens - systemTokens
    if budget <= 0 {
        return nil, fmt.Errorf("system prompt exceeds token limit")
    }

    // 从最新消息开始向前保留，直到预算耗尽
    kept := make([]ContextMessage, 0, len(ctx.Messages))
    usedTokens := 0

    for i := len(ctx.Messages) - 1; i >= 0; i-- {
        msg := ctx.Messages[i]

        // Tool 结果如果是最近一轮的，强制保留
        if msg.Source == SourceTool && i >= len(ctx.Messages)-3 {
            kept = append([]ContextMessage{msg}, kept...)
            usedTokens += msg.TokenCount
            continue
        }

        if usedTokens+msg.TokenCount > budget {
            // 预算不够，停止保留
            break
        }

        kept = append([]ContextMessage{msg}, kept...)
        usedTokens += msg.TokenCount
    }

    ctx.Metadata.TruncatedCount = len(ctx.Messages) - len(kept)
    ctx.Messages = kept
    ctx.TokenCount = systemTokens + usedTokens

    return ctx, nil
}
```

### 5.2 截断示意

```text
截断前（20 条消息，6000 tokens，上限 4000）：
┌──────────────────────────────────────┐
│ msg1  msg2  msg3 ... msg20           │  6000 tokens
└──────────────────────────────────────┘

从最新向前保留，直到预算耗尽：

截断后（14 条消息，3900 tokens）：
                              ┌──────────────────────────┐
                              │ msg7 msg8 ... msg20      │  3900 tokens
                              └──────────────────────────┘
        ← 丢弃 →
   ┌──────────────┐
   │ msg1 ~ msg6  │  2100 tokens 被移除
   └──────────────┘
```

---

## 6. Manager 完整结构

```go
type Manager struct {
    tokenizer       Tokenizer       // Token 计算器
    provider        provider.Provider // 用于压缩时的 LLM 调用
    defaultStrategy Strategy        // 默认策略
    compressConfig  compressConfig  // 压缩配置
    mu              sync.Mutex      // 并发保护
}

type compressConfig struct {
    keepRecent     int           // 压缩时保留的最近消息数
    maxCompressTokens int        // 摘要请求的最大 Token
    summaryModel   string        // 压缩使用的模型（可用廉价模型）
}

type Tokenizer interface {
    Count(text string) int
    CountMessages(msgs []ContextMessage) int
}
```

### 6.1 配置示例

```yaml
# yaa.yaml 中的 Context 配置
context:
  default_strategy: hybrid       # none | slide_window | summary | hybrid
  keep_recent: 10                # 压缩时保留最近消息数
  max_tokens_override: 0         # 0 = 使用 Provider 默认限制
  compress:
    summary_model: "gpt-4o-mini"  # 压缩使用的廉价模型
    max_compress_tokens: 2000    # 摘要请求 Token 上限
```

---

## 7. 策略对比

| 策略 | 速度 | 信息保留 | Token 节省 | 适用场景 |
|------|------|---------|-----------|---------|
| SlideWindow | ⚡ 最快 | ❌ 旧消息丢弃 | 中 | 实时对话、低延迟要求 |
| Summary | 🐢 较慢 | ✅ 摘要保留 | 高 | 长对话、需要保留上下文 |
| Hybrid | 🔀 中等 | ✅ 先摘要后截断 | 最高 | 生产环境默认推荐 |
| None | ⚡ 最快 | ✅ 全部保留 | 无 | 调试、小上下文场景 |

---

*最后更新: 2025-07-17*
