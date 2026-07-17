# MCP Client 设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/mcp/client.md`
> 依赖: `docs/architecture.md` §3.9 MCP

---

## 1. 概述

MCP Client 是 Yaa! MCP 双角色之一。它负责**连接外部 MCP Server**，
将其提供的 Tool 发现并映射为 Yaa! 内部 Tool，使 Agent 可以像调用
内置 Tool 一样调用外部 MCP Server 的能力。

```text
外部 MCP Server          Yaa! Runtime
┌─────────────┐         ┌──────────────────┐
│  filesystem │◄─stdio──│  MCP Client      │
│  github     │◄─SSE────│  ├─ discover()   │
│  database   │◄─WS─────│  ├─ mapTools()   │
└─────────────┘         │  └─ callTool()   │
                        │                  │
                        │  Tool Manager     │
                        │  (统一注册)        │
                        └──────────────────┘
```

---

## 2. 核心接口

### 2.1 MCPClient

```go
// MCPClient 管理与单个外部 MCP Server 的连接。
type MCPClient struct {
    name      string              // Server 名称（唯一标识）
    transport Transport           // 传输层（stdio/SSE/WS）
    status    ConnectionStatus    // 连接状态
    tools     []MCPToolInfo        // 已发现的 Tool 列表
    mu        sync.RWMutex
}

// ConnectionStatus 连接状态
type ConnectionStatus string

const (
    StatusDisconnected ConnectionStatus = "disconnected"
    StatusConnecting   ConnectionStatus = "connecting"
    StatusConnected    ConnectionStatus = "connected"
    StatusError        ConnectionStatus = "error"
)
```

### 2.2 核心方法

```go
// Connect 建立与 MCP Server 的连接
func (c *MCPClient) Connect(ctx context.Context) error

// DiscoverTools 发现 Server 提供的 Tool 列表
func (c *MCPClient) DiscoverTools(ctx context.Context) ([]MCPToolInfo, error)

// CallTool 调用 Server 上的指定 Tool
func (c *MCPClient) CallTool(ctx context.Context, toolName string, args map[string]any) (json.RawMessage, error)

// Close 断开连接，释放资源
func (c *MCPClient) Close() error

// Health 健康检查
func (c *MCPClient) Health() error
```

### 2.3 MCPToolInfo

```go
// MCPToolInfo 描述从 MCP Server 发现的 Tool
type MCPToolInfo struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`  // JSON Schema
    ServerName  string         `json:"serverName"`     // 来源 Server
}
```

---

## 3. Tool 发现与映射

### 3.1 发现流程

```text
Connect()
  │
  ▼
Send: tools/list  ──→  MCP Server
  │
  │  返回 Tool 列表
  ▼
Receive: tools/list response
  │
  │  解析为 []MCPToolInfo
  ▼
MapToYaaTools()
  │
  │  将每个 MCPToolInfo 转为 Yaa! Tool
  │  命名规则: mcp.{server}.{tool}
  ▼
ToolManager.Register()
  │
  │  注册到统一 Tool 注册表
  ▼
Agent 可通过 ToolManager 调用
```

### 3.2 Tool 映射

```go
// MapToYaaTools 将 MCP Tool 映射为 Yaa! Tool
func (c *MCPClient) MapToYaaTools() []tool.Tool {
    var yaaTools []tool.Tool
    for _, mcpTool := range c.tools {
        yaaTool := &MCPToolAdapter{
            info:     mcpTool,
            client:   c,
            toolName: fmt.Sprintf("mcp.%s.%s", c.name, mcpTool.Name),
        }
        yaaTools = append(yaaTools, yaaTool)
    }
    return yaaTools
}
```

### 3.3 MCPToolAdapter

```go
// MCPToolAdapter 将 MCP Tool 适配为 Yaa! Tool 接口
type MCPToolAdapter struct {
    info     MCPToolInfo
    client   *MCPClient
    toolName string
}

func (a *MCPToolAdapter) Name() string { return a.toolName }

func (a *MCPToolAdapter) Description() string { return a.info.Description }

func (a *MCPToolAdapter) Parameters() json.RawMessage { return a.info.InputSchema }

func (a *MCPToolAdapter) Execute(ctx context.Context, args map[string]any) (tool.ToolResult, error) {
    result, err := a.client.CallTool(ctx, a.info.Name, args)
    if err != nil {
        return tool.ToolResult{}, fmt.Errorf("MCP tool %s/%s failed: %w",
            a.client.name, a.info.Name, err)
    }
    return tool.ToolResult{Content: string(result)}, nil
}
```

---

## 4. 生命周期管理

### 4.1 状态机

```text
    ┌──────────┐  Connect()   ┌────────────┐  成功   ┌───────────┐
    │Disconnected│───────────►│ Connecting  │───────►│ Connected │
    └──────────┘             └────────────┘         └───────────┘
         ▲                        │ 失败                  │
         │                        ▼                       │ Close()
         │                   ┌────────┐                   │
         │                   │ Error  │                   │
         │                   └────────┘                   │
         └────────────────────────────────────────────────┘
