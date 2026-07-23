# Session 可观测性

> 上级: [Session 系统设计](README.md)
> 对话流事件: [对话 API](../remote-api/conversation.md)

---

## 1. 结构化日志

Session 日志使用 `slog` 兼容 API，固定 `component=session`。不记录消息内容、ReasoningContent、Tool arguments/result、metadata 或凭据。

| 事件 | 级别 | 字段 |
|------|------|------|
| `session.created` | INFO | `session_id`, `agent_id`, `persist` |
| `session.state.changed` | INFO | `session_id`, `from`, `to`, `reason` |
| `session.message.appended` | DEBUG | `session_id`, `message_count`, `roles`（不记录 Turn ID） |
| `session.message.deleted` | INFO | `session_id`, `count`, `reason` |
| `session.deleted` | INFO | `session_id`, `agent_id` |
| `session.restore.completed` | INFO | `total`, `state_changes`, `duration` |
| `session.operation.failed` | WARN/ERROR | `operation`, `session_id?`, `error_class` |

`error_class` 使用稳定常量名，不把底层错误全文作为 label；cause 仅作为结构化日志字段。

## 2. 指标

所有指标使用 `yaa_` 前缀。禁止使用 `session_id`、Turn ID、message ID、ToolCall ID、错误文本或用户 metadata 作为 label。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `yaa_session_current` | Gauge | `state` | 当前各状态 Session 数 |
| `yaa_session_operations_total` | Counter | `operation`, `result` | Create/Pause/Resume/Close/Delete 等结果 |
| `yaa_session_messages_total` | Counter | `role` | 已提交消息数 |
| `yaa_session_message_bytes` | Histogram | `role` | 单条 Provider message JSON 字节数 |
| `yaa_session_turn_wait_seconds` | Histogram | — | FIFO gate 等待时间 |
| `yaa_session_turn_duration_seconds` | Histogram | `result` | 完整 turn 执行时间 |
| `yaa_session_persistence_errors_total` | Counter | `operation` | Snapshot 写入/删除失败 |
| `yaa_session_restore_total` | Counter | `result` | Restore 成功或失败次数 |
| `yaa_session_cleanup_transitions_total` | Counter | `to`, `reason` | TTL/max lifetime 转换 |
| `yaa_session_event_publish_errors_total` | Counter | `event` | Event Bus 发布失败 |

Gauge 从 Manager 当前 snapshot 计算或在提交点增减；不能同时使用两种方式。恢复完成时一次性初始化，避免重复累加。

## 3. 事件契约

事件 envelope：

```json
{
  "event_id": "evt_01J...",
  "type": "session.state.changed",
  "session_id": "ses_01J...",
  "agent_id": "default",
  "occurred_at": "2026-07-22T01:02:00Z",
  "data": {}
}
```

唯一事件类型如下：

| type | `data` 必需字段 | 触发 |
|------|-----------------|------|
| `session.created` | `state`, `persist` | Create 提交后 |
| `session.state.changed` | `from`, `to`, `reason` | Pause/Resume/Close/TTL/max lifetime 提交后 |
| `session.message.appended` | `turn_id`, `message_ids`, `roles`, `count` | AppendUser/Append 批次提交后 |
| `session.message.deleted` | `message_ids`, `count`, `reason` | DeleteMessage/Clear 提交后 |
| `session.deleted` | `previous_state` | 物理 Delete 提交后 |
| `session.error` | `operation`, `error_class`, `retryable` | 操作失败后 |

`reason` 取值：

- 状态：`manual | ttl | max_lifetime | restore`；
- 消息删除：`manual | clear`。

所有 mutation 事件只在 Storage 与内存提交后发布一次。Close 已 Closed、Clear 空历史等 no-op 不发事件。Restore 不重放 mutation 事件；restore 导致的状态修正只计日志/指标，避免启动时向客户端伪装实时转换。`session.error` 不含消息内容或底层 Storage 错误文本。

## 4. SSE / WebSocket 边界

Remote API 可以把上述 Session 事件转发到 `/api/v1/sessions/:id/events`。Provider delta 使用 `assistant_delta`、`reasoning_delta` 等 conversation frame，不能伪装成 `session.message.appended`；只有完整消息写入 snapshot 后才发送 Session mutation 事件。

Remote API 为每个订阅者使用固定 256 帧有界队列；队列满时断连，不逐帧丢弃。Session 提交不得等待单个 SSE/WS 客户端，也不得因推送失败回滚；完整策略见对话 API。

## 5. Trace 与健康

推荐 span：

- `session.create`
- `session.turn.wait`
- `session.turn`
- `session.persist`
- `session.restore`
- `session.cleanup`

Trace 属性可以包含 `session.id` 和 `agent.id`，但不能包含内容。

Session 健康只表示 Manager 是否完成 Restore、是否接受任务；Created、Paused 或 Closed 的数量不影响健康。Storage 的连接健康由 Storage 子系统报告，Restore 失败则 Session `ready=false`，Remote API 返回 `50301`。

---

*最后更新: 2026-07-22*
