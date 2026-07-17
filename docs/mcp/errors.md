# MCP 错误处理与降级策略

> 文档路径: `docs/mcp/errors.md`
> 上级: `docs/mcp/README.md`

---

## 1. 错误分类总览

MCP 层涉及三类实体：MCP Server（远端服务）、Transport（传输层）、Tool（由 MCP Server 暴露的工具）。错误按发生阶段分类如下：

| 错误类型 | 枚举值 | 发生阶段 | 可恢复 | 默认处理 |
|---------|--------|---------|--------|---------|
| `ErrMCPConnRefused` | `conn_refused` | 连接建立 | ✅ | 重连 |
| `ErrMCPConnTimeout` | `conn_timeout` | 连接建立 | ✅ | 重连（指数退避） |
| `ErrMCPAuthFailed` | `auth_failed` | 连接鉴权 | ❌ | 返回错误，不重试 |
| `ErrMCPTransportClosed` | `transport_closed` | 传输中 | ✅ | 自动重连 |
| `ErrMCPTransportWrite` | `transport_write` | 传输中 | ✅ | 重连后重发 |
| `ErrMCPProtocolError` | `protocol_error` | 协议解析 | ⚠️ | 重连一次，仍失败则禁用 Server |
| `ErrMCPToolNotFound` | `tool_not_found` | Tool 调用 | ❌ | 返回给 LLM |
| `ErrMCPToolExecFailed` | `tool_exec_failed` | Tool 执行 | ⚠️ | 由 LLM 决定是否重试 |
| `ErrMCPToolTimeout` | `tool_timeout` | Tool 执行 | ✅ | 可配置超时后取消 |
| `ErrMCPServerOverloaded` | `server_overloaded` | Tool 执行 | ✅ | 降级或排队 |

---

## 2. 连接错误

### 2.1 连接建立流程

```text
Dial → Handshake → Initialize → ListTools → Ready
  │         │          │             │
  │         │          │             └─ ErrMCPProtocolError
  │         │          └─ ErrMCPAuthFailed
  │         └─ ErrMCPConnTimeout
  └─ ErrMCPConnRefused
```

### 2.2 连接错误处理

```go
func (m *Manager) connect(ctx context.Context, server *MCPServerConfig) (*ClientConn, error) {
    // 1. 建立传输层
    transport, err := m.transportFactory.Dial(ctx, server.Endpoint)
    if err != nil {
        if isTimeout(err) {
            return nil, fmt.Errorf("%w: %s (endpoint=%s)", ErrMCPConnTimeout, err, server.Endpoint)
        }
        return nil, fmt.Errorf("%w: %s (endpoint=%s)", ErrMCPConnRefused, err, server.Endpoint)
    }

    // 2. 发送 Initialize 请求
    initResp, err := m.sendInitialize(ctx, transport, server)
    if err != nil {
        transport.Close()
        if isAuthError(err) {
            return nil, fmt.Errorf("%w: %s", ErrMCPAuthFailed, err)
        }
        return nil, fmt.Errorf("%w: %s", ErrMCPProtocolError, err)
    }

    // 3. 注册连接
    conn := &ClientConn{
        ServerID:     server.ID,
        Transport:    transport,
        ProtocolVer:  initResp.ProtocolVersion,
        Capabilities: initResp.Capabilities,
        Status:       ConnStatusReady,
        ConnectedAt:  time.Now(),
    }

    m.logger.Info("mcp server connected",
        "server", server.ID,
        "endpoint", server.Endpoint,
        "protocol_version", initResp.ProtocolVersion,
    )

    return conn, nil
}
```

---

## 3. 传输错误

传输层负责 JSON-RPC 消息的收发。传输错误分为写入失败和连接意外关闭两类。

