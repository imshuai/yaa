# 可观测性

> 文档路径: docs/tool/observability.md
> 上级: README.md 10

---

## 10. 可观测性

### 10.1 日志

每次 Tool 执行记录结构化日志：

```json
{
  "level": "info",
  "msg": "tool executed",
  "tool": "shell",
  "agent_id": "default",
  "session_id": "sess_abc",
  "duration_ms": 342,
  "is_error": false,
  "result_tokens": 150,
  "timestamp": "2025-07-15T16:00:00Z"
}
```

日志中的 `tool` 始终是 canonical name。日志不记录 Tool params、result content、凭据或 Provider 返回的未知 alias；错误只记录稳定分类和脱敏摘要。alias 映射只存在于 turn 内存，不能进入审计存储。

### 10.2 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `yaa_tool_calls_total` | Counter | Tool 调用总次数；label: `tool`, `result` |
| `yaa_tool_call_duration_seconds` | Histogram | Tool 执行耗时；label: `tool` |
| `yaa_tool_errors_total` | Counter | Tool 执行错误次数；label: `tool`, `class` |
| `yaa_tool_timeouts_total` | Counter | Tool 超时次数；label: `tool` |
| `yaa_tool_concurrent` | Gauge | 当前并发执行数 |
| `yaa_tool_alias_projection_errors_total` | Counter | Provider 投影失败；label 仅为 `reason=collision|invalid_history|invalid_choice` |

alias、canonical name、ToolCall ID、Session ID 都不得新增为该错误指标的 label。Provider unknown/非法 alias 属于 Agent 协议错误，不计为一次 Tool 调用。

### 10.3 Remote API 事件

Remote 只使用 [ConversationFrame](../remote-api/conversation.md) 已定义的 `tool_call` 和 `tool_result`，不另外定义 `tool_start`/`tool_end`。这些 frame 的 Tool name 已由 Agent 反查为 canonical；Provider alias 不出现在 REST/SSE/WS。frame 是当前 turn 的观察信号，Session 是否已提交仍以完整 Tool unit 的 snapshot mutation 为准。

---
