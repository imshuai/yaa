# Session 生命周期管理

> 文档路径: `docs/session/lifecycle.md`
> 上级: `docs/session/README.md` §2

---

## 1. 概述

Session 生命周期是 Yaa! 会话管理的核心状态机。每个 Session 从创建到关闭，经历明确的状态转换，每个状态有严格的处理逻辑和允许的操作。

---

## 2. 状态定义

```go
type SessionState int

const (
    SessionStateCreated SessionState = iota // 刚创建，尚未激活
    SessionStateActive                       // 活跃，可接收消息
    SessionStatePaused                       // 暂停，拒绝新消息
    SessionStateClosed                       // 已关闭，终态
)
```

| 状态 | 值 | 说明 | 可接收消息 | 可恢复 |
|------|-----|------|-----------|--------|
| `Created` | 0 | 刚创建，尚未接收首条消息 | ❌ | — |
| `Active` | 1 | 正常交互中 | ✅ | — |
| `Paused` | 2 | 临时暂停 | ❌ | ✅ |
| `Closed` | 3 | 已关闭，终态 | ❌ | ❌ |

---

## 3. 状态转换图

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
                           │ Closed    │  (终态)
                           └──────────┘
```

---

## 4. 状态转换规则

| 从 | 到 | 触发方法 | 前置条件 | 副作用 |
|----|----|---------|---------|--------|
| (无) | Created | `Create()` | AgentID 有效 | 分配 ULID、写入存储 |
| Created | Active | 首条 `AppendMessage()` | State == Created | 更新 UpdatedAt |
| Created | Closed | `Close()` | — | 标记终态 |
| Active | Paused | `Pause()` | State == Active | 拒绝新消息 |
| Active | Closed | `Close()` | State == Active | 归档消息 |
| Paused | Active | `Resume()` | State == Paused | 恢复消息接收 |
| Paused | Closed | `Close()` | State == Paused | 归档消息 |
| Closed | * | — | **不允许** | — |

---

## 5. 各状态处理逻辑

### 5.1 Created

```go
func (m *Manager) Create(agentID string, opts ...SessionOption) (*Session, error) {
    // 1. 验证 Agent 存在
    if _, err := m.agentMgr.Get(agentID); err != nil {
        return nil, fmt.Errorf("agent not found: %w", err)
    }

    // 2. 构造 Session
    sess := &Session{
        ID:        "sess_" + ulid.Make().String(),
        AgentID:   agentID,
        Messages:  []Message{},
        State:     SessionStateCreated,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
        Metadata:  make(map[string]any),
    }

    // 3. 应用选项
    for _, opt := range opts {
        opt(sess)
    }

    // 4. 持久化
    if err := m.store.Save(sess); err != nil {
        return nil, fmt.Errorf("persist session: %w", err)
    }

    // 5. 注册到内存索引
    m.mu.Lock()
    m.sessions[sess.ID] = sess
    m.agentIdx[agentID] = append(m.agentIdx[agentID], sess.ID)
    m.mu.Unlock()

    m.logger.Info("session created", "session_id", sess.ID, "agent_id", agentID)
    return sess, nil
}
```

### 5.2 Active

Active 是主要工作状态。消息处理流程：

```go
func (m *Manager) AppendMessage(sessionID string, msg Message) error {
    sess, err := m.getLocked(sessionID)
    if err != nil {
        return err
    }

    // 状态检查
    if sess.State == SessionStateCreated {
        sess.State = SessionStateActive // 首条消息触发激活
    }
    if sess.State != SessionStateActive {
        return ErrSessionNotActive
    }

    msg.ID = "msg_" + ulid.Make().String()
    msg.CreatedAt = time.Now()
    sess.Messages = append(sess.Messages, msg)
    sess.UpdatedAt = time.Now()

    // 异步持久化
    return m.store.Save(sess)
}
```

### 5.3 Paused

```go
func (m *Manager) Pause(sessionID string) error {
    sess, err := m.getLocked(sessionID)
    if err != nil {
        return err
    }
    if sess.State != SessionStateActive {
        return ErrInvalidStateTransition
    }

    sess.State = SessionStatePaused
    sess.UpdatedAt = time.Now()
    m.logger.Info("session paused", "session_id", sessionID)
    return m.store.Save(sess)
}
```

Paused 状态下：
- `AppendMessage()` 返回 `ErrSessionPaused`
- `Get()` / `GetMessages()` 正常工作（只读）
- `Resume()` 可恢复到 Active

### 5.4 Closed

```go
func (m *Manager) Close(sessionID string) error {
    sess, err := m.getLocked(sessionID)
    if err != nil {
        return err
    }
    if sess.State == SessionStateClosed {
        return nil // 幂等
    }

    sess.State = SessionStateClosed
    sess.UpdatedAt = time.Now()

    // 触发归档回调
    if m.onClose != nil {
        m.onClose(sess)
    }

    m.logger.Info("session closed", "session_id", sessionID,
        "message_count", len(sess.Messages))
    return m.store.Save(sess)
}
```

Closed 状态下：
- 所有写操作返回 `ErrSessionClosed`
- `Get()` / `GetMessages()` 仍可读取（归档数据）
- `Delete()` 可彻底移除

---

## 6. 状态守卫

所有公开方法通过状态守卫确保操作合法性：

```go
// ensureState 检查 Session 是否处于允许的状态。
func (s *Session) ensureState(allowed ...SessionState) error {
    for _, st := range allowed {
        if s.State == st {
            return nil
        }
    }
    return fmt.Errorf("session %s is in %s state, expected one of %v",
        s.ID, s.State, allowed)
}
```

| 方法 | 允许的状态 |
|------|----------|
| `AppendMessage` | Created, Active |
| `Pause` | Active |
| `Resume` | Paused |
| `Close` | Created, Active, Paused |
| `Get` | 任意 |
| `GetMessages` | 任意 |
| `Delete` | 任意 |
| `UpdateMetadata` | Created, Active, Paused |

---

## 7. 超时与自动关闭

```go
// autoCloseTicker 定期检查并自动关闭超时 Session。
func (m *Manager) autoCloseTicker(ctx context.Context) {
    ticker := time.NewTicker(m.config.AutoCloseInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.closeIdleSessions(m.config.IdleTimeout)
        }
    }
}
```

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `auto_close_interval` | 5m | 检查间隔 |
| `idle_timeout` | 24h | 超时阈值 |
| `max_lifetime` | 72h | 最大存活时间 |

---

*最后更新: 2025-07-16*
