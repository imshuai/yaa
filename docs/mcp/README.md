# MCP 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/mcp/` (原计划单文件 `docs/mcp.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.9, `docs/tool/` 全系列

---

## 1. 概述

### 1.1 什么是 MCP

MCP (Model Context Protocol) 是一种开放协议，用于 LLM/Agent 与外部工具、数据源之间的标准化通信。Yaa! 原生支持 MCP，无需第三方桥接。

### 1.2 双角色设计

Yaa! 在 MCP 生态中同时扮演两个角色：

| 角色 | 说明 | 方向 |
|------|------|------|
| **MCP Client** | 连接外部 MCP Server，将其暴露的 Tool 注册为 Yaa! Tool 使用 | Yaa! → 外部 |
| **MCP Server** | 将 Yaa! 自身的 Tool / Skill 通过 MCP 协议暴露给其他 MCP Client | 外部 → Yaa! |

```text
                         ┌─────────────────┐
                         │   External      │
                         │   MCP Client    │
                         │   (Claude /     │
                         │    Cursor /     │
                         │    其他 Agent)   │
                         └────────┬────────┘
                                  │
                          MCP 协议  │ stdio / SSE / WS
                                  ▼
┌─────────────────────────────────────────────────────────┐
│                    Yaa! Runtime                          │
│                                                         │
│   ┌─────────────────────────────────────────────────┐   │
│   │              MCP Manager                         │   │
│   │                                                  │   │
│   │   ┌──────────────┐         ┌──────────────┐     │   │
│   │   │  MCP Client  │         │  MCP Server   │     │   │
│   │   │  (连接外部    │         │  (暴露 Yaa!   │     │   │
│   │   │   Server)    │         │   能力)       │     │   │
│   │   └──────┬───────┘         └───────┬──────┘     │   │
│   │          │                        │             │   │
│   │          ▼                        ▼             │   │
│   │   Tool Registry            Tool / Skill         │   │
│   │   (外部 Tool 注册)          (内置能力)           │   │
│   └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
         │
         │ MCP 协议
         │ stdio / SSE / WS
         ▼
   ┌──────────────────┐
   │  External MCP     │
   │  Server           │
   │  (filesystem /    │
   │   github / db)    │
   └──────────────────┘
```

### 1.3 核心原则

1. **Native MCP** — 无需插件，MCP 支持内置于 Runtime
2. **双向互通** — 既是 Client 也是 Server，无缝融入 MCP 生态
3. **Tool 透明** — 外部 MCP Tool 注册后与内置 Tool 无差异
4. **Config over Code** — 通过 `mcp.servers` 配置连接外部 Server
5. **传输无关** — 支持 stdio、SSE、WebSocket 三种传输方式

---

## 2. 核心接口

### 2.1 MCP Manager

MCP Manager 是 MCP 子系统的入口，统一管理所有 MCP Client 连接和 MCP Server 实例。

```go
// MCP Manager 管理所有 MCP 连接（Client + Server）
type Manager struct {
    Clients  map[string]*Client   // 已连接的外部 MCP Server（Yaa! 作为 Client）
    Servers  map[string]*Server   // Yaa! 暴露的 MCP Server 实例
    Tools    *tool.Manager        // Tool 注册中心引用
    Config   *MCPConfig
}

// 初始化：加载配置中的 mcp.servers，建立连接并注册 Tool
func (m *Manager) Init(cfg *MCPConfig) error

// 启动所有 MCP Server 实例
func (m *Manager) StartServers() error

// 优雅关闭所有连接
func (m *Manager) Shutdown(ctx context.Context) error
```

### 2.2 MCP Client

MCP Client 代表 Yaa! 到一个外部 MCP Server 的连接。

```go
// Client 连接外部 MCP Server，将其 Tool 注册到 Yaa! Tool Registry
type Client struct {
    Name      string             // 连接名称（配置中的 name）
    Transport Transport          // 传输层（stdio / SSE / WS）
    Tools     []MCPTool          // 远端暴露的 Tool 列表
    State     ClientState        // Disconnected / Connecting / Ready / Error
}

// 建立连接并发现远端 Tool
func (c *Client) Connect(ctx context.Context) error

// 列出远端可用 Tool
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error)

// 调用远端 Tool
func (c *Client) CallTool(ctx context.Context, name string, params map[string]any) (json.RawMessage, error)

// 断开连接
func (c *Client) Disconnect() error
```

MCP Client 发现的 Tool 会自动注册到 Yaa! 的 Tool Registry，命名格式为 `mcp.{server_name}.{tool_name}`，与内置 Tool 一样参与 Agent 的 Tool Loop。

### 2.3 MCP Server

MCP Server 将 Yaa! 的内置 Tool 和 Skill 通过 MCP 协议暴露给外部 Client。

