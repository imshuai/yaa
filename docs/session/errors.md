# Session 错误契约

> 上级: [Session 系统设计](README.md)
> Remote API 错误码: [统一错误码](../remote-api/INDEX.md#7-错误码)

---

## 1. 稳定错误集合

```go
var (
    ErrSessionNotFound         = errors.New("session: not found")
    ErrMessageNotFound         = errors.New("session: message not found")
    ErrAgentNotFound           = errors.New("session: agent not found")
    ErrSessionClosed           = errors.New("session: closed")
    ErrSessionPaused           = errors.New("session: paused")
    ErrSessionExpired          = errors.New("session: max lifetime exceeded")
    ErrInvalidStateTransition  = errors.New("session: invalid state transition")
    ErrInvalidMessage          = errors.New("session: invalid message")
    ErrInvalidMessageSequence  = errors.New("session: invalid message sequence")
    ErrMessageTooLarge         = errors.New("session: message too large")
    ErrMessageLimitExceeded    = errors.New("session: message limit exceeded")
    ErrSessionSnapshotTooLarge = errors.New("session: snapshot too large")
    ErrCapacityExceeded        = errors.New("session: capacity exceeded")
    ErrSessionConfigInvalid    = errors.New("session: invalid config")
    ErrPersistenceFailed       = errors.New("session: persistence failed")
    ErrRestoreFailed           = errors.New("session: restore failed")
    ErrSchemaUnsupported       = errors.New("session: unsupported schema")
    ErrManagerClosed           = errors.New("session: manager closed")
    ErrInvalidTurnID           = errors.New("session: invalid turn id")
    ErrTurnIDConflict          = errors.New("session: turn id already used")
    ErrTurnNotActive           = errors.New("session: turn not active")
)
```

空历史返回空 slice，不是错误。并发请求等待 FIFO gate 或由 `context.Context` 取消，不定义 busy/lock timeout 错误。

## 2. 带上下文包装

```go
type OpError struct {
    Op        string
    SessionID string
    Err       error
}

func (e *OpError) Error() string {
    if e.SessionID == "" {
        return fmt.Sprintf("session %s: %v", e.Op, e.Err)
    }
    return fmt.Sprintf("session %s %s: %v", e.SessionID, e.Op, e.Err)
}

func (e *OpError) Unwrap() error { return e.Err }
```

底层错误使用 `%w` 或 `OpError.Unwrap` 保留 `errors.Is`。日志和 API 通过稳定错误常量分类，不解析错误字符串。Storage 原始错误可作为 cause 写日志，但不能原样返回客户端。

`CancelTurn` 使用保存的 `context.CancelCauseFunc`，因此 callback 观察到 `turnCtx.Err()==context.Canceled`；`RunTurn` 返回时改用 `context.Cause(turnCtx)`，保留 `ErrAgentStopped`、Manager shutdown、caller cancel 或 deadline。业务层先按 cause 分类，再决定日志和 Remote 映射；客户端断开时通常不再写响应。

## 3. 原子失败语义

| 错误 | 状态影响 |
|------|----------|
| 校验、状态或容量错误 | 无变更 |
| `ErrPersistenceFailed` | 内存和事件均无变更，旧 snapshot 继续有效 |
| `ErrRestoreFailed` | Manager 不发布部分索引，Runtime 保持 Not Ready |
| Event Bus 发布失败 | snapshot 已提交，不回滚；增加事件失败指标 |
| Provider / Tool 错误 | 由 Agent 层处理；只保留此前已提交的合法 Session 边界 |

不存在“持久化失败后继续纯内存运行”的降级。`persist=false` 是创建时明确解析的 policy，不是错误恢复路径。

## 4. Remote API 映射

| Session 错误 | HTTP / code | 说明 |
|--------------|-------------|------|
| `ErrSessionNotFound`, `ErrMessageNotFound`, `ErrAgentNotFound` | 404 / `40401` | 资源不存在 |
| `ErrSessionClosed`, `ErrSessionPaused`, `ErrSessionExpired`, `ErrInvalidStateTransition` | 409 / `40901` | 当前资源状态不允许操作 |
| `ErrInvalidMessage`, `ErrSessionConfigInvalid`, `ErrInvalidTurnID`, `ErrTurnIDConflict` | 400 / `40001` | 请求字段无效或 Turn ID 重复 |
| `ErrTurnNotActive` | 404 / `40401` | 取消目标不存在或已终态 |
| `ErrInvalidMessageSequence`, `ErrMessageTooLarge`, `ErrMessageLimitExceeded` | 422 / `42201` | 请求可解析但无法满足 Session 语义 |
| `ErrSessionSnapshotTooLarge` | 422 / `42201` | 完整持久 snapshot 超过 16 MiB |
| `ErrCapacityExceeded` | 429 / `42901` | Agent 的非 Closed Session 已达上限 |
| `ErrPersistenceFailed`, `ErrRestoreFailed`, `ErrManagerClosed` | 503 / `50301` | Runtime 暂不可用 |
| `ErrSchemaUnsupported` | 500 / `50001` | 持久数据版本不受支持，Runtime 不 Ready |

Snapshot 编码结果超过根 Storage 的 16 MiB 时，同时包装 `ErrPersistenceFailed` 与 `ErrSessionSnapshotTooLarge`：

```go
return fmt.Errorf(
    "encode session snapshot: %w: %w",
    ErrPersistenceFailed,
    ErrSessionSnapshotTooLarge,
)
```

映射时必须先检查更具体的 `ErrSessionSnapshotTooLarge`，再检查 `ErrPersistenceFailed`。底层 `storage.ErrValueTooLarge` 只写入内部 cause，不直接暴露给客户端。

Handler 必须先使用 `errors.Is` 选择最具体的映射，再由统一 middleware 生成 envelope。不要在 Session 包中依赖 HTTP 类型。

## 5. 恢复和后台任务

- Restore 发现任一坏 snapshot 时返回聚合的 `ErrRestoreFailed`，日志列出失败 key 和 cause；不记录消息内容。
- Cleanup 单个 Session 持久化失败时发 `session.error` 并继续其他 Session；失败 Session 保持旧状态，下个 tick 重试。
- Close 的幂等成功不记错误、不重复事件。
- Event subscriber 缓慢或断开不得转化为 Session 操作失败。

---

*最后更新: 2026-07-22*