```

### 4.2 自动重连

```go
// Reconnect 尝试重新连接
func (c *MCPClient) Reconnect(ctx context.Context, maxRetries int) error {
    backoff := time.Second
    for i := 0; i < maxRetries; i++ {
        if err := c.Connect(ctx); err == nil {
            // 重连成功后重新发现 Tool
            if _, err := c.DiscoverTools(ctx); err == nil {
                return nil
            }
        }
        time.Sleep(backoff)
        backoff *= 2  // 指数退避
    }
    return fmt.Errorf("reconnect failed after %d retries", maxRetries)
}
```

### 4.3 生命周期事件

| 事件 | 触发时机 | 日志级别 |
|------|----------|----------|
| `mcp.client.connecting` | 开始连接 | INFO |
| `mcp.client.connected` | 连接成功 | INFO |
| `mcp.client.disconnected` | 连接断开 | WARN |
| `mcp.client.reconnecting` | 自动重连 | INFO |
| `mcp.client.error` | 连接/调用错误 | ERROR |
| `mcp.client.tools_discovered` | Tool 发现完成 | DEBUG |

---

## 5. 多 Server 管理

### 5.1 MCPManager 管理多个 Client

```go
// MCPManager 管理所有 MCP Client 连接
type Manager struct {
    clients map[string]*MCPClient  // name → client
    toolMgr *tool.Manager          // Tool 注册表引用
    config  []MCPServerConfig      // 配置
    mu      sync.RWMutex
}

// StartAll 启动所有配置的 MCP Server 连接
func (m *Manager) StartAll(ctx context.Context) error {
    for _, cfg := range m.config {
        client := NewMCPClient(cfg)
        if err := client.Connect(ctx); err != nil {
            log.Warn("MCP server connect failed", "name", cfg.Name, "err", err)
            continue
        }
        tools, _ := client.DiscoverTools(ctx)
        yaaTools := client.MapToYaaTools()
        m.toolMgr.Register(yaaTools...)
        m.clients[cfg.Name] = client
        log.Info("MCP server connected", "name", cfg.Name, "tools", len(tools))
    }
    return nil
}
```

### 5.2 配置示例

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
      transport: "stdio"
      timeout: 30s
      auto_start: true

    - name: "github"
      url: "https://mcp.github.com/sse"
      transport: "sse"
      timeout: 60s
      auto_start: true

    - name: "custom-ws"
      url: "ws://localhost:3000/mcp"
      transport: "websocket"
      timeout: 30s
      auto_start: false
```

---

## 6. 错误处理

| 错误类型 | 场景 | 处理策略 |
|---------|------|----------|
| `ErrConnectionRefused` | Server 不可达 | 指数退避重连，最多 3 次 |
| `ErrToolNotFound` | Tool 名称不存在 | 返回错误，不重试 |
| `ErrTimeout` | 调用超时 | 返回超时错误，记录日志 |
| `ErrProtocolError` | MCP 协议错误 | 断开连接，标记 Error 状态 |
| `ErrTransportClosed` | 传输层断开 | 触发自动重连 |

```go
var (
    ErrConnectionRefused = errors.New("mcp: connection refused")
    ErrToolNotFound      = errors.New("mcp: tool not found")
    ErrTimeout           = errors.New("mcp: operation timed out")
    ErrProtocolError     = errors.New("mcp: protocol error")
    ErrTransportClosed   = errors.New("mcp: transport closed")
)
```

---

## 7. 与 Tool Manager 集成

MCP Client 发现的 Tool 通过 `MCPToolAdapter` 适配后注册到 Tool Manager，
对 Agent 完全透明。Agent 调用 `mcp.filesystem.read_file` 与调用内置 Tool
的方式完全一致。

```go
// 注册流程
client := NewMCPClient(config)
client.Connect(ctx)
client.DiscoverTools(ctx)
yaaTools := client.MapToYaaTools()
toolManager.Register(yaaTools...)

// Agent 调用时，Tool Manager 自动路由到 MCP Client
result := toolManager.Execute(ctx, "mcp.filesystem.read_file", args)
// → MCPToolAdapter.Execute()
// → MCPClient.CallTool("read_file", args)
// → MCP Server 执行并返回
```

---

*最后更新: 2025-07-17*
