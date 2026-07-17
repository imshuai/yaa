# Session 与 Agent / Context / Memory 的集成

> 文档路径: `docs/session/integration.md`
> 上级: `docs/session/README.md` §2

---

## 1. 概述

Session 不是孤立存在的，它是 Yaa! 运行时中连接 Agent、Context、Memory 三大核心模块的枢纽。本文档描述 Session 如何被 Agent 管理和调度、Context 如何从 Session 消息历史构建 LLM 请求、以及 Memory 如何与 Session 关联实现跨会话记忆。

```text
┌─────────────────────────────────────────────────────────────┐
│                        Agent                                 │
│  ┌───────────────────────────────────────────────────────┐  │
│  │  Agent 管理 0..N 个 Session                             │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐            │  │
│  │  │ Session A │  │ Session B │  │ Session C │            │  │
│  │  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘            │  │
│  └────────┼─────────────┼─────────────┼──────────────────┘  │
│           │             │             │                      │
│           ▼             ▼             ▼                      │
│  ┌────────────────────────────────────────────────────────┐  │
│  │              Context Builder                            │  │
│  │  从 Session.Messages 构建 LLM 请求消息序列               │  │
│  │  注入 System Prompt / Skill Prompt / Memory 摘要        │  │
│  └───────────────────────┬────────────────────────────────┘  │
│                           │                                   │
│                           ▼                                   │
│  ┌────────────────────────────────────────────────────────┐  │
│  │              Memory Manager                             │  │
│  │  写入: Session 关闭时归档消息摘要                        │  │
│  │  读取: Session 创建时注入历史记忆摘要                     │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Session 与 Agent 的集成

### 2.1 Agent 对 Session 的管理

Agent 是 Session 的拥有者。每个 Agent 维护一组 Session，负责创建、调度和清理。

| 职责 | 方法 | 说明 |
|------|------|------|
| 创建 Session | `agent.NewSession()` | 委托 SessionManager.Create，自动绑定 AgentID |
| 查找 Session | `agent.GetSession(id)` | 从 SessionManager 获取 |
| 列出 Session | `agent.ListSessions()` | 返回该 Agent 下所有 Active/Paused Session |
| 关闭 Session | `agent.CloseSession(id)` | 委托 SessionManager.Close |
| 清理超时 | `agent.reapIdle()` | 定时关闭空闲 Session |

### 2.2 Agent 调度 Session 的流程

```go
// Agent 处理一条用户消息的完整流程
func (a *Agent) HandleMessage(ctx context.Context, sessionID string, content string) (*Response, error) {
    // 1. 获取 Session，验证归属
    sess, err := a.sessionMgr.Get(sessionID)
    if err != nil {
        return nil, fmt.Errorf("get session: %w", err)
    }
    if sess.AgentID != a.ID {
        return nil, ErrSessionNotOwned
    }

    // 2. 追加用户消息
    userMsg := Message{Role: RoleUser, Content: content}
    if err := a.sessionMgr.AppendMessage(sessionID, userMsg); err != nil {
        return nil, fmt.Errorf("append message: %w", err)
    }

    // 3. 从 Session 构建 Context
    ctxBuilder := NewContextBuilder(a.config.Context)
    llmMessages, err := ctxBuilder.Build(sess)
    if err != nil {
        return nil, fmt.Errorf("build context: %w", err)
    }

    // 4. 调用 LLM Provider
    resp, err := a.provider.Chat(ctx, llmMessages)
    if err != nil {
        return nil, fmt.Errorf("llm chat: %w", err)
    }

    // 5. 追加助手消息
    asstMsg := Message{Role: RoleAssistant, Content: resp.Content, ReasoningContent: resp.ReasoningContent, ToolCalls: resp.ToolCalls}
    if err := a.sessionMgr.AppendMessage(sessionID, asstMsg); err != nil {
        return nil, fmt.Errorf("append assistant message: %w", err)
    }

    // 6. 若有 Tool 调用，执行并追加结果
    if len(resp.ToolCalls) > 0 {
        toolResults := a.executeToolCalls(ctx, resp.ToolCalls)
        for _, tr := range toolResults {
            a.sessionMgr.AppendMessage(sessionID, tr)
        }
    }

    return &Response{Content: resp.Content}, nil
}
```

### 2.3 多 Session 并发模型

```text
Agent
  │
  ├── Session A (goroutine 1) ── 串行处理消息
  ├── Session B (goroutine 2) ── 串行处理消息
  └── Session C (goroutine 3) ── 串行处理消息

