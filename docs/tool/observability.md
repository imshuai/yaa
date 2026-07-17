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
  "params": {"command": "ls -la"},
  "duration_ms": 342,
  "is_error": false,
  "result_tokens": 150,
  "timestamp": "2025-07-15T16:00:00Z"
}
```

### 10.2 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `tool.calls_total` | Counter | Tool 调用总次数（按 tool name 标签） |
| `tool.calls_duration` | Histogram | Tool 执行耗时分布 |
| `tool.calls_errors` | Counter | Tool 执行错误次数 |
| `tool.calls_timeout` | Counter | Tool 超时次数 |
| `tool.concurrent` | Gauge | 当前并发执行数 |

### 10.3 Remote API 事件

Tool 执行过程中的事件通过 Remote API 推送给客户端：

```json
// Tool 开始执行
{"type": "tool_start", "tool": "shell", "call_id": "call_1", "params": {...}}

// Tool 执行完成
{"type": "tool_end", "tool": "shell", "call_id": "call_1", "duration_ms": 342, "is_error": false}

// Tool 结果（可选，受权限控制是否回传完整结果）
{"type": "tool_result", "call_id": "call_1", "content": "..."}
```

---