```go
// Server 将 Yaa! 能力暴露为 MCP Server
type Server struct {
    Name      string             // Server 名称
    Transport Transport          // 传输层（stdio / SSE / WS）
    Tools     []tool.Tool        // 暴露的 Tool 列表
    Skills    []skill.Skill      // 暴露的 Skill 列表
    State     ServerState        // Stopped / Running / Error
}

// 启动 MCP Server，监听外部 Client 连接
func (s *Server) Start(ctx context.Context) error

// 处理来自外部 Client 的 Tool 调用
func (s *Server) handleToolCall(ctx context.Context, name string, params map[string]any) (json.RawMessage, error)

// 停止 Server
func (s *Server) Stop() error
```

---

## 3. 传输方式

### 3.1 传输方式对比

| 传输方式 | 适用场景 | 优势 | 限制 |
|----------|----------|------|------|
| **stdio** | 本地进程通信 | 零配置、低延迟、最简单 | 仅限同机，需启动子进程 |
| **SSE** | 远程单向调用 | HTTP 兼容、穿透防火墙 | 不支持双向实时通信 |
| **WebSocket** | 远程双向通信 | 全双工、实时性强 | 需 WS 兼容环境 |

### 3.2 传输接口

```go
// Transport 抽象所有传输方式
type Transport interface {
    // Start 启动传输层，建立连接
    Start(ctx context.Context) error

    // 发送消息
    Send(ctx context.Context, msg *Message) error

    // 接收消息
    Recv(ctx context.Context) (*Message, error)

    // 关闭连接
    Close() error

    // Info 返回传输层元信息
    Info() TransportInfo
}

// 三种实现
type StdioTransport struct {
    Cmd    *exec.Cmd       // 子进程
    Stdin  io.WriteCloser
    Stdout io.ReadCloser
}

type SSETransport struct {
    URL    string           // Server SSE 端点
    Client *http.Client
}

type WebSocketTransport struct {
    URL    string           // WS 端点
    Conn   *websocket.Conn
}
```

---

## 4. 配置参考

```yaml
# yaa.yaml — MCP 配置示例
mcp:
  # 作为 MCP Client：连接外部 MCP Server
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"

    - name: "github"
      transport: "sse"
      url: "http://localhost:3001/sse"

    - name: "remote-tools"
      transport: "websocket"
      url: "ws://10.0.0.5:8090/mcp"

  # 作为 MCP Server：暴露 Yaa! 能力
  expose:
    enabled: true
    transport: "sse"
    addr: ":9090"
    tools: ["shell", "http", "file"]   # 暴露哪些 Tool
    skills: ["web-scraper"]            # 暴露哪些 Skill
```

配置加载后，MCP Manager 会：
1. 逐个连接 `mcp.servers` 中的外部 Server
2. 发现远端 Tool 并注册到 Tool Registry（前缀 `mcp.{name}.`）
3. 如果 `mcp.expose.enabled` 为 true，启动 MCP Server 实例

---

## 5. 数据流

### 5.1 MCP Client 调用流程

```text
Agent Tool Loop
  │
  │  LLM 返回 Tool Call: mcp.filesystem.read_file
  │
  ▼
Tool Registry
  │
  │  查找 Tool → 发现是 MCP Tool
  │
  ▼
MCP Manager → Client ("filesystem")
  │
  │  通过 Transport 发送 MCP 请求
  │
  ▼
External MCP Server (stdio / SSE / WS)
  │
  │  执行 Tool，返回结果
  │
  ▼
MCP Client → Tool Registry → Context Manager
  │
  │  Tool 结果加入上下文
  │
  ▼
Agent → 再次调用 LLM
```

### 5.2 MCP Server 响应流程

```text
External MCP Client
  │
  │  MCP 请求 (stdio / SSE / WS)
  │
  ▼
MCP Server (Yaa!)
  │
  │  解析请求 → 路由到对应 Tool / Skill
  │
  ▼
Tool / Skill 执行
  │
  │  返回结果
  │
  ▼
MCP Server → Transport
  │
  │  MCP 响应
  │
  ▼
External MCP Client
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [client.md](client.md) | MCP Client — 连接外部 Server、Tool 发现、调用转发、重连策略 |
| [server.md](server.md) | MCP Server — 暴露 Yaa! 能力、Tool/Skill 映射、会话管理 |
| [transport.md](transport.md) | 传输层 — stdio / SSE / WebSocket 实现、连接生命周期 |
| [integration.md](integration.md) | 与 Tool / Agent / Config 的集成 |
| [config-ref.md](config-ref.md) | 配置参考 — mcp.servers / mcp.expose 完整字段说明 |
| [errors.md](errors.md) | 错误处理 — 连接失败、超时、重连、降级策略 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策（MC-001 ~ MC-008）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-17*