各 Session 间并行，Session 内串行（详见 concurrency.md）
```

---

## 3. Session 与 Context 的集成

### 3.1 Context 构建流程

Context Builder 负责将 Session 的消息历史转换为 LLM Provider 能接受的完整消息序列，并在其中注入 System Prompt、Skill Prompt 和 Memory 摘要。

```text
┌──────────┐     ┌──────────────────────┐     ┌───────────────┐
│  Session  │────▶│  Context Builder     │────▶│  LLM Messages │
│ Messages  │     │                      │     │  (最终请求)    │
└──────────┘     │  1. System Prompt     │     └───────────────┘
                  │  2. Memory 摘要      │
                  │  3. Skill Prompt     │
                  │  4. 历史消息 (截断)   │
                  │  5. 当前用户消息      │
                  └──────────────────────┘
```

### 3.2 Context Builder 实现

```go
// ContextBuilder 从 Session 构建 LLM 请求消息序列。
type ContextBuilder struct {
    config     ContextConfig
    memoryMgr  MemoryManager
    skillMgr   SkillManager
}

type ContextConfig struct {
    MaxMessages   int  // 最大历史消息数，默认 50
    MaxTokens     int  // 最大 token 数估算，默认 8192
    InjectMemory  bool // 是否注入 Memory 摘要，默认 true
    InjectSystem  bool // 是否注入 System Prompt，默认 true
}

// Build 将 Session 消息历史转换为 LLM 消息序列。
func (b *ContextBuilder) Build(sess *Session) ([]Message, error) {
    var messages []Message

    // 1. System Prompt
    if b.config.InjectSystem {
        messages = append(messages, Message{
            Role:    RoleSystem,
            Content: b.buildSystemPrompt(sess),
        })
    }

    // 2. Memory 摘要（注入到 System 消息之后）
    if b.config.InjectMemory {
        memSummary, err := b.memoryMgr.GetSummary(sess.AgentID, sess.ID)
        if err == nil && memSummary != "" {
            messages = append(messages, Message{
                Role:    RoleSystem,
                Content: fmt.Sprintf("[Memory]\n%s", memSummary),
            })
        }
    }

    // 3. Skill Prompt 注入
    skillPrompts := b.skillMgr.GetActivePrompts(sess.AgentID)
    for _, sp := range skillPrompts {
        messages = append(messages, Message{
            Role:    RoleSystem,
            Content: sp,
        })
    }

    // 4. 历史消息（截断 + Token 估算）
    history := b.truncateMessages(sess.Messages, b.config.MaxMessages, b.config.MaxTokens)
    messages = append(messages, history...)

    return messages, nil
}

// truncateMessages 按数量和 token 预算截断消息历史。
func (b *ContextBuilder) truncateMessages(msgs []Message, maxMsgs, maxTokens int) []Message {
    // 从末尾保留最近的消息
    if len(msgs) > maxMsgs {
        msgs = msgs[len(msgs)-maxMsgs:]
    }

    // 粗略 token 估算: 1 token ≈ 4 chars
    totalTokens := 0
    result := make([]Message, 0, len(msgs))
    for i := len(msgs) - 1; i >= 0; i-- {
        est := len(msgs[i].Content) / 4
        if totalTokens+est > maxTokens {
            break
        }
        totalTokens += est
        result = append([]Message{msgs[i]}, result...)
    }
    return result
}
```

### 3.3 消息序列构建示例

| 步骤 | 来源 | Role | 内容示例 |
|------|------|------|---------|
| 1 | Agent 配置 | system | "你是 Yaa! 助手，负责…" |
| 2 | Memory Manager | system | "[Memory] 用户偏好中文回复" |
| 3 | Skill Manager | system | "可用工具: weather, web_search" |
| 4 | Session 历史 | user | "今天天气怎么样？" |
| 5 | Session 历史 | assistant | "让我查一下…" |
| 6 | Session 历史 | tool | `{"temp": 28, "city": "上海"}` |
| 7 | Session 历史 | assistant | "上海今天 28°C，晴天。" |
| 8 | 当前请求 | user | "那明天呢？" |

---

## 4. Session 与 Memory 的集成

### 4.1 Memory 关联模型

Memory 与 Session 的关系分两层：

| 层级 | 作用域 | 生命周期 | 说明 |
|------|--------|---------|------|
| Agent 级 Memory | 跨所有 Session | Agent 存在期间 | 用户偏好、长期知识 |
| Session 级 Memory | 单个 Session | Session 生命周期 | 本轮对话的关键事实摘要 |

```text
Agent
  │
  ├── Agent Memory (持久)     ─── 跨 Session 共享
  │     "用户偏好中文、时区 UTC+8"
  │
  ├── Session A Memory
  │     "用户在讨论天气，关注上海"
  │
  └── Session B Memory
        "用户在调试代码，Go 项目"
