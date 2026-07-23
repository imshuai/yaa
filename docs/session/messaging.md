# Session 消息管理

> 上级: [Session 系统设计](README.md)
> Provider 类型: [Provider 请求与响应](../provider.md#3-请求响应类型)

---

## 1. 存储模型

Session 使用 `SessionMessage` 包裹完整 `provider.Message`：

```go
type AppendInput struct {
    Message  provider.Message
    Metadata map[string]any
}

type SessionMessage struct {
    ID        string
    TurnID    string
    Payload   provider.Message
    CreatedAt time.Time
    Metadata  map[string]any
}
```

Manager 分配 `ID` 和 `CreatedAt`，从当前 `Turn` 写入受控 `TurnID`，并深拷贝 `Message`、ToolCalls、Raw JSON 和 metadata。不得保存调用方可继续修改的引用。Agent 在调用 `Append` 前必须已将 Provider alias 精确反查为 canonical；Session 不接受或生成 alias。`TurnID` 不能放进 metadata，也不能由 `AppendInput` 指定。

## 2. Role 校验

| Role | 必须满足 | 禁止 |
|------|----------|------|
| `user` | `Content` 非空 | `ToolCalls`、`ToolCallID` |
| `assistant` final | `Content`、`ReasoningContent` 或 `Refusal` 至少一个非空 | `ToolCallID` |
| `assistant` tool call | `ToolCalls` 非空，每个 ID 唯一，arguments 是合法 JSON | `ToolCallID` |
| `tool` | `ToolCallID` 非空并对应前置 assistant call | `ToolCalls` |
| `system` | — | 始终拒绝持久化 |

非 Tool message 的 `Name` 和 `Refusal` 按 Provider 原值保留；assistant ToolCalls 的 function name 与 tool message 的非空 `Name` 必须是 canonical，且非空 tool `Name` 必须等于其 `ToolCallID` 所引用 call 的 function name。Tool 允许空 `Content`，以表达成功但无输出；失败也必须生成对应的 Tool result，其 Content 使用 Tool 层定义的结构化错误文本。

任何未知 role、非法字段组合、重复 ToolCall ID 或悬空 Tool result 返回 `ErrInvalidMessage` 或 `ErrInvalidMessageSequence`。

## 3. 原子追加批次

`Turn.AppendUser` 只接受当前 turn 的首条非空 user，并在同一个候选 snapshot 中提交消息和已使用 Turn ID。随后 `Turn.Append` 只接受以下批次之一：

1. 单条无 ToolCalls 的 final `assistant`；
2. 一条含 ToolCalls 的 `assistant`，紧跟与每个 call 一一对应的全部 `tool` 结果。

第三种批次是不可拆分 Tool unit。Tool 可以并行执行，但 Agent 必须等待每个 call 都产生结果，再按 assistant 中 ToolCalls 的顺序组装批次。一个结果不能匹配多个 call，批次也不能混入下一轮 assistant 消息。

```text
assistant(tool_calls=[call_a, call_b])
tool(tool_call_id=call_a)
tool(tool_call_id=call_b)
```

追加前对整个候选状态执行：

1. 状态和消息序列校验；候选历史非空时首条必须是 `user`。因此 Created 首批，以及 Clear 后对 Active 恢复追加的首批，都必须由新 `RunTurn` 的 `AppendUser` 开始。
2. 为每条消息计算 JSON 序列化字节数，均不得超过 `policy.max_message_bytes`。
3. 检查 `len(existing)+len(batch) <= policy.max_messages`。
4. 分配 Message ID 和统一的提交时间。
5. 按 [同步提交协议](persistence.md#3-同步提交协议) 提交完整 snapshot。

任一检查或持久化失败都拒绝整个批次，不分配可见 ID、不改变状态和计数。首次成功追加将 `created -> active` 与消息写入同一 snapshot。

## 4. 历史查询

历史按 `CreatedAt`、再按 Message ID 升序返回。默认 `page=1`、`page_size=50`，最大 `page_size=200`；无结果返回空 `items`，不是错误。

支持：

- `role=user|assistant|tool` 过滤；`system` 不可能出现在 Session；
- `after=<message-id>` 增量读取；指定 ID 不属于该 Session 时返回 `ErrMessageNotFound`；
- 标准 `page` / `page_size` 分页；`after` 与 `page>1` 不能同时使用。

查询返回深拷贝。API DTO 显式返回 `turn_id`，并将 canonical `Payload` 展开为 `role`、`content`、`reasoning_content`、`name`、`tool_calls`、`tool_call_id` 和 `refusal`；不得制造 Session 未保存的 `tokens`、`model` 或 alias 字段。

## 5. 删除与清空

消息不支持编辑。为隐私删除提供两个显式操作：

- `DeleteMessage(sessionID, messageID)` 删除目标消息；若目标属于 Tool unit，则删除该 assistant Tool call 和全部对应 Tool result。
- `ClearMessages(sessionID)` 删除全部消息，但保留 Session、state、policy 和 metadata。

删除后必须重新校验完整候选序列：不得存在悬空 Tool result，并且历史必须为空或首条消息为 `user`。例如删除首条 user 后若留下 assistant，整个删除返回 `ErrInvalidMessageSequence`，snapshot 和事件均不变。操作只允许 `created`、`active` 或 `paused`，Closed 返回 `ErrSessionClosed`。成功时更新 `UpdatedAt`，不更新 `LastActivityAt`，并在同步提交后发布一次 `session.message.deleted`：

```json
{
  "session_id": "ses_01J...",
  "message_ids": ["msg_01J..."],
  "count": 1,
  "reason": "manual",
  "occurred_at": "2026-07-22T01:02:00Z"
}
```

Clear 使用 `reason=clear`，以一个事件携带全部删除 ID；没有消息时返回 count 0 且不写 snapshot、不发事件。DeleteMessage/Clear 都不删除已使用 Turn ID tombstone，避免删除历史后重放同一请求。

## 6. 流式输出边界

Provider 的 delta、Tool 执行进度和 WebSocket/SSE 传输属于 [对话 API](../remote-api/conversation.md)，不是 Session 历史。Session 只在完整 user、assistant 或 Tool unit 提交后发布状态事件，绝不把半个流式响应写入 snapshot。

客户端中断时：

- 已提交的 user 消息保留；
- 尚未形成完整 assistant/Tool unit 的 delta 不持久化；
- 已提交的 Tool unit 保留，后续可从该合法边界继续请求 Provider。

---

*最后更新: 2026-07-23*
