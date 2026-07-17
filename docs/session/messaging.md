# Session 消息管理

> 文档路径: `docs/session/messaging.md`
> 上级: `docs/session/README.md` §2

---

## 1. 概述

消息是 Session 的核心数据单元。本文档定义消息类型、历史查询接口、流式输出机制（SSE / WebSocket）。

---

## 2. 消息类型

```go
type MessageRole string

const (
    RoleUser      MessageRole = "user"
    RoleAssistant MessageRole = "assistant"
    RoleTool      MessageRole = "tool"
    RoleSystem    MessageRole = "system"
)
```

| Role | 生成方 | 持久化 | 说明 |
|------|--------|--------|------|
| `user` | Remote API Client | ✅ | 用户输入文本 |
| `assistant` | Provider 返回 | ✅ | LLM 响应（纯文本或含 ToolCalls） |
| `tool` | Tool Manager | ✅ | Tool 执行结果，关联 ToolCallID |
| `system` | Skill/Context Manager | ❌ | 系统注入（Skill Prompt 等），不存入 Session |

### 2.1 消息结构

```go
type Message struct {
    ID               string         `json:"id"`
    Role             MessageRole    `json:"role"`
    Content          string         `json:"content"`
    ReasoningContent string         `json:"reasoning_content,omitempty"`
    ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
    ToolCallID       string         `json:"tool_call_id,omitempty"`
    Name             string         `json:"name,omitempty"`
    CreatedAt        time.Time      `json:"created_at"`
    Metadata         map[string]any `json:"metadata,omitempty"`
}

type ToolCall struct {
    ID        string         `json:"id"`
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments"`
}
```

### 2.2 消息流转示例

```text
[user] "帮我查一下北京天气"
   │
   ▼
[assistant] ToolCall{id: "tc_1", name: "weather", args: {city: "北京"}}
   │
   ▼
[tool] ToolCallID: "tc_1", Content: "北京 25°C 晴"
   │
   ▼
[assistant] "北京今天 25°C，晴天，适合出行。"
```

---

## 3. 消息追加

```go
func (m *Manager) AppendMessage(sessionID string, msg Message) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    sess, ok := m.sessions[sessionID]
    if !ok {
        return ErrSessionNotFound
    }
    if err := sess.ensureState(SessionStateCreated, SessionStateActive); err != nil {
        return err
    }

    // 首条消息触发 Created → Active
    if sess.State == SessionStateCreated {
        sess.State = SessionStateActive
    }

    msg.ID = "msg_" + ulid.Make().String()
    msg.CreatedAt = time.Now()
    sess.Messages = append(sess.Messages, msg)
    sess.UpdatedAt = time.Now()

    // 通知流式订阅者
    m.notifySubscribers(sessionID, msg)

    return m.persist(sess)
}
```

---

## 4. 历史查询

```go
type MessageQuery struct {
    Limit  int          // 最大返回条数
    Offset int          // 偏移量（分页）
    Role   MessageRole  // 按角色过滤
    Since  time.Time    // 返回此时间之后的消息
    Until  time.Time    // 返回此时间之前的消息
}

