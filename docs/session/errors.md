# Session 错误处理

> 文档路径: `docs/session/errors.md`
> 上级: `docs/session/README.md` §7
> 依赖: `docs/session/README.md` §2 (核心类型), `docs/architecture.md` §5 (并发模型)

---

## 7. 错误分类

### 7.1 错误分类表

Session 系统的错误按来源分为五大类：**生命周期错误**、**消息错误**、**并发错误**、**持久化错误**、**配置错误**。

| 错误类型 | 常量 | 说明 | 处理方式 |
|---------|------|------|---------|
| **生命周期** | | | |
| Session 不存在 | `ErrSessionNotFound` | 指定 ID 的 Session 未找到 | 返回 404 给 Remote API Client |
| Session 已关闭 | `ErrSessionClosed` | 对已 Closed 的 Session 操作 | 返回 409 Conflict |
| Session 已暂停 | `ErrSessionPaused` | 对已 Paused 的 Session 发送消息 | 返回 409，提示 Resume |
| Session 非活跃 | `ErrSessionNotActive` | Session 未处于 Active 状态 | 返回 409，提示激活或恢复 |
| 非法状态转换 | `ErrInvalidStateTransition` | 如 Closed → Active | 返回 409，记录 Warn 日志 |
| Agent 不存在 | `ErrAgentNotFound` | 创建 Session 时 AgentID 无效 | 返回 400 |
| **消息** | | | |
| 消息为空 | `ErrEmptyMessage` | Content 和 ToolCalls 均为空 | 返回 400 |
| 消息历史为空 | `ErrNoMessages` | 获取消息历史时 Session 无消息 | 返回空切片，非错误 |
| 消息超限 | `ErrMessageLimitExceeded` | 消息数量超过配置上限 | 拒绝追加，返回 413 |
| **并发** | | | |
| Session 已锁定 | `ErrSessionBusy` | 同一 Session 正在处理消息 | 返回 429，建议重试 |
| 获取锁超时 | `ErrLockTimeout` | 等待 Session 锁超过阈值 | 返回 503，建议退避重试 |
| **持久化** | | | |
| 存储不可用 | `ErrStorageUnavailable` | 后端存储连接失败 | 降级为纯内存模式，记录 Error |
| 序列化失败 | `ErrSerializeFailed` | Session 数据序列化错误 | 跳过该 Session，记录 Error |
| 恢复失败 | `ErrRestoreFailed` | 启动时恢复 Session 失败 | 跳过损坏记录，继续恢复其余 |
| **配置** | | | |
| 配置无效 | `ErrInvalidConfig` | Session 级配置校验失败 | 返回 400，拒绝创建 |
| 容量超限 | `ErrCapacityExceeded` | Agent 下 Session 数超过上限 | 返回 429，提示关闭旧 Session |

### 7.2 错误定义

```go
package session

import "errors"

// 生命周期错误
var (
    ErrSessionNotFound       = errors.New("session: not found")
    ErrSessionClosed         = errors.New("session: already closed")
    ErrSessionPaused         = errors.New("session: paused, resume first")
    ErrSessionNotActive      = errors.New("session: not active")
    ErrInvalidStateTransition = errors.New("session: invalid state transition")
    ErrAgentNotFound         = errors.New("session: agent not found")
)

// 消息错误
var (
    ErrEmptyMessage          = errors.New("session: message content is empty")
    ErrMessageLimitExceeded  = errors.New("session: message limit exceeded")
)

// 并发错误
var (
    ErrSessionBusy  = errors.New("session: busy, another message is being processed")
    ErrLockTimeout  = errors.New("session: lock acquisition timeout")
)

// 持久化错误
var (
    ErrStorageUnavailable = errors.New("session: storage unavailable")
    ErrSerializeFailed    = errors.New("session: serialize failed")
    ErrRestoreFailed      = errors.New("session: restore failed")
)

// 配置错误
var (
    ErrInvalidConfig    = errors.New("session: invalid config")
    ErrCapacityExceeded = errors.New("session: capacity exceeded")
)
```

---

## 8. 错误传递策略

### 8.1 传递路径