```

### 4.2 Memory 写入时机

```go
// MemoryManager 在 Session 生命周期的关键节点写入记忆。
type MemoryManager interface {
    // OnMessage 在每条消息后更新 Session 级摘要
    OnMessage(sess *Session, msg Message) error

    // OnClose 在 Session 关闭时将摘要归档到 Agent 级 Memory
    OnClose(sess *Session) error

    // GetSummary 获取 Session 相关的记忆摘要（供 Context Builder 使用）
    GetSummary(agentID, sessionID string) (string, error)
}
```

### 4.3 Memory 写入实现

```go
// OnClose 在 Session 关闭时提取摘要并写入 Agent 级 Memory。
func (m *MemoryManager) OnClose(sess *Session) error {
    // 1. 从 Session 消息历史提取关键信息
    summary := m.summarizeSession(sess.Messages)

    // 2. 写入 Agent 级 Memory（持久化）
    entry := MemoryEntry{
        ID:        "mem_" + ulid.Make().String(),
        AgentID:   sess.AgentID,
        SessionID: sess.ID,
        Content:   summary,
        Type:      MemoryTypeSessionSummary,
        CreatedAt: time.Now(),
    }
    if err := m.store.Save(entry); err != nil {
        return fmt.Errorf("save memory: %w", err)
    }

    // 3. 清理 Session 级临时 Memory
    m.sessionCache.Delete(sess.ID)

    m.logger.Info("memory archived",
        "agent_id", sess.AgentID,
        "session_id", sess.ID,
        "summary_len", len(summary))
    return nil
}

// summarizeSession 用 LLM 提取对话摘要。
func (m *MemoryManager) summarizeSession(msgs []Message) string {
    if len(msgs) == 0 {
        return ""
    }

    // 构建摘要请求
    var sb strings.Builder
    for _, msg := range msgs {
        sb.WriteString(fmt.Sprintf("[%s] %s\n", msg.Role, msg.Content))
    }

    prompt := fmt.Sprintf("请用一段话总结以下对话的关键信息:\n\n%s", sb.String())
    resp, err := m.provider.Chat(context.Background(), []Message{
        {Role: RoleSystem, Content: "你是一个对话摘要助手。"},
        {Role: RoleUser, Content: prompt},
    })
    if err != nil {
        m.logger.Warn("summarize failed, using fallback", "error", err)
        // 降级: 取最后几条消息作为摘要
        return m.fallbackSummary(msgs)
    }
    return resp.Content
}
```

### 4.4 Memory 读取与注入

Session 创建或首次构建 Context 时，Memory Manager 检索 Agent 级历史记忆，注入到 Context 中：

```go
// GetSummary 返回 Agent 级 + Session 级记忆摘要。
func (m *MemoryManager) GetSummary(agentID, sessionID string) (string, error) {
    var parts []string

    // 1. Agent 级 Memory（跨 Session 的长期记忆）
    agentMems, err := m.store.ListByAgent(agentID, MemoryTypeAgentLongTerm)
    if err == nil {
        for _, mem := range agentMems {
            parts = append(parts, mem.Content)
        }
    }

    // 2. Session 级 Memory（当前会话的实时摘要）
    if cached, ok := m.sessionCache.Get(sessionID); ok {
        parts = append(parts, cached.(string))
    }

    return strings.Join(parts, "\n---\n"), nil
}
```

---

## 5. 三模块协作完整流程

```text
用户消息到达
     │
     ▼
┌─────────────┐
│  Agent       │  1. 识别目标 Session
└──────┬──────┘  2. 验证归属与状态
       │
       ▼
┌─────────────┐
│  Session     │  3. AppendMessage(user)
│  Manager     │  4. 状态检查 + 持久化
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Memory      │  5. GetSummary(agentID, sessionID)
│  Manager     │  6. 返回历史记忆摘要
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Context     │  7. Build(sess)
│  Builder     │  8. System + Memory + Skill + History
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Provider    │  9. Chat(llmMessages)
│              │  10. 返回 LLM 响应
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Session     │  11. AppendMessage(assistant)
│  Manager     │  12. 持久化
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Memory      │  13. OnMessage(sess, msg)
│  Manager     │  14. 更新 Session 级实时摘要
└─────────────┘
       │
       ▼
   返回响应给用户
```

---

## 6. 接口依赖关系

```go
// Agent 持有的依赖
type Agent struct {
    ID          string
    config      AgentConfig
    sessionMgr  SessionManager
    contextBld  *ContextBuilder
    memoryMgr   MemoryManager
    skillMgr    SkillManager
    provider    Provider
    toolMgr     ToolManager
}
```

| 依赖 | 接口 | Session 相关方法 | 调用时机 |
|------|------|-----------------|---------|
| SessionManager | `session.Manager` | Get / AppendMessage / Close | 每次消息处理 |
| ContextBuilder | `*ContextBuilder` | Build(sess) | 每次 LLM 调用前 |
| MemoryManager | `MemoryManager` | GetSummary / OnMessage / OnClose | Context 构建 + 消息后 + 关闭时 |
| SkillManager | `SkillManager` | GetActivePrompts | Context 构建时 |
| Provider | `Provider` | Chat | Context 构建后 |

---

*最后更新: 2025-07-17*
