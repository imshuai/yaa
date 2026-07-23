# 对话 API

> [返回索引](INDEX.md) · Session 串行边界见 [Session 集成](../session/integration.md)

同一 Session 的 REST 和 WebSocket turn 共享一个 FIFO gate；不同 Session 可并行。只有完整消息或 Tool unit 提交后才改变 Session snapshot。

## POST /api/v1/sessions/:id/messages

提交一条 user 消息并等待当前 Agent turn 完成：

```json
{
  "turn_id": "turn_01J...",
  "content": "What is the weather in Beijing?",
  "metadata": {"source": "api"}
}
```

客户端不能提交 `role`、system、assistant 或 tool 消息。成功 data：

```json
{
  "turn_id": "turn_01J...",
  "message": {
    "id": "msg_01J...",
    "turn_id": "turn_01J...",
    "role": "assistant",
    "content": "The weather is sunny.",
    "reasoning_content": "",
    "tool_calls": [],
    "tool_call_id": "",
    "refusal": "",
    "metadata": {},
    "created_at": "2026-07-22T01:00:00Z"
  },
  "usage": {"prompt_tokens": 156, "completion_tokens": 24, "total_tokens": 180},
  "tool_call_count": 1
}
```

`turn_id` 必填并遵循下文统一的长度、字符和永久唯一规则；服务端不会为 REST 请求另造第二个 ID。Handler 只校验 DTO/归属并调用 `agent.Manager.HandleTurn`；由 Agent 在 Session FIFO callback 中先 `AppendUser`，再执行 Memory 检索、Context Build、Provider/Tool loop 和 final assistant 提交。Provider/Tool/Context 失败不会伪造 final assistant；此前已提交的 user 或完整 Tool unit 保留。HTTP write deadline 使用 request/Provider/Tool policy，不受普通控制端点的固定 30s 截断。

Handler 按 cause 优先映射 turn 失败：request context 的 cause 为 `context.DeadlineExceeded` 时使用 504 / `50401`；`ErrAgentStopped` 或其他 invalid state 使用 409 / `40901`；`ErrAgentManagerClosed` 使用 503 / `50301`；显式 client cancel 通常不再写 HTTP response，流连接发送 `code:"canceled"`。只有可由 `errors.As` 识别的真实 `*provider.ProviderError` 使用 502 / `50202`；Provider 自己的总 deadline 也属于该上游分类。`ErrAgentToolRoundLimit`、`ErrAgentProviderProtocol`、Planner 生成/解析/校验/执行失败和其他内部错误使用 500 / `50001`，不得伪装成 Provider 上游失败。

REST 与 WebSocket handler 都构造 `TurnRequest{Stream:true}`，并把非阻塞 `Emit` 接到该 Session 的 Event Hub；因此先建立的 SSE 订阅能观察任一入口发起的增量。REST 仍等待 `HandleTurn` 返回并发送普通 JSON response；WS writer 从同一 hub 接收 frame。`assistant_done` 只能在 Agent 返回已持久化 assistant 后发布，失败只发布一个 `error` 终态。

## GET /api/v1/sessions/:id/events

只读 SSE 订阅。客户端先订阅，再用 REST POST 或 WS 发起 turn。SSE 使用 `text/event-stream`，每 15 秒发送 `: heartbeat`。

所有业务 data 都是同一 `ConversationFrame` JSON；`event` 名称只用于过滤：

```text
event: conversation
data: {"type":"assistant_delta","turn_id":"turn_01J...","delta":"The weather"}

event: session
data: {"type":"session_event","turn_id":"turn_01J...","event":{"event_id":"evt_01J...","type":"session.message.appended","session_id":"ses_01J...","agent_id":"default","occurred_at":"2026-07-22T01:00:00Z","data":{"turn_id":"turn_01J...","message_ids":["msg_01J..."],"roles":["user"],"count":1}}}
```

唯一 wire DTO 如下；所有可选 pointer 字段只在对应 `type` 出现，解码器必须拒绝未知字段和不符合下表的组合：

```go
type ConversationFrame struct {
    Type          string              `json:"type"`
    TurnID        string              `json:"turn_id,omitempty"`
    Position      *int                `json:"position,omitempty"`
    Delta         *string             `json:"delta,omitempty"`
    ToolCall      *provider.ToolCall  `json:"tool_call,omitempty"`
    ToolResult    *ToolResultView     `json:"tool_result,omitempty"`
    Assistant     *SessionMessageView `json:"assistant,omitempty"`
    Usage         *provider.Usage     `json:"usage,omitempty"`
    ToolCallCount *int                `json:"tool_call_count,omitempty"`
    Event         *SessionEventView   `json:"event,omitempty"`
    Code          string              `json:"code,omitempty"`
    Message       string              `json:"message,omitempty"`
    Reason        string              `json:"reason,omitempty"`
}

type ToolResultView struct {
    ToolCallID string `json:"tool_call_id"`
    Name       string `json:"name"`
    Content    string `json:"content"`
    IsError    bool   `json:"is_error"`
}

type SessionMessageView struct {
    ID               string              `json:"id"`
    TurnID           string              `json:"turn_id"`
    Role             string              `json:"role"`
    Content          string              `json:"content"`
    ReasoningContent string              `json:"reasoning_content"`
    ToolCalls        []provider.ToolCall `json:"tool_calls"`
    ToolCallID       string              `json:"tool_call_id"`
    Refusal          string              `json:"refusal"`
    Metadata         map[string]any      `json:"metadata"`
    CreatedAt        time.Time           `json:"created_at"`
}

type SessionEventView struct {
    EventID    string         `json:"event_id"`
    Type       string         `json:"type"`
    SessionID  string         `json:"session_id"`
    AgentID    string         `json:"agent_id"`
    OccurredAt time.Time      `json:"occurred_at"`
    Data       map[string]any `json:"data"`
}
```

