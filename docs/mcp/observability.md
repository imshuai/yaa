# MCP 可观测性

> 文档路径: `docs/mcp/observability.md`
> 上级: `docs/mcp/README.md`

---

本文档定义 MCP Server 的可观测性方案，包括连接状态日志、Tool 调用指标、SSE 事件以及健康检查。

---

## 1. 连接状态日志

MCP Manager 在 Server 连接生命周期中记录结构化日志：

```go
// MCPManager 在连接各阶段输出日志。
func (m *Manager) connect(ctx context.Context, name string, cfg ServerConfig) error {
    m.log.Info("mcp.server.connecting",
        "server", name,
        "transport", cfg.Transport,
        "endpoint", cfg.Endpoint,
    )

    client, err := m.transport.Dial(ctx, cfg)
    if err != nil {
        m.log.Error("mcp.server.connect_failed",
            "server", name,
            "error", err,
        )
        return err
    }

    m.log.Info("mcp.server.connected",
        "server", name,
        "protocol_version", client.ProtocolVersion(),
        "tools", len(client.Tools()),
    )
    return nil
}
```

**断开与重连日志：**

```go
func (m *Manager) disconnect(name string, reason string) {
    m.log.Warn("mcp.server.disconnected",
        "server", name,
        "reason", reason,
        "uptime", time.Since(m.servers[name].ConnectedAt),
    )

    if m.servers[name].AutoReconnect {
        m.log.Info("mcp.server.reconnect_scheduled",
            "server", name,
            "backoff", m.servers[name].Backoff.String(),
        )
    }
}
```

---

## 2. Tool 调用指标

MCP Manager 为每个 Server 下的 Tool 调用维护运行时指标：

```go
// MCPToolStats 是 MCP Tool 的运行时统计。
type MCPToolStats struct {
    ServerName    string        // 所属 MCP Server
    ToolName     string        // Tool 名称
    CallCount    int           // 调用次数
    ErrorCount   int           // 错误次数
    SuccessRate  float64       // 成功率
    TotalLatency time.Duration  // 总延迟
    AvgLatency   time.Duration  // 平均延迟
    MaxLatency   time.Duration  // 最大延迟
    LastCalled   time.Time      // 最后调用时间
}
```

```go
// recordToolCall 记录一次 Tool 调用。
func (m *Manager) recordToolCall(server, tool string, latency time.Duration, err error) {
    stats := m.toolStats[server+"/"+tool]
    stats.CallCount++
    stats.TotalLatency += latency
    stats.AvgLatency = stats.TotalLatency / time.Duration(stats.CallCount)
    if latency > stats.MaxLatency {
        stats.MaxLatency = latency
    }
    if err != nil {
        stats.ErrorCount++
    }
    stats.SuccessRate = float64(stats.CallCount-stats.ErrorCount) / float64(stats.CallCount)
    stats.LastCalled = time.Now()
}
```

---

## 3. SSE 事件表

MCP 可观测性通过 SSE 向订阅客户端推送以下事件：

| 事件 | 触发时机 | Payload 字段 | 说明 |
|------|---------|-------------|------|
| `mcp.server.connecting` | 开始连接 Server | `server`, `transport`, `endpoint` | 连接尝试开始 |
| `mcp.server.connected` | Server 连接成功 | `server`, `protocol_version`, `tools` | 握手完成 |
| `mcp.server.disconnected` | Server 断开 | `server`, `reason`, `uptime` | 主动或被动断开 |
| `mcp.server.reconnect_scheduled` | 计划重连 | `server`, `backoff`, `attempt` | 自动重连场景 |
| `mcp.tool.called` | Tool 被调用 | `server`, `tool`, `latency_ms`, `success` | 每次调用结束 |
| `mcp.tool.registered` | Tool 注册/刷新 | `server`, `tool`, `schema_version` | 工具列表变更 |

**事件示例：**

```json
{
  "event": "mcp.tool.called",
  "data": {
    "server": "filesystem",
    "tool": "read_file",
    "latency_ms": 12,
    "success": true,
    "timestamp": "2026-07-17T09:30:00Z"
  }
}
```

```json
{
  "event": "mcp.server.disconnected",
  "data": {
    "server": "github",
    "reason": "transport closed",
    "uptime": "2h15m",
    "timestamp": "2026-07-17T09:31:00Z"
  }
}
```

---

## 4. 健康检查

```go
// MCPHealthReport 是 MCP 系统的健康报告。
type MCPHealthReport struct {
    TotalServers int            // 配置的 Server 总数
    Connected    int            // 当前已连接
    Disconnected int            // 已断开
    Error        int            // 处于错误状态
    Details      []MCPHealth    // 各 Server 详情
}

// MCPHealth 是单个 Server 的健康状态。
type MCPHealth struct {
    Name         string        // Server 名称
    Status       string        // connected / disconnected / error
    Transport    string        // 传输方式
    Uptime       time.Duration // 连接存活时长
    ToolCount    int           // 暴露的 Tool 数量
    LatencyP99   time.Duration // P99 延迟
    Warnings     []string      // 告警信息
}
```

```go
// HealthCheck 检查所有 MCP Server 的健康状态。
func (m *Manager) HealthCheck() MCPHealthReport {
    report := MCPHealthReport{
        TotalServers: len(m.servers),
        Details:       make([]MCPHealth, 0),
    }
    for name, s := range m.servers {
        h := MCPHealth{
            Name:      name,
            Status:    s.Status,
            Transport: s.Config.Transport,
            Uptime:    time.Since(s.ConnectedAt),
            ToolCount: len(s.Tools),
        }
        switch s.Status {
        case "connected":
            report.Connected++
            if h.LatencyP99 > 5*time.Second {
                h.Warnings = append(h.Warnings, "P99 latency exceeds 5s")
            }
        case "disconnected":
            report.Disconnected++
        case "error":
            report.Error++
            h.Warnings = append(h.Warnings, s.LastError)
        }
        report.Details = append(report.Details, h)
    }
    return report
}
```

**健康检查结果示例：**

```json
{
  "total_servers": 3,
  "connected": 2,
  "disconnected": 1,
  "error": 0,
  "details": [
    {"name": "filesystem", "status": "connected", "transport": "stdio", "uptime": "1h30m", "tool_count": 4, "latency_p99": "15ms"},
    {"name": "github", "status": "connected", "transport": "sse", "uptime": "45m", "tool_count": 8, "latency_p99": "320ms"},
    {"name": "database", "status": "disconnected", "transport": "websocket", "uptime": "0s", "tool_count": 0, "warnings": ["transport closed"]}
  ]
}
```