```go
func (c *ClientConn) send(ctx context.Context, req *Request) (*Response, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.Status != ConnStatusReady {
        return nil, fmt.Errorf("%w: server=%s status=%v", ErrMCPTransportClosed, c.ServerID, c.Status)
    }

    // 写入请求
    if err := c.Transport.Write(ctx, req); err != nil {
        c.Status = ConnStatusError
        m.logger.Warn("mcp transport write failed", "server", c.ServerID, "error", err)
        return nil, fmt.Errorf("%w: %s", ErrMCPTransportWrite, err)
    }

    // 读取响应
    resp, err := c.Transport.Read(ctx)
    if err != nil {
        if isTimeout(err) {
            return nil, fmt.Errorf("%w: %s", ErrMCPToolTimeout, err)
        }
        c.Status = ConnStatusError
        return nil, fmt.Errorf("%w: %s", ErrMCPTransportClosed, err)
    }

    return resp, nil
}
```

---

## 4. Tool 执行错误

MCP Server 暴露的 Tool 执行失败时，错误需要传递回 Agent 层，由 LLM 决定后续动作。

```go
func (m *Manager) callTool(ctx context.Context, serverID, toolName string, args map[string]any) (*ToolResult, error) {
    conn, err := m.getConn(serverID)
    if err != nil {
        return nil, err
    }

    req := &Request{
        Method: "tools/call",
        Params: map[string]any{
            "name":      toolName,
            "arguments": args,
        },
    }

    resp, err := conn.send(ctx, req)
    if err != nil {
        return nil, err
    }

    // MCP Server 返回错误
    if resp.Error != nil {
        m.logger.Warn("mcp tool exec failed",
            "server", serverID,
            "tool", toolName,
            "code", resp.Error.Code,
            "message", resp.Error.Message,
        )

        switch {
        case resp.Error.Code == -32601: // Method not found
            return nil, fmt.Errorf("%w: server=%s tool=%s", ErrMCPToolNotFound, serverID, toolName)
        case resp.Error.Code == -32001: // Server-defined: overloaded
            return nil, fmt.Errorf("%w: server=%s tool=%s", ErrMCPServerOverloaded, serverID, toolName)
        default:
            return nil, fmt.Errorf("%w: server=%s tool=%s code=%d msg=%s",
                ErrMCPToolExecFailed, serverID, toolName, resp.Error.Code, resp.Error.Message)
        }
    }

    return parseToolResult(resp.Result)
}
```

---

## 5. 超时处理

| 超时项 | 配置键 | 默认值 | 触发后行为 |
|--------|--------|--------|-----------|
| 连接超时 | `mcp.timeout.connect` | 10s | 标记连接失败，进入重连队列 |
| 初始化超时 | `mcp.timeout.init` | 15s | 关闭连接，返回错误 |
| Tool 调用超时 | `mcp.timeout.tool` | 30s | 取消 ctx，返回 `ErrMCPToolTimeout` |
| 空闲超时 | `mcp.timeout.idle` | 5m | 发送 ping，无响应则关闭连接 |
| 重连间隔 | `mcp.reconnect.interval` | 1s | 指数退避，上限 60s |

```go
func (m *Manager) callToolWithTimeout(ctx context.Context, serverID, toolName string, args map[string]any) (*ToolResult, error) {
    timeout := m.config.MCP.Timeout.Tool // 默认 30s
    if t, ok := args["_timeout_ms"]; ok {
        if ms, err := toInt(t); err == nil && ms > 0 {
            timeout = time.Duration(ms) * time.Millisecond
        }
    }

    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    result, err := m.callTool(ctx, serverID, toolName, args)
    if ctx.Err() == context.DeadlineExceeded {
        m.metrics.ToolTimeoutCounter.Inc(serverID, toolName)
        return nil, fmt.Errorf("%w: server=%s tool=%s timeout=%s", ErrMCPToolTimeout, serverID, toolName, timeout)
    }
    return result, err
}
```

---

## 6. 重连策略

```text
连接断开
    │
    ├─ 标记 Status = Disconnected
    ├─ 记录断开时间
    └─ 启动重连协程
         │
         ├─ Attempt 1: 间隔 1s
         ├─ Attempt 2: 间隔 2s
         ├─ Attempt 3: 间隔 4s
         ├─ ...
         ├─ Attempt N: 间隔 min(2^N, 60s)
         │
         ├─ 成功 → Status = Ready，重置计数器
         └─ 超过 maxRetries → Status = Failed，发送告警事件
```

