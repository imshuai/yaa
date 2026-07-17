# 对话 API

> [← 返回索引](./INDEX.md)

---

## POST /api/v1/sessions/:id/messages

发送消息（非流式）。阻塞等待 Agent 完整响应后返回。

**请求 Body:**

```json
{
  "content": "What is the weather in Beijing?",
  "role": "user",
  "metadata": { "source": "api" }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `content` | string | ✅ | 消息内容 |
| `role` | string | ❌ | 消息角色，默认 `user` |
| `metadata` | object | ❌ | 自定义元数据 |

**响应 data:**

```json
{
  "message": {
    "id": "msg_01J...",
    "role": "assistant",
    "content": "The weather in Beijing is sunny, 32°C.",
    "model": "gpt-4o",
    "tokens": { "input": 156, "output": 24 },
    "tool_calls": [
      {
        "id": "call_01J...",
        "name": "web_search",
        "arguments": { "query": "Beijing weather" },
        "result": { "temp": 32, "condition": "sunny" }
      }
    ],
    "created_at": "2025-07-15T10:00:00Z"
  },
  "usage": {
    "total_tokens": 180,
    "input_tokens": 156,
    "output_tokens": 24,
    "tool_calls": 1
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `message` | object | Assistant 的回复消息 |
| `message.tool_calls` | array\|null | 本次回复中触发的工具调用及结果 |
| `usage.total_tokens` | int | 本次对话总 token 消耗 |
| `usage.tool_calls` | int | 工具调用次数 |

---

## GET /api/v1/sessions/:id/events

事件流（SSE）。建立连接后，Session 中所有事件实时推送。

**响应头:**

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

**SSE 事件格式:**

```
event: message
data: {"type":"user","message":{"role":"user","content":"Hello"}}

event: message
data: {"type":"assistant_start","message":{"role":"assistant","model":"gpt-4o"}}

event: message
data: {"type":"reasoning_delta","delta":"让我分析一下这个问题..."}

event: message
data: {"type":"reasoning_delta","delta":"首先需要比较两个数字..."}

event: message
data: {"type":"assistant_delta","delta":"I am"}

event: message
data: {"type":"assistant_delta","delta":" an AI"}

event: message
data: {"type":"assistant_done","message":{"id":"msg_01J...","content":"I am an AI","tokens":{"input":12,"output":8}}}

event: message
data: {"type":"tool_call","tool_call":{"id":"call_01J...","name":"web_search","arguments":{"query":"weather"}}}

event: message
data: {"type":"tool_result","tool_call_id":"call_01J...","result":{"temp":32}}
```

**事件类型:**

| type | 说明 |
|------|------|
| `user` | 用户消息已接收 |
| `assistant_start` | Agent 开始生成回复 |
| `reasoning_delta` | 思维链增量片段（深度思考模式） |
| `assistant_delta` | 流式 token 片段 |
| `assistant_done` | Agent 回复完成 |
| `tool_call` | Agent 发起工具调用 |
| `tool_result` | 工具调用返回结果 |
| `error` | 错误事件 |
| `session_end` | 会话结束 |

---

## WS /api/v1/sessions/:id/stream

流式对话（WebSocket）。双向通信，客户端发送消息，服务端流式返回。

**连接:** `ws://host:port/api/v1/sessions/:id/stream?token=<token>`

**客户端 → 服务端消息:**

```json
{
  "type": "message",
  "content": "Tell me a joke",
  "metadata": {}
}
```

| type | 说明 |
|------|------|
| `message` | 发送对话消息 |
| `ping` | 心跳检测 |

**服务端 → 客户端消息:**

```json
{
  "type": "assistant_start",
  "message": { "role": "assistant", "model": "gpt-4o" }
}
```

```json
{
  "type": "reasoning_delta",
  "delta": "让我分析一下..."
}
```

```json
{
  "type": "assistant_delta",
  "delta": "Why did"
}
```

```json
{
  "type": "assistant_done",
  "message": {
    "id": "msg_01J...",
    "content": "Why did the chicken cross the road?",
    "tokens": { "input": 10, "output": 12 }
  }
}
```

```json
{
  "type": "tool_call",
  "tool_call": { "id": "call_01J...", "name": "web_search", "arguments": {} }
}
```

```json
{
  "type": "tool_result",
  "tool_call_id": "call_01J...",
  "result": {}
}
```

```json
{
  "type": "error",
  "code": 50001,
  "message": "provider error"
}
```

```json
{
  "type": "pong"
}
```
