# MCP 系统设计

> 文档路径: `docs/mcp/README.md`
> 依赖: `docs/architecture.md` §3.9、`docs/tool/` 全系列

---

## 1. 范围

Yaa! 同时作为 MCP Client 和 MCP Server。MVP 只实现 Tool 能力；首选 MCP `2025-03-26`，并为 legacy SSE 接受 `2024-11-05`。Resource 与 Prompt 留待后续版本。

```text
Agent → Tool Manager → MCP Tool Proxy → MCP Client → External MCP Server

External MCP Client → Yaa! MCP Server → Tool Manager → Yaa! Tool
```

MCP 与 Yaa! Remote API 是两套协议：Remote API 可使用 WebSocket；MCP transport 只使用 `stdio`、`streamable_http` 和兼容旧 Server 的 `sse`。

## 2. Manager

```go
type ServerStatus struct {
    Name            string           `json:"name"`
    Status          ConnectionStatus `json:"status"`
    Transport       string           `json:"transport"`
    ProtocolVersion *string          `json:"protocol_version"`
    ToolCount       int              `json:"tool_count"`
    ConnectedAt     *time.Time       `json:"connected_at"`
    LastError       string           `json:"last_error,omitempty"`
}

func (m *Manager) Prepare() error
func (m *Manager) Activate() error
func (m *Manager) Stop(ctx context.Context) error
func (m *Manager) Done() <-chan struct{}
func (m *Manager) Ready() bool
func (m *Manager) Get(name string) (ServerStatus, bool)
func (m *Manager) Tools(name string) ([]tool.ToolInfo, bool)
func (m *Manager) List() []ServerStatus
```

完整 Manager owner 字段只在 [config-ref.md](config-ref.md#7-加载与启动流程) 定义。Manager 是 catalog、稳定 Proxy、heartbeat 和重连的唯一 owner，不向调用方暴露可变 `Client`。`Prepare` 校验并持有本地 transport，但不接收请求；同时连接 auto-start 上游、发现 Tool 并注册稳定 Proxy。`Config.Activate` 完成 binding 校验后，Runtime 才调用 `Activate` 启动本地 `Serve`。外部 Server 连接失败只把该连接标记为 `error`；若配置实际引用了未注册的 MCP Tool，后续 binding 校验仍会拒绝启动。显式启用的本地 Server 若配置、listener、Agent principal 或 exposed Tool 无效则启动失败。`Ready` 在本地 `Serve` 意外退出后返回 false，使 Runtime 进入 unhealthy/Not Ready。`ServerStatus` 是 Manager、健康快照与 Remote 投影共用的唯一上游状态类型；敏感连接配置不进入该类型。`Stop(ctx)` 可以因 caller deadline 先返回；Runtime 必须等待 `Done()`，再用 `Stop(context.Background())` 读取缓存的最终 teardown error，之后才能关闭 Tool Manager 等依赖。

## 3. Client

```go
func (c *Client) Connect(ctx context.Context) error
func (c *Client) Initialize(ctx context.Context) error
func (c *Client) DiscoverTools(ctx context.Context) ([]MCPTool, error)
func (c *Client) Ping(ctx context.Context) error
func (c *Client) Done() <-chan struct{}
func (c *Client) Err() error
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error)
func (c *Client) Close() error
```

Client 的唯一完整结构见 [client.md](client.md#1-状态与结构)。每个 Client 只代表一代连接；`Done`/`Err` 把终止状态交给 Manager，Client 自己不重连。连接状态只有 `disconnected`、`connecting`、`connected`、`error`。Initialize 请求和响应都必须协商 `protocolVersion`。`streamable_http` 只接受 `2025-03-26`，legacy `sse` 只接受 `2024-11-05`，`stdio` 首选 `2025-03-26` 并接受这两个版本。

Tool 映射到 Yaa! 后统一命名为：

```text
mcp.<server_name>.<tool_name>
```

## 4. Server

Yaa! MCP Server 通过独立 `ServerTransport` 接收 JSON-RPC，只声明实际实现的 Tool capability。

| 方法 | MVP 行为 |
|------|----------|
| `initialize` | 按 transport 协商 `2025-03-26` 或 legacy `2024-11-05`，返回 Tool capability |
| `notifications/initialized` | 标记连接就绪 |
| `tools/list` | 返回允许暴露的 Tool，支持 cursor |
| `tools/call` | 校验名称和参数后调用 Tool Manager |
| `ping` | 返回空 result |
| `resources/*`, `prompts/*` | 返回 `-32601 Method not found` |

可通过 `mcp.server.exposed_tools` 配置公开 Tool；默认空列表，不暴露任何内部 Tool。

## 5. Transport

| transport | 角色 | 说明 |
|-----------|------|------|
| `stdio` | Client/Server | 本地子进程或标准输入输出 |
| `streamable_http` | Client/Server | MCP 2025-03-26 标准 HTTP transport |
| `sse` | Client/Server | MCP 2024-11-05 legacy 兼容 |

Client 和 Server transport 接口、SSE frame 与 Streamable HTTP session header 见 [transport.md](transport.md)。

## 6. 配置

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      auto_start: true
    - name: remote-search
      transport: streamable_http
      url: https://mcp.example.com/mcp
      headers:
        Authorization: "Bearer ${MCP_TOKEN}"
      auto_start: true
  timeout:
    connect: 10s
    init: 15s
    tool: 0
  reconnect:
    enabled: true
    max_attempts: 3
    initial_delay: 1s
    max_delay: 60s
  server:
    enabled: false
    agent_id: "default"
    transport: stdio
    addr: "127.0.0.1:9090"
    path: "/mcp"
    exposed_tools: []
```

完整字段见 [config-ref.md](config-ref.md)。

## 7. 错误语义

- 未知 JSON-RPC method：JSON-RPC `-32601`。
- 未知 Tool 或参数无效：JSON-RPC `-32602`。
- Tool 已被正确调用但执行失败：返回 `result.isError=true` 和安全的文本内容。
- Transport/协议错误：返回 `ErrMCP*`，连接进入 `error`。
- 连接断开后不自动重放任何已经发送的 `tools/call` 请求。
- 已注册的 Proxy 在暂时断线时保留并返回 `ErrMCPUnavailable`；只在 Manager 永久关闭时注销。

## 8. 启动顺序

完整 Runtime 顺序中 Plugin 和 MCP 都在 Skill Load/binding 之前完成 Tool Proxy 注册：

```text
Config Validate
  → Tool Manager
  → MCP Manager.Prepare
  → 为 auto_start Server 建立连接
  → initialize 版本协商
  → 分页拉取 tools/list
  → 注册稳定 MCP Tool Proxy
  → Skill Load
  → Config.Activate(binding)
  → MCP Manager.Activate（本地 Serve）
```

## 文档索引

| 文件 | 内容 |
|------|------|
| [client.md](client.md) | MCP Client、初始化、Tool 发现与调用 |
| [server.md](server.md) | MCP Server 方法与暴露策略 |
| [transport.md](transport.md) | Client/Server transport 与 wire 约定 |
| [integration.md](integration.md) | Tool/Agent/Config 集成 |
| [config-ref.md](config-ref.md) | 完整配置字段 |
| [errors.md](errors.md) | 错误、重试和降级 |
| [observability.md](observability.md) | 日志、指标与事件 |
| [decisions.md](decisions.md) | 设计决策 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-17*