```text
Session 操作错误 → Session Manager → Agent → Remote API → Client
                         │
                         ├─ 生命周期错误 → HTTP 4xx，Client 调整行为
                         ├─ 并发错误    → HTTP 429/503，Client 退避重试
                         ├─ 持久化错误  → 降级运行 + 日志告警
                         └─ 配置错误    → HTTP 400，拒绝请求
```

### 8.2 状态守卫模式

Session Manager 在每个操作入口进行状态校验，将错误尽早拦截：

```go
func (m *Manager) AppendMessage(sessionID string, msg Message) error {
    // 1. 查找 Session
    sess, err := m.get(sessionID)
    if err != nil {
        return fmt.Errorf("append message: %w", ErrSessionNotFound)
    }

    // 2. 状态守卫
    switch sess.State {
    case SessionStateClosed:
        return fmt.Errorf("append message: %w", ErrSessionClosed)
    case SessionStatePaused:
        return fmt.Errorf("append message: %w", ErrSessionPaused)
    case SessionStateCreated, SessionStateActive:
        // 继续（Created 状态下首条消息触发激活）
    default:
        return fmt.Errorf("append message: %w (state=%s)",
            ErrInvalidStateTransition, sess.State)
    }

    // 3. 消息校验
    if msg.Content == "" && len(msg.ToolCalls) == 0 {
        return ErrEmptyMessage
    }

    // 4. 容量检查
    if sess.MaxMessages > 0 && len(sess.Messages) >= sess.MaxMessages {
        return fmt.Errorf("append message: %w (limit=%d)",
            ErrMessageLimitExceeded, sess.MaxMessages)
    }

    // 5. 追加 + 持久化
    sess.Messages = append(sess.Messages, msg)
    sess.UpdatedAt = time.Now()

    if err := m.store.Save(sess); err != nil {
        m.logger.Error("persist session failed, running in-memory only",
            "session", sessionID, "error", err)
        // 降级：消息已在内存中，不阻断流程
    }

    return nil
}
```

### 8.3 并发错误处理

```go
func (m *Manager) SendMessage(sessionID string, msg Message) error {
    sess, err := m.get(sessionID)
    if err != nil {
        return fmt.Errorf("send message: %w", ErrSessionNotFound)
    }

    // 尝试获取 Session 级处理锁
    lock := m.getLock(sessionID)
    if !lock.TryLock() {
        return fmt.Errorf("send message: %w", ErrSessionBusy)
    }
    defer lock.Unlock()

    // 执行对话处理（串行）
    return m.process(sess, msg)
}
```

### 8.4 恢复容错

Runtime 启动时批量恢复 Session，单条失败不阻断整体：

```go
func (m *Manager) RestoreAll() error {
    sessions, err := m.store.LoadAll()
    if err != nil {
        return fmt.Errorf("restore all: %w", err)
    }

    var errs []error
    for _, sess := range sessions {
        if err := m.restoreOne(sess); err != nil {
            m.logger.Warn("skip corrupt session during restore",
                "session", sess.ID, "error", err)
            errs = append(errs, fmt.Errorf("session %s: %w", sess.ID, err))
            continue
        }
        m.sessions[sess.ID] = sess
    }

    if len(errs) > 0 {
        m.logger.Warn("restore completed with errors",
            "total", len(sessions), "failed", len(errs))
    }
    // 不返回错误，允许 Runtime 在部分恢复后启动
    return nil
}
```

### 8.5 Remote API 映射

| Session 错误 | HTTP 状态码 | 说明 |
|-------------|------------|------|
| `ErrSessionNotFound` | 404 | 资源不存在 |
| `ErrSessionClosed` | 409 | 状态冲突 |
| `ErrSessionPaused` | 409 | 状态冲突，提示 Resume |
| `ErrSessionNotActive` | 409 | 状态冲突，提示激活 |
| `ErrInvalidStateTransition` | 409 | 非法转换 |
| `ErrSessionBusy` | 429 | 限流，建议重试 |
| `ErrLockTimeout` | 503 | 服务暂不可用 |
| `ErrEmptyMessage` | 400 | 请求参数无效 |
| `ErrMessageLimitExceeded` | 413 | 请求体过大 |
| `ErrCapacityExceeded` | 429 | 容量限制 |
| `ErrInvalidConfig` | 400 | 配置无效 |
| `ErrAgentNotFound` | 400 | 关联资源不存在 |