func (m *Manager) GetMessages(sessionID string, opts ...MessageQueryOption) ([]Message, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    sess, ok := m.sessions[sessionID]
    if !ok {
        return nil, ErrSessionNotFound
    }

    q := &MessageQuery{Limit: 100}
    for _, opt := range opts {
        opt(q)
    }

    msgs := sess.Messages

    // 过滤
    if q.Role != "" {
        msgs = filterByRole(msgs, q.Role)
    }
    if !q.Since.IsZero() {
        msgs = filterSince(msgs, q.Since)
    }
    if !q.Until.IsZero() {
        msgs = filterUntil(msgs, q.Until)
    }

    // 分页
    if q.Offset > 0 && q.Offset < len(msgs) {
        msgs = msgs[q.Offset:]
    }
    if q.Limit > 0 && q.Limit < len(msgs) {
        msgs = msgs[:q.Limit]
    }

    return msgs, nil
}
```

### 4.1 查询选项

| 选项 | 方法 | 说明 |
|------|------|------|
| 限制条数 | `WithLimit(n)` | 默认 100 |
| 偏移量 | `WithOffset(n)` | 分页 |
| 角色过滤 | `WithRole(r)` | 仅返回指定角色 |
| 时间范围 | `WithSince(t)` / `WithUntil(t)` | 时间范围查询 |

---

## 5. 流式输出

### 5.1 SSE (Server-Sent Events)

```text
GET /api/v1/sessions/:id/events
Accept: text/event-stream
```

```go
func (h *SessionHandler) HandleSSE(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "id")
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    sub := h.manager.Subscribe(sessionID)
    defer h.manager.Unsubscribe(sessionID, sub)

    flusher, _ := w.(http.Flusher)
    ctx := r.Context()

    for {
        select {
        case <-ctx.Done():
            return
        case event := <-sub:
            data, _ := json.Marshal(event)
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
            flusher.Flush()
        }
    }
}
```

**SSE 事件类型：**

| 事件 type | 说明 |
|-----------|------|
| `user` | 用户消息已接收 |
| `assistant_start` | Agent 开始生成回复 |
| `reasoning_delta` | 思维链增量片段（深度思考模式） |
| `assistant_delta` | 流式 token 片段 |
| `assistant_done` | Agent 回复完成 |
| `tool_call` | Agent 发起工具调用 |
| `tool_result` | 工具调用返回结果 |
| `error` | 错误事件 |
| `session_end` | 会话结束 |

### 5.2 WebSocket

```text
WS /api/v1/sessions/:id/stream
```

```go
func (h *SessionHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "id")
    conn, _ := h.upgrader.Upgrade(w, r, nil)
    defer conn.Close()

    sub := h.manager.Subscribe(sessionID)
    defer h.manager.Unsubscribe(sessionID, sub)

    // 读 goroutine：接收客户端消息
    go func() {
        for {
            var req struct {
                Content string `json:"content"`
            }
            if err := conn.ReadJSON(&req); err != nil {
                return
            }
            h.manager.AppendMessage(sessionID, Message{
                Role:    RoleUser,
                Content: req.Content,
            })
        }
    }()

    // 写 goroutine：推送事件
    for event := range sub {
        if err := conn.WriteJSON(event); err != nil {
            return
        }
    }
}
```

### 5.3 SSE vs WebSocket 对比

| 特性 | SSE | WebSocket |
|------|-----|-----------|
| 方向 | 服务端 → 客户端（单向） | 双向 |
| 协议 | HTTP | WS |
| 自动重连 | 浏览器内置 | 需手动实现 |
| 适用场景 | 只读流式输出、事件监听 | 实时双向对话 |
| 复杂度 | 低 | 中 |

---

## 6. 消息订阅机制

```go
type Subscriber chan Event

type Event struct {
    Type    string      `json:"type"`
    Payload interface{} `json:"payload"`
}

// Subscribe 订阅 Session 事件。
func (m *Manager) Subscribe(sessionID string) Subscriber {
    m.subMu.Lock()
    defer m.subMu.Unlock()

    sub := make(Subscriber, 64)
    m.subscribers[sessionID] = append(m.subscribers[sessionID], sub)
    return sub
}

// notifySubscribers 向所有订阅者推送事件。
func (m *Manager) notifySubscribers(sessionID string, msg Message) {
    m.subMu.RLock()
    subs := m.subscribers[sessionID]
    m.subMu.RUnlock()

    event := Event{Type: "message.append", Payload: msg}
    for _, sub := range subs {
        select {
        case sub <- event:
        default: // 队列满则丢弃，避免阻塞
        }
    }
}
```

---

*最后更新: 2025-07-16*
