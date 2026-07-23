# MCP 可观测性

> 文档路径: `docs/mcp/observability.md`
> 上级: [`README.md`](README.md)

---

## 1. 结构化日志

每条日志至少包含 `server`、`transport`、`status` 和 `request_id`（存在调用时）。HTTP Header、Token、环境变量值、Tool Secret 参数和完整 Tool 结果不得写入日志。

| event | level | 关键字段 |
|-------|-------|----------|
| `mcp.server.connecting` | info | `server`, `transport`, `endpoint`, `attempt` |
| `mcp.server.connected` | info | `server`, `protocol_version`, `tool_count` |
| `mcp.server.disconnected` | warn | `server`, `reason`, `uptime_ms` |
| `mcp.server.reconnect_scheduled` | info | `server`, `attempt`, `backoff_ms` |
| `mcp.server.error` | error | `server`, `error_type`, `message` |
| `mcp.tool.called` | info | `server`, `tool`, `duration_ms`, `is_error` |

Endpoint 日志只记录 `scheme://host/path`，移除 userinfo、query 和 fragment。

## 2. 指标

以下名称是 MCP 模块的唯一指标契约；其他文档只引用本表。

| 名称 | 类型 | labels | 说明 |
|------|------|--------|------|
| `yaa_mcp_servers` | Gauge | `status`, `transport` | 各状态的上游 Server 数量 |
| `yaa_mcp_tool_calls_total` | Counter | `server`, `tool`, `result` | Tool 调用总数；result=`success|error|timeout` |
| `yaa_mcp_tool_call_duration_seconds` | Histogram | `server`, `tool` | Tool 调用耗时 |
| `yaa_mcp_reconnects_total` | Counter | `server`, `result` | 重连次数 |
| `yaa_mcp_tools` | Gauge | `server` | 当前注册的 MCP Tool 数量 |

指标 label 不包含 request ID、Session ID、错误消息或其他高基数字段。

## 3. 健康快照

```go
type HealthReport struct {
    Total        int            `json:"total"`
    Connecting   int            `json:"connecting"`
    Connected    int            `json:"connected"`
    Disconnected int            `json:"disconnected"`
    Error        int            `json:"error"`
    Servers      []ServerStatus `json:"servers"`
    LocalState   string         `json:"local_state"` // disabled | prepared | serving | unhealthy | stopped
    LocalError   string         `json:"local_error,omitempty"`
    Ready        bool           `json:"ready"`
}
```

`ServerStatus` 的唯一类型定义见 [README](README.md#2-manager)。`protocol_version` 和 `connected_at` 在尚未完成 initialize 时为 `null`。`Ready` 直接投影 `Manager.Ready()`；本地 Server 未启用时 `LocalState=disabled` 且 Ready=true，Serve 非预期退出时 `LocalState=unhealthy` 且 Ready=false。`LastError`/`LocalError` 必须脱敏并限制长度；健康端点不能返回 Header、Token、子进程环境或 Tool 参数。MCP 没有全局 Remote SSE 路由，状态变化只写日志和指标；客户端通过只读 MCP API 获取快照。

---

*最后更新: 2025-07-17*
