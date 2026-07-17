# Session 可观测性

> 文档路径: `docs/session/observability.md`
> 上级: `docs/session/README.md` §8

---

本文档定义 Session 系统的日志事件、运行时指标和 Remote API 事件（SSE），为调试、监控和客户端实时同步提供基础。

---

## 8.1 日志事件

Session Manager 使用 `slog.Logger` 输出结构化日志，所有事件携带 `component=session` 标签。

| 事件名 | 级别 | 关键字段 | 说明 |
|--------|------|---------|------|
| `session.created` | INFO | `session_id`, `agent_id` | Session 创建成功 |
| `session.message.appended` | INFO | `session_id`, `role`, `msg_id` | 消息追加到历史 |
| `session.state.changed` | INFO | `session_id`, `from`, `to` | 状态转换 |
| `session.paused` | INFO | `session_id` | 显式暂停 |
| `session.resumed` | INFO | `session_id` | 从暂停恢复 |
| `session.closed` | INFO | `session_id`, `msg_count` | Session 关闭，记录消息总数 |
| `session.restored` | INFO | `session_id`, `agent_id` | 从存储恢复 |
| `session.deleted` | WARN | `session_id` | 从存储彻底删除 |
| `session.message.rejected` | WARN | `session_id`, `state`, `reason` | 当前状态不允许追加消息 |
| `session.persist.failed` | ERROR | `session_id`, `error` | 持久化写入失败 |
| `session.restore.failed` | ERROR | `agent_id`, `error` | 批量恢复中单条失败 |
| `session.not_found` | WARN | `session_id` | 查询不存在的 Session |

**日志示例：**

```go
// 状态转换日志
logger.Info("session.state.changed",
    slog.String("component", "session"),
    slog.String("session_id", sessID),
    slog.String("from", "active"),
    slog.String("to", "paused"),
)

// 消息被拒绝（Paused 状态收到新消息）
logger.Warn("session.message.rejected",
    slog.String("component", "session"),
    slog.String("session_id", sessID),
    slog.String("state", sess.State.String()),
    slog.String("reason", "session is paused, resume before sending messages"),
)
```

---

## 8.2 运行时指标

Session Manager 通过 `expvar` 或 Prometheus 兼容接口暴露以下指标。

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `yaa_session_total` | Gauge | `agent_id`, `state` | 各状态 Session 数量 |
| `yaa_session_messages_total` | Counter | `agent_id`, `role` | 累计追加的消息数 |
| `yaa_session_message_bytes` | Histogram | `agent_id` | 单条消息字节数分布 |
| `yaa_session_duration_seconds` | Histogram | `state` | Session 从创建到关闭的时长 |
| `yaa_session_persist_errors_total` | Counter | — | 持久化失败次数 |
| `yaa_session_restore_errors_total` | Counter | — | 批量恢复失败次数 |
| `yaa_session_concurrent_active` | Gauge | `agent_id` | 当前 Active 状态的 Session 数 |

**指标采集示例：**

```go
type SessionMetrics struct {
    Total       *expvar.Map   // key: "agent_id:state" → count
    Messages    *expvar.Map   // key: "agent_id:role" → count
    ActiveGauge *expvar.Int   // 当前 Active Session 数
}

// AppendMessage 时更新指标
func (m *Manager) AppendMessage(sessionID string, msg Message) error {
    sess, err := m.Get(sessionID)
    if err != nil {
        return err
    }
    if sess.State != SessionStateActive {
        m.metrics.Messages.Add(fmt.Sprintf("%s:%s", sess.AgentID, "rejected"), 1)
        return ErrSessionNotActive
    }
    // ... 追加消息逻辑 ...
    m.metrics.Messages.Add(fmt.Sprintf("%s:%s", sess.AgentID, string(msg.Role)), 1)
    return nil
}
```

---

## 8.3 SSE 事件

Remote API 通过 Server-Sent Events 向客户端实时推送 Session 状态变更和消息流。

| SSE 事件 | `event` 字段 | `data` 载荷 | 触发时机 |
|----------|-------------|------------|---------|
| 会话创建 | `session.created` | `{session_id, agent_id, created_at}` | `POST /sessions` 成功 |
| 状态变更 | `session.state` | `{session_id, from, to, timestamp}` | Pause / Resume / Close |
| 消息追加 | `session.message` | `{session_id, msg_id, role, content}` | `AppendMessage` 成功 |
| 流式 Token | `session.token` | `{session_id, delta, finish_reason}` | Provider 流式输出 |
| 错误 | `session.error` | `{session_id, code, message}` | 处理过程中发生错误 |

**SSE 推送示例：**

```go
// Server-Sent Events 写入
func writeSSE(w http.ResponseWriter, event string, data any) error {
    payload, _ := json.Marshal(data)
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
    if f, ok := w.(http.Flusher); ok {
        f.Flush()
    }
    return nil
}

// 状态变更时推送
func (m *Manager) Pause(sessionID string) error {
    sess, err := m.Get(sessionID)
    if err != nil {
        return err
    }
    old := sess.State
    sess.State = SessionStatePaused
    m.bus.Publish("session.state", map[string]any{
        "session_id": sessionID,
        "from":       old.String(),
        "to":         "paused",
        "timestamp":  time.Now().UTC(),
    })
    return nil
}
```

**客户端消费示例：**

```go
// 客户端订阅 SSE 流
resp, _ := http.Get("http://localhost:8080/api/sessions/sess_xxx/events")
scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "event: ") {
        event = strings.TrimPrefix(line, "event: ")
    }
    if strings.HasPrefix(line, "data: ") {
        payload := strings.TrimPrefix(line, "data: ")
        fmt.Printf("[%s] %s\n", event, payload)
    }
}
```

---

## 8.4 健康检查

```go
// HealthCheck 返回 Session 系统的运行状态摘要。
func (m *Manager) HealthCheck() SessionHealthReport {
    m.mu.RLock()
    defer m.mu.RUnlock()
    report := SessionHealthReport{
        TotalSessions: len(m.sessions),
        ByState:       make(map[string]int),
    }
    for _, s := range m.sessions {
        report.ByState[s.State.String()]++
    }
    report.Healthy = report.ByState["active"]+report.ByState["paused"] == report.TotalSessions
    return report
}
```

| 字段 | 说明 |
|------|------|
| `TotalSessions` | 内存中 Session 总数 |
| `ByState` | 各状态数量，如 `{"active": 3, "paused": 1, "closed": 0}` |
| `Healthy` | 无异常状态时为 `true` |
