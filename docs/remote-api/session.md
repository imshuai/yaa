# Session API

> [返回索引](INDEX.md)
> 领域契约: [Session 系统设计](../session/README.md)

所有 REST 响应使用 [统一 envelope](INDEX.md#4-rest-envelope)。本文件仅展示 `data`。状态字段统一命名为 `state`，取值为 `created|active|paused|closed`。

## Session DTO

```json
{
  "id": "ses_01J...",
  "agent_id": "default",
  "state": "active",
  "message_count": 2,
  "metadata": {"title": "Chat about Go", "source": "telegram"},
  "policy": {
    "max_messages": 1000,
    "max_message_bytes": 10485760,
    "ttl": "24h0m0s",
    "max_lifetime": "720h0m0s",
    "persist": true
  },
  "created_at": "2026-07-22T01:00:00Z",
  "updated_at": "2026-07-22T01:01:00Z",
  "last_activity_at": "2026-07-22T01:01:00Z"
}
```

`policy` 是创建时解析并冻结的 effective policy。Session 没有 system prompt override 或持久化 Context token/compression 状态。

## POST /api/v1/agents/:id/sessions

为指定 Agent 创建 Session。

```json
{
  "metadata": {"title": "Chat about Go", "source": "telegram"},
  "policy": {
    "max_messages": 200,
    "ttl": "4h",
    "persist": false
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| `metadata` | object | 否 | 透明保存的 Session metadata |
| `policy` | object | 否 | Create 级 `SessionOverride` |
| `policy.max_messages` | int | 否 | `> 0` |
| `policy.max_message_bytes` | int | 否 | `> 0` |
| `policy.ttl` | duration string | 否 | 0 禁用，否则 `>=1m` |
| `policy.max_lifetime` | duration string | 否 | 0 禁用，否则 `>=1m` |
| `policy.persist` | bool | 否 | false 表示只存在于当前进程 |

未知字段返回 `40001`。成功返回 Session DTO，HTTP 201。

## GET /api/v1/agents/:id/sessions

分页列出 Agent 的 Session。

| Query | 默认值 | 规则 |
|-------|--------|------|
| `page` | 1 | `>=1` |
| `page_size` | 20 | 1..100 |
| `state` | 全部 | `created|active|paused|closed` |

返回标准分页 `data={items,total,page,page_size}`，items 为 Session DTO，默认按 `created_at` 降序、ID 降序稳定排序。

## GET /api/v1/sessions/:id

返回 Session DTO。此端点不计算 Context token 数；Context 是每次 Provider 请求的临时视图。

## POST /api/v1/sessions/:id/pause

执行 `active -> paused`。请求 body 为空或 `{}`。成功返回：

```json
{
  "id": "ses_01J...",
  "state": "paused",
  "updated_at": "2026-07-22T01:02:00Z"
}
```

## POST /api/v1/sessions/:id/resume

执行 `paused -> active`。若已达到 max lifetime，直接返回 `40901`，不改变 Session；cleanup/Restore 负责把已过期 Session 关闭。成功结构同 Pause，state 为 `active`。

## POST /api/v1/sessions/:id/close

将任意非 Closed Session 关闭并保留历史。对 Closed 重复调用成功且不重复事件。

```json
{
  "id": "ses_01J...",
  "state": "closed",
  "updated_at": "2026-07-22T01:03:00Z"
}
```

## DELETE /api/v1/sessions/:id

物理删除 Session snapshot、内存索引和全部消息。它与 Close 不同，成功后无法查询。

```json
{"id": "ses_01J...", "deleted": true}
```

## POST /api/v1/sessions/:id/clear

清空全部消息，保留 Session state、policy 和 metadata。Closed Session 返回 `40901`。空历史是 no-op。

```json
{
  "id": "ses_01J...",
  "deleted_count": 12,
  "message_count": 0,
  "updated_at": "2026-07-22T01:04:00Z"
}
```

## GET /api/v1/sessions/:id/messages

按时间正序分页读取已提交消息。

响应中的 assistant `tool_calls[].function.name` 与非空 tool message `name` 都是 Session 保存的 canonical Tool name。历史 Tool 当前即使已删除或禁用仍原样可读；Remote 不重新绑定它，也不暴露 Provider alias。

| Query | 默认值 | 规则 |
|-------|--------|------|
| `page` | 1 | `>=1` |
| `page_size` | 50 | 1..200 |
| `role` | 全部 | `user|assistant|tool` |
| `after` | 空 | 必须是当前 Session 的 Message ID；不能与 `page>1` 同用 |

```json
{
  "items": [
    {
      "id": "msg_01J...",
      "session_id": "ses_01J...",
      "turn_id": "turn_01J...",
      "role": "assistant",
      "content": "I will check.",
      "reasoning_content": "",
      "name": "",
      "tool_calls": [
        {
          "id": "call_01J...",
          "type": "function",
          "function": {"name": "weather", "arguments": "{\"city\":\"Beijing\"}"}
        }
      ],
      "tool_call_id": "",
      "refusal": "",
      "metadata": {},
      "created_at": "2026-07-22T01:01:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 50
}
```

`turn_id` 来自 `SessionMessage.TurnID`，其余 Provider 字段直接映射 `Payload`。Session 不保存每条消息的 model 或 token usage，因此本 DTO 不制造这些字段；turn usage 由 [对话 API](conversation.md) 响应提供。system 消息不会出现在历史中。

## DELETE /api/v1/sessions/:id/messages/:msgid

删除消息。目标属于 Tool unit 时，同时删除 assistant Tool call 和全部对应 Tool results。

```json
{
  "deleted": true,
  "message_ids": ["msg_01J...", "msg_01K..."],
  "deleted_count": 2
}
```

## 错误

| 场景 | HTTP / code |
|------|-------------|
| Session、Agent 或 Message 不存在 | 404 / `40401` |
| 当前 state 不允许操作 | 409 / `40901` |
| 字段、duration 或分页参数非法 | 400 / `40001` |
| 消息序列/单条大小/数量不满足 policy，或完整持久 snapshot 超过 16 MiB | 422 / `42201` |
| Agent Session 容量已满 | 429 / `42901` |
| Storage 或 Manager 未就绪 | 503 / `50301` |

精确错误映射见 [Session 错误契约](../session/errors.md#4-remote-api-映射)。

---

*最后更新: 2026-07-22*