`ConversationFrame` 中 `tool_call.function.name` 与 `tool_result.name` 已由 Agent 精确反查为 canonical Tool name；`assistant.tool_calls` 来自已提交的 canonical Session message。Remote 不接收 alias map，也不能把 Provider 原始 alias 直接转发。unknown/非法/history-only alias 使 turn 以 `ErrAgentProviderProtocol` 失败，在任何 `tool_call` frame 或 Tool 执行之前终止。

`ConversationFrame.type` 与必需字段：

| type | 必需字段 | 说明 |
|------|----------|------|
| `queued` | `turn_id`, `position` | turn 已进入 FIFO；position 可为 0 |
| `assistant_start` | `turn_id` | Provider 开始响应 |
| `reasoning_delta` | `turn_id`, `delta` | Provider 明确提供的推理增量；delta 可为空串 |
| `assistant_delta` | `turn_id`, `delta` | 最终回答文本增量；delta 可为空串 |
| `tool_call` | `turn_id`, `tool_call` | Provider call 完成校验及 alias 反查后的 canonical 描述 |
| `tool_result` | `turn_id`, `tool_result` | ExecuteBatch 收拢后按 call 顺序发布；不是实时完成或提交证明，不暴露 `ToolResult.Meta` |
| `assistant_done` | `turn_id`, `assistant`, `usage`, `tool_call_count` | final assistant 已提交 |
| `session_event` | `event`；`turn_id` 可选 | 已提交的 Session mutation event |
| `error` | `turn_id`, `code`, `message` | turn 失败；稳定 code/message |
| `session_end` | `reason` | Session Closed/Deleted，reason 为 `closed|deleted`，订阅结束 |

Frame `code` 的 JSON 类型固定为 string：取消为 `"canceled"`，REST business code 在 frame 中使用十进制字符串，例如 `"50001"`、`"50202"`、`"50401"`；不得混用 JSON number。每个 turn 恰好发送一个终态 `assistant_done` 或 `error`；`session_end` 只终止订阅，不替代 turn 终态。

每个 SSE 或 WS 订阅者使用固定容量 256 的独立输出队列和单一 writer。发布者只做非阻塞 enqueue；队列已满时 Event Hub 原子注销该订阅者并关闭连接，不等待客户端、不逐帧丢弃，也不影响 Session 提交或 turn。SSE 断开只取消订阅，不取消由其他连接发起的 turn。

v1 不保存 frame replay buffer，也不实现 sequence cursor。SSE 的 `Last-Event-ID` 被忽略，重连只收到连接建立后的新 frame；客户端必须通过 Session/Message REST 读取已提交状态，不能把 SSE/WS 当作 source of truth。

## GET /api/v1/sessions/:id/stream (WebSocket)

握手必须使用 Authorization Header。连接建立后客户端 frame：

```json
{"type":"message","turn_id":"turn_01J...","content":"Tell me a joke","metadata":{}}
```

`turn_id` 是客户端生成的 1..128 UTF-8 bytes 字符串，不能含控制字符，并在该 Session 内永久唯一；服务端从 `queued` 到终态 frame 始终原样回显。重复 queued/running 或已经提交 user 的 `turn_id` 返回 `40001`，因此客户端在发送后即可可靠地寻址取消目标。

```json
{"type":"cancel","turn_id":"turn_01J..."}
```

WebSocket ping/pong 使用 transport control frame，不是应用层 `ConversationFrame`，服务端不得把它写入 Session 或 SSE。

服务端对 `message` 先发送：

```json
{"type":"queued","turn_id":"turn_01J...","position":0}
```

`cancel` 可取消匹配的排队或运行中 turn。queued turn 在 user 提交前取消不消费 ID；user 一旦提交，该 ID 即使 turn 失败或历史被删除也不再复用。已经提交的 user/Tool unit 不回滚；取消后的 turn 发送一个 `error` frame（`code:"canceled"`，不是 `"50401"`）。只有请求 deadline exceeded 才使用 REST code `50401` 或 Frame code `"50401"`。每个连接最多一个运行中 turn，其余由 Session FIFO 排队；连接断开取消该连接发起的全部非终态 turn。

WS 与 SSE 使用相同的 `ConversationFrame` 字段；WS 不再包一层不同的 `session_event` 结构。

---

*最后更新: 2026-07-22*
