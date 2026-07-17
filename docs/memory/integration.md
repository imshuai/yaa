# Memory 集成设计

> Memory 与 Session / Context / Agent 的集成方案
> 依赖: `architecture.md` §3.2/§3.3/§3.4/§3.6, `memory/README.md` §2

---

## 1. 集成总览

Memory 系统不是孤立模块，它深度嵌入 Agent Loop 的各个阶段。下表展示各模块与 Memory 的交互关系：

| 模块 | 交互方向 | 时机 | 操作 |
|------|----------|------|------|
| **Agent** | 双向 | Agent 创建时 | 获取/懒加载 Memory 实例 |
| **Session** | 读+写 | Session 创建/关闭 | 加载关联记忆；关闭时持久化短期记忆 |
| **Context** | 读 | Context 构建时 | 检索长期记忆并注入上下文窗口 |
| **Agent Loop** | 读+写 | 每轮对话 | 读：构建前检索；写：回复后提取记忆 |

---

## 2. Agent Loop 中的记忆读写时机

```text
┌─────────────────────────────────────────────────────────┐
│                    Agent Loop (单轮)                     │
│                                                         │
│  ① 用户消息到达                                          │
│     │                                                   │
│     ▼                                                   │
│  ② Session 追加用户消息                                  │
│     │                                                   │
│     ▼                                                   │
│  ③ Memory 检索（读）                                     │
│     │  query = 用户最新消息                               │
│     │  results = memory.Search(query, limit)              │
│     ▼                                                   │
│  ④ Context 构建                                         │
│     │  System Prompt + 历史消息 + Memory 结果 + Tool 结果  │
│     │  → 截断/压缩至 Token 限制                            │
│     ▼                                                   │
│  ⑤ Provider 调用 (LLM)                                  │
│     │                                                   │
│     ├──── 有 Tool Call ──→ 执行 Tool ──→ 回到 ④          │
│     │                                                   │
│     ▼                                                   │
│  ⑥ LLM 返回最终回复                                      │
│     │                                                   │
│     ▼                                                   │
│  ⑦ Session 追加助手消息                                  │
│     │                                                   │
│     ▼                                                   │
│  ⑧ Memory 写入（写）                                     │
│     │  提取关键信息 → memory.Add(...)                      │
│     │  短期记忆自动存储                                   │
│     ▼                                                   │
│  ⑨ 返回给 Client                                        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### 2.1 读时机详解

| 步骤 | 触发条件 | 检索策略 | 注入位置 |
|------|----------|----------|----------|
| ③ | 每轮 Context 构建前 | 语义搜索用户最新消息 | 作为 System 消息的补充段 |
| ④ | Context 压缩时 | 检索 Summary 层记忆 | 替换被压缩的历史消息 |

### 2.2 写时机详解

| 步骤 | 触发条件 | 写入层级 | 写入内容 |
|------|----------|----------|----------|
| ② | 用户消息到达 | Short-term | 原始用户消息 |
| ⑦ | 助手回复完成 | Short-term | 原始助手消息 |
| ⑧ | 助手回复后 | Long-term | 提取的关键事实/偏好 |
| Session 关闭 | Session 结束 | Summary | Session 对话摘要 |

---

## 3. 记忆检索注入 Context

Memory 检索结果以结构化方式注入 Context，而非简单拼接文本。

### 3.1 注入格式

检索到的记忆被包装为一段 system 角色的补充消息，注入到 Context 中：

```text
[System Prompt]

--- Relevant Memories ---
[1] (score: 0.92) 用户偏好: 喜欢用中文交流
[2] (score: 0.87) 项目信息: 正在开发 Yaa! Agent Runtime
[3] (score: 0.81) 历史决策: 选择了 SQLite 作为默认存储
--- End Memories ---

[历史消息...]
```

### 3.2 注入策略

| 策略 | 说明 | 适用场景 |
|------|------|----------|
| **Top-K 语义检索** | 按相似度取前 K 条 | 默认策略，K=5 |
| **元数据过滤** | 按 tag/layer/time 过滤后再检索 | 需要 scope 隔离时 |
| **混合检索** | 语义 + 关键词，去重合并 | 向量搜索召回不足时 |
| **摘要优先** | 优先注入 Summary 层记忆 | Context 被压缩时 |

### 3.3 Token 预算

Memory 注入受 Token 预算约束，不会无限制占用上下文窗口：

```go
// Memory 注入的 Token 预算计算
memoryBudget := totalTokenLimit
    - systemPromptTokens     // System Prompt 固定开销
    - minHistoryTokens       // 保留的最小历史消息
    - toolResultTokens       // 当前轮 Tool 结果
    - reservedForResponse    // 预留给 LLM 回复的空间
```

---

## 4. Go 代码示例

### 4.1 Agent Loop 集成

```go
// agent_loop.go — Agent 主循环中的 Memory 读写集成

