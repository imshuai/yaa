# Session API

> [← 返回索引](./INDEX.md)

---

## POST /api/v1/agents/:id/sessions

为指定 Agent 创建新的 Session。

**请求 Body:**

```json
{
  "title": "Chat about Go",
  "metadata": { "source": "telegram" },
  "system_prompt_override": "You are a Go expert."
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `title` | string | ❌ | 会话标题 |
| `metadata` | object | ❌ | 自定义元数据 |
| `system_prompt_override` | string | ❌ | 覆盖 Agent 的系统提示词，仅对本次会话生效 |

**响应 data:**

```json
{
  "id": "ses_01J...",
  "agent_id": "agt_01J...",
  "title": "Chat about Go",
  "status": "created",
  "message_count": 0,
  "metadata": { "source": "telegram" },
  "system_prompt_override": "You are a Go expert.",
  "created_at": "2025-07-15T10:00:00Z",
  "updated_at": "2025-07-15T10:00:00Z"
}
```

---

## GET /api/v1/agents/:id/sessions

列出指定 Agent 的所有 Session（分页）。

**Query 参数:**

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 页码，默认 1 |
| `page_size` | int | 每页条数，默认 20 |
| `status` | string | 按状态过滤：`created` / `active` / `paused` / `closed` |

**响应 data:**

```json
{
  "items": [
    {
      "id": "ses_01J...",
      "agent_id": "agt_01J...",
      "title": "Chat about Go",
      "status": "active",
      "message_count": 12,
      "created_at": "2025-07-15T10:00:00Z",
      "updated_at": "2025-07-15T11:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

## GET /api/v1/sessions/:id

获取 Session 详情。

**响应 data:** 同创建响应结构，额外包含：

```json
{
  "context": {
    "token_count": 3500,
    "max_tokens": 8192,
    "compression_count": 0
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `context.token_count` | int | 当前上下文 token 数 |
| `context.max_tokens` | int | 上下文窗口上限 |
| `context.compression_count` | int | 已执行的上下文压缩次数 |

---

## DELETE /api/v1/sessions/:id

关闭并删除 Session。关联的消息历史一并删除。

**响应 data:**

```json
{ "deleted": true }
```

---

## POST /api/v1/sessions/:id/clear

清空 Session 的消息历史，但保留 Session 本身。

**响应 data:**

```json
{
  "id": "ses_01J...",
  "message_count": 0,
  "cleared_at": "2025-07-15T10:00:00Z"
}
```

---

## GET /api/v1/sessions/:id/messages

获取 Session 的消息历史（分页，按时间正序）。

**Query 参数:**

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 页码，默认 1 |
| `page_size` | int | 每页条数，默认 50 |
| `role` | string | 按角色过滤：`user` / `assistant` / `system` / `tool` |
| `after` | string | 返回此消息 ID 之后的消息（用于增量拉取） |

**响应 data:**

```json
{
  "items": [
    {
      "id": "msg_01J...",
      "session_id": "ses_01J...",
      "role": "user",
      "content": "Hello, who are you?",
      "tool_calls": null,
      "tool_call_id": null,
      "tokens": { "input": 10, "output": 0 },
      "model": null,
      "created_at": "2025-07-15T10:00:00Z"
    },
    {
      "id": "msg_01J...",
      "session_id": "ses_01J...",
      "role": "assistant",
      "content": "I am an AI assistant.",
      "tool_calls": null,
      "tool_call_id": null,
      "tokens": { "input": 12, "output": 8 },
      "model": "gpt-4o",
      "created_at": "2025-07-15T10:00:01Z"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 50
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 消息唯一 ID |
| `role` | string | 消息角色：`user` / `assistant` / `system` / `tool` |
| `content` | string | 消息文本内容 |
| `tool_calls` | array\|null | Assistant 发起的工具调用列表 |
| `tool_call_id` | string\|null | 工具调用结果对应的 call ID（role=tool 时） |
| `tokens.input` | int | 输入 token 数 |
| `tokens.output` | int | 输出 token 数 |
| `model` | string\|null | 生成该消息的模型（仅 assistant） |

---

## DELETE /api/v1/sessions/:id/messages/:msgid

删除指定消息。删除后上下文窗口同步更新。

**响应 data:**

```json
{ "deleted": true }
```

---

## POST /api/v1/sessions/:id/context/compress

手动触发上下文压缩。将历史消息摘要压缩，释放上下文窗口空间。

**请求 Body:**

```json
{
  "strategy": "summarize",
  "max_tokens": 2048,
  "keep_recent": 6
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `strategy` | string | ❌ | 压缩策略：`summarize`（默认）/ `truncate` / `extract` |
| `max_tokens` | int | ❌ | 压缩后保留的最大 token 数 |
| `keep_recent` | int | ❌ | 保留最近 N 条消息不压缩，默认 6 |

**响应 data:**

```json
{
  "session_id": "ses_01J...",
  "strategy": "summarize",
  "messages_before": 24,
  "messages_after": 8,
  "tokens_before": 6500,
  "tokens_after": 1800,
  "compression_count": 1,
  "compressed_at": "2025-07-15T10:00:00Z"
}
```