```go
func (m *Manager) reconnectLoop(ctx context.Context, serverID string) {
    backoff := m.config.MCP.Reconnect.Interval // 1s
    maxBackoff := 60 * time.Second
    maxRetries := m.config.MCP.Reconnect.MaxRetries // 10

    for attempt := 1; attempt <= maxRetries; attempt++ {
        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff):
        }

        conn, err := m.connect(ctx, m.servers[serverID])
        if err == nil {
            m.conns.Store(serverID, conn)
            m.logger.Info("mcp reconnected", "server", serverID, "attempt", attempt)
            m.emitEvent("mcp.reconnected", map[string]any{"server": serverID, "attempt": attempt})
            return
        }

        m.logger.Warn("mcp reconnect failed", "server", serverID, "attempt", attempt, "error", err)
        backoff = min(backoff*2, maxBackoff) // 指数退避
    }

    // 超过最大重试次数
    m.logger.Error("mcp server unreachable, giving up", "server", serverID, "attempts", maxRetries)
    m.emitEvent("mcp.server_failed", map[string]any{"server": serverID, "reason": "max_retries_exceeded"})
}
```

---

## 7. 降级策略

当 MCP Server 不可用时，Runtime 需要决定如何降级，避免影响 Agent 主流程。

| 场景 | 降级策略 | 影响 |
|------|---------|------|
| 单个 MCP Server 宕机 | 从 Tool 注册表移除该 Server 的所有 Tool | LLM 无法调用这些 Tool，但不影响其他 Tool |
| 所有 MCP Server 宕机 | 仅保留内置 Tool | LLM 可继续使用内置能力 |
| Tool 执行超时 | 返回超时错误给 LLM | LLM 可选择换 Tool 或告知用户 |
| Tool 返回错误 | 原样返回给 LLM | LLM 可调整参数重试或放弃 |
| 协议版本不兼容 | 拒绝连接，记录日志 | 管理员需升级 MCP Server |
| Server 过载 | 排队等待或返回降级提示 | 可配置是否阻塞或快速失败 |

```go
func (m *Manager) handleServerFailure(serverID string) {
    // 1. 标记连接为 Failed
    if conn, ok := m.conns.Load(serverID); ok {
        conn.Status = ConnStatusFailed
    }

    // 2. 从 Tool 注册表移除该 Server 的所有 Tool
    removed := m.toolRegistry.RemoveByServer(serverID)
    m.logger.Warn("mcp server tools removed due to failure",
        "server", serverID,
        "tools_removed", len(removed),
    )

    // 3. 通知 Agent 层 Tool 列表已更新
    m.emitEvent("mcp.tools_unavailable", map[string]any{
        "server": serverID,
        "tools":  removed,
    })

    // 4. 启动后台重连
    go m.reconnectLoop(context.Background(), serverID)
}
```

---

## 8. 错误事件与可观测性

### 8.1 SSE 事件

| 事件 | 触发时机 | Payload |
|------|---------|---------|
| `mcp.connect_failed` | 连接建立失败 | server, endpoint, error |
| `mcp.reconnected` | 重连成功 | server, attempt |
| `mcp.server_failed` | 重连耗尽 | server, reason |
| `mcp.tool_error` | Tool 执行返回错误 | server, tool, code, message |
| `mcp.tools_unavailable` | Server 故障导致 Tool 下线 | server, tools[] |

### 8.2 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `mcp_conn_total` | Gauge | server, status | 当前连接数（按状态） |
| `mcp_conn_failed_total` | Counter | server, reason | 连接失败次数 |
| `mcp_reconnect_total` | Counter | server | 重连尝试次数 |
| `mcp_tool_call_total` | Counter | server, tool | Tool 调用次数 |
| `mcp_tool_error_total` | Counter | server, tool, code | Tool 执行错误次数 |
| `mcp_tool_timeout_total` | Counter | server, tool | Tool 超时次数 |
| `mcp_tool_duration` | Histogram | server, tool | Tool 执行耗时 |