func (a *Agent) handleMessage(ctx context.Context, sess *session.Session, userMsg string) (string, error) {
    // ② 追加用户消息到 Session
    sess.AppendMessage(session.RoleUser, userMsg)

    // ③ Memory 检索：用用户消息作为查询
    memResults, err := a.Memory.Search(userMsg, 5)
    if err != nil {
        a.logger.Warn("memory search failed, degrading", "error", err)
        memResults = nil // 优雅降级：搜索失败不阻塞对话
    }

    // ④ 构建 Context（注入 Memory 检索结果）
    context, err := a.ContextMgr.Build(sess,
        context.WithMemory(memResults),
        context.WithTools(a.Tools),
    )
    if err != nil {
        return "", fmt.Errorf("context build: %w", err)
    }

    // ⑤ 调用 LLM（含 Tool Loop）
    response, err := a.runWithTools(ctx, context)
    if err != nil {
        return "", err
    }

    // ⑦ 追加助手消息到 Session
    sess.AppendMessage(session.RoleAssistant, response)

    // ⑧ Memory 写入：异步提取并存储关键信息
    go a.persistMemory(response, userMsg)

    return response, nil
}
```

### 4.2 Context 构建中的记忆注入

```go
// context_builder.go — Context 构建器注入 Memory

func (b *ContextBuilder) Build(sess *session.Session, opts ...ContextOption) (*Context, error) {
    ctx := &Context{Session: sess}
    for _, opt := range opts {
        opt(ctx)
    }

    // 构建 System Prompt
    parts := []string{ctx.SystemPrompt}

    // 注入 Memory 检索结果
    if len(ctx.Memories) > 0 {
        parts = append(parts, formatMemories(ctx.Memories))
    }

    // 组装消息列表
    ctx.Messages = []Message{
        {Role: RoleSystem, Content: strings.Join(parts, "\n\n")},
    }
    ctx.Messages = append(ctx.Messages, sess.Messages...)

    // Token 截断
    return b.truncate(ctx)
}

// formatMemories 将检索结果格式化为注入文本
func formatMemories(items []*memory.MemoryItem) string {
    var b strings.Builder
    b.WriteString("--- Relevant Memories ---\n")
    for i, item := range items {
        fmt.Fprintf(&b, "[%d] (score: %.2f) %s\n", i+1, item.Score, item.Content)
    }
    b.WriteString("--- End Memories ---")
    return b.String()
}
```

### 4.3 Session 关闭时的记忆持久化

```go
// session_close.go — Session 关闭时晋升短期记忆、生成摘要

func (m *SessionManager) Close(sessID string) error {
    sess, err := m.get(sessID)
    if err != nil {
        return err
    }

    // 生成 Session 摘要
    summary, err := m.summarizer.Summarize(sess.Messages)
    if err != nil {
        m.logger.Warn("summary generation failed", "error", err)
    } else {
        // 写入 Summary 层记忆
        key := fmt.Sprintf("session:%s:summary", sess.ID)
        _ = sess.Agent.Memory.Add(key, summary, map[string]any{
            "layer":      memory.LayerSummary,
            "session_id": sess.ID,
            "agent_id":   sess.AgentID,
            "created_at": time.Now(),
        })
    }

    // 短期记忆晋升：将关键短期记忆提升为长期记忆
    if ext, ok := sess.Agent.Memory.(memory.MemoryExtended); ok {
        items, _ := ext.ListByLayer(memory.LayerShortTerm, 100, 0)
        for _, item := range items {
            if shouldPromote(item) {
                _ = ext.Promote(item.Key)
            }
        }
    }

    return m.store.Delete(sessID)
}
```

---

## 5. 完整集成流程图

```text
Session 创建                     Agent Loop                      Session 关闭
     │                              │                               │
     ▼                              │                               │
┌──────────┐                       │                        ┌──────────┐
│ Session  │                       │                        │ 摘要生成 │
│ Manager  │                       │                        │  Summary │
└────┬─────┘                       │                        └────┬─────┘
     │                              │                             │
     │ 加载关联记忆                  │                             │ 写入 Summary
     ▼                              │                             ▼
┌──────────┐    ③检索    ┌──────────┐    ⑧写入    ┌──────────────┐
│  Memory  │◄───────────│  Context │───────────►│    Memory    │
│  Store   │            │  Builder │            │    Store     │
└──────────┘            └────┬─────┘            └──────────────┘
     ▲                       │                       ▲
     │                       │ ④构建                  │ ⑦晋升
     │                       ▼                       │
     │                  ┌──────────┐                  │
     │     ⑤调用        │  Agent   │     ⑥回复        │
     │◄─────────────────│   Loop   │─────────────────►│
     │                   └──────────┘                  │
     │                                                  │
     └────────────── ③ 检索 / ⑧ 写入 ──────────────────┘
```

---

## 6. 错误处理与降级

| 场景 | 处理策略 | 用户感知 |
|------|----------|----------|
| 检索失败 | 跳过 Memory 注入，继续对话 | 无感知，回复可能缺少上下文 |
| 写入失败 | 日志记录，不阻塞回复 | 无感知，该轮记忆丢失 |
| 向量搜索不可用 | 回退到关键词搜索 | 检索质量略降 |
| 存储后端宕机 | 短期记忆保留在内存，长期记忆排队重试 | 短期内无影响 |

**核心原则：** Memory 系统的任何故障都不应阻塞 Agent Loop 的主流程，始终优雅降级。

---

## 7. 配置控制

```yaml
agents:
  - id: "default"
    memory:
      enabled: true
      search:
        limit: 5                    # 检索返回条数
        min_score: 0.7               # 最低相关性分数
        fallback_to_keyword: true    # 向量不可用时回退关键词
      injection:
        max_tokens: 500              # Memory 注入的最大 Token 数
        position: "after_system"     # 注入位置
      persistence:
        auto_promote: true           # 短期记忆自动晋升
        summary_on_close: true       # Session 关闭时生成摘要
```

---

*最后更新: 2025-07-17*
