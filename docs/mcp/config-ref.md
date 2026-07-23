# MCP 配置参考

> 本文档详细描述上游 MCP Server 连接和 Yaa! 本地 MCP Server 的配置字段、传输方式及使用示例。
> MCP（Model Context Protocol）是 Yaa! 原生支持的能力扩展协议。

---

## 1. 配置根结构

```go
type MCPConfig struct {
    Servers   []MCPServerConfig   `yaml:"servers"   json:"servers"`
    Server    MCPExposeConfig     `yaml:"server"    json:"server"`
    Timeout   MCPTimeoutConfig    `yaml:"timeout"   json:"timeout"`
    Reconnect MCPReconnectConfig  `yaml:"reconnect" json:"reconnect"`
}

type MCPTimeoutConfig struct {
    Connect time.Duration `yaml:"connect" json:"connect"`
    Init    time.Duration `yaml:"init"    json:"init"`
    Tool    time.Duration `yaml:"tool"    json:"tool"`
}

type MCPReconnectConfig struct {
    Enabled      bool          `yaml:"enabled"       json:"enabled"`
    MaxAttempts  int           `yaml:"max_attempts"  json:"max_attempts"`
    InitialDelay time.Duration `yaml:"initial_delay" json:"initial_delay"`
    MaxDelay     time.Duration `yaml:"max_delay"     json:"max_delay"`
}

type MCPExposeConfig struct {
    Enabled        bool     `yaml:"enabled"        json:"enabled"`
    AgentID        string   `yaml:"agent_id"       json:"agent_id"`
    Transport      string   `yaml:"transport"      json:"transport"`
    Addr           string   `yaml:"addr"           json:"addr"`
    Path           string   `yaml:"path"           json:"path"`
    MessagesPath   string   `yaml:"messages_path"  json:"messages_path"`
    ExposedTools   []string `yaml:"exposed_tools"   json:"exposed_tools"`
    OriginAllowlist []string `yaml:"origin_allowlist" json:"origin_allowlist"`
}
```

| 全局字段 | 默认值 | 说明 |
|----------|--------|------|
| `timeout.connect` | `10s` | transport 建立超时 |
| `timeout.init` | `15s` | initialize 完成超时 |
| `timeout.tool` | `0` | 可选 Tool hard cap；0 表示只使用 Tool Manager/caller deadline |
| `reconnect.enabled` | `true` | 暂时性断线后是否重连 |
| `reconnect.max_attempts` | `3` | 最大重连次数 |
| `reconnect.initial_delay` | `1s` | 首次重连等待 |
| `reconnect.max_delay` | `60s` | 指数退避上限 |

`servers[]` 是 Yaa! 作为 Client 连接的上游 Server；`server` 是 Yaa! 作为 Server 对外暴露的本地监听器。

## 2. 上游连接 `mcp.servers[]`

MCP Server 配置定义在主配置文件的 `mcp.servers` 数组中，每个 Server 支持以下字段：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|:----:|--------|------|
| `name` | `string` | ✅ | — | MCP Server 唯一标识名称，用于注册与引用 |
| `transport` | `string` | ❌ | `stdio` | 传输协议：`stdio` / `sse`（legacy）/ `streamable_http` |
| `command` | `string` | ⚠️ | — | stdio 模式下启动 Server 子进程的命令（如 `npx`、`node`） |
| `args` | `[]string` | ❌ | `[]` | stdio 模式下传递给 command 的参数列表 |
| `url` | `string` | ⚠️ | — | SSE / Streamable HTTP 模式下 MCP Server 的连接地址 |
| `env` | `map[string]string` | ❌ | `{}` | stdio 模式下注入子进程的环境变量 |
| `headers` | `map[string]string` | ❌ | `{}` | HTTP 请求头；值支持 `${VAR}`，读回和日志必须脱敏 |
| `tls.ca_file` | `string` | ❌ | — | 自定义 CA bundle；不提供 `insecure_skip_verify` |
| `timeout` | `duration` | ❌ | `0` | 该 Server 的可选 Tool hard cap；0 继承 `mcp.timeout.tool` |
| `auto_start` | `bool` | ❌ | `true` | Runtime 启动时是否自动连接该 MCP Server |

> **条件必填：** `command` 在 `transport: stdio` 时必填；`url` 在 `transport: sse` 或 `streamable_http` 时必填。`headers`/`tls` 只对网络传输生效。

---

## 3. 传输方式说明

| 传输方式 | 字段要求 | 适用场景 |
|----------|----------|----------|
| `stdio` | `command` + `args` | 本地子进程，如 npx 启动的 npm 包 |
| `sse` | `url` | 旧版 MCP SSE 端点（仅兼容现有 Server） |
| `streamable_http` | `url` | MCP 2025-03-26 Streamable HTTP 端点 |

---

## 4. 本地 MCP Server `mcp.server`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | `bool` | `false` | 是否启动 Yaa! MCP Server |
| `agent_id` | `string` | — | enabled 时必填；执行暴露 Tool 时使用的真实 Agent principal |
| `transport` | `string` | `stdio` | `stdio` / `sse` / `streamable_http` |
| `addr` | `string` | `127.0.0.1:9090` | 网络 transport 的监听地址；必须显式改为非回环地址 |
| `path` | `string` | `/mcp` | Streamable HTTP endpoint |
| `messages_path` | `string` | `/message` | legacy SSE POST endpoint |
| `exposed_tools` | `[]string` | `[]` | 允许暴露的完整 Tool 名称；空列表表示不暴露 |
| `origin_allowlist` | `[]string` | `[]` | Streamable HTTP 精确 Origin 白名单；无 Origin 允许，有 Origin 时必须命中非空列表 |

网络 Server 默认只绑定 loopback；绑定非回环地址时必须配合认证和 TLS/反向代理，并执行 Origin 校验。

```yaml
mcp:
  server:
    enabled: true
    agent_id: "default"
    transport: streamable_http
    addr: "127.0.0.1:9090"
    path: "/mcp"
    exposed_tools: ["file_read"]
    origin_allowlist: ["https://console.example"]
```

## 5. YAML 配置示例

### 5.1 stdio 模式（本地子进程）

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"
      env:
        NODE_ENV: "production"
      timeout: 30s
      auto_start: true

    - name: "sqlite"
      command: "uvx"
      args: ["mcp-server-sqlite", "--db-path", "./data/yaa.db"]
      transport: "stdio"
      timeout: 60s
```

### 5.2 SSE 模式（远程端点）

```yaml
mcp:
  servers:
    - name: "remote-tools"
      transport: "sse"
      url: "http://10.0.0.5:3001/sse"
      timeout: 60s
      auto_start: true
```

### 5.3 Streamable HTTP 模式

```yaml
mcp:
  servers:
    - name: "remote-http"
      transport: "streamable_http"
      url: "https://mcp.example.com/mcp"
      headers:
        Authorization: "Bearer ${MCP_TOKEN}"
      tls:
        ca_file: "/etc/ssl/mcp-ca.pem"
      timeout: 30s
```

### 5.4 多 Server 混合配置

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"

    - name: "remote-search"
      transport: "sse"
      url: "http://search-service.local:8080/sse"
      timeout: 45s

    - name: "debug-tools"
      transport: "stdio"
      command: "node"
      args: ["./tools/mcp-debug-server.js"]
      env:
        LOG_LEVEL: "debug"
      auto_start: false
```

以上非零 `servers[].timeout` 仅演示显式 Tool hard cap；省略或设为 0 时继承 `mcp.timeout.tool`，两者均为 0 时只使用 caller deadline。

---

## 6. Go 配置结构体与校验边界

配置 DTO 统一由 `internal/config` 持有；MCP 模块接收 `config.MCPConfig`，不在 `internal/mcp` 重复声明同名结构体，也不复制基础字段校验。默认值、字段格式、范围、transport 依赖以及 name/`exposed_tools` 唯一性的唯一基础校验见 [`docs/config/validation.md`](../config/validation.md#3-配置校验)。

MCP 构造器仍防御直接调用和资源边界：深拷贝可变配置，拒绝空依赖、重复 Server name、无效 transport 和重复 `exposed_tools`；`NewMCPServer` 还要在绑定 listener 前确认 exposed Tool 当前 enabled、存在且通过 Agent allowlist。这些构造期检查与根 Config Validator 使用相同契约，但不形成第二套配置校验器，也不替代 `Config.Activate(binding)` 的跨模块引用校验。

---

## 7. 加载与启动流程

MCP Manager 是上游生命周期、catalog 和稳定 Proxy 的唯一 owner；`Client` 只代表一代 transport 连接。Manager 不把可变 `Client` 暴露给调用方。

```go
type upstreamEntry struct {
    config        config.MCPServerConfig // 深拷贝，运行期不可变
    handle        *ProxyHandle           // 所有该 Server Proxy 共享
    catalog       []MCPTool              // 已规范化、按 Name 排序
    client        *Client                // 当前代；只在 mu 下读写
    generation    uint64                 // 每次发布新 Client 单调递增
    listChanged   <-chan struct{}        // 当前 Client 的合并通知
    status        ConnectionStatus
    protocol      string
    connectedAt   *time.Time
    lastErr       error                  // 只保存脱敏/typed error
    mu            sync.RWMutex
}

type Manager struct {
    config       config.MCPConfig
    entries      map[string]*upstreamEntry // NewManager 构造后不增不删
    tools        *tool.Manager
    server       *MCPServer
    proxyNames   []string
    runCtx       context.Context
    runCancel    context.CancelFunc
    lifecycleMu  sync.Mutex // 关闭后禁止 wg.Add 和发布新 Client
    stopping     bool
    wg           sync.WaitGroup
    serverDone   chan struct{}
    serverState  string // prepared | serving | unhealthy | stopped
    serverErr    error
    stopOnce     sync.Once
    stopDone     chan struct{}
    stopErr      error
    logger       *slog.Logger
    mu           sync.RWMutex
}

func NewManager(ctx context.Context, cfg config.MCPConfig, tools *tool.Manager, logger *slog.Logger) (*Manager, error)
func (m *Manager) Prepare() error
func (m *Manager) Activate() error
func (m *Manager) Stop(ctx context.Context) error
func (m *Manager) Done() <-chan struct{}
func (m *Manager) Get(name string) (ServerStatus, bool)
func (m *Manager) Tools(name string) ([]tool.ToolInfo, bool)
func (m *Manager) List() []ServerStatus
func (m *Manager) Ready() bool
```

`NewManager` 只创建长期 `runCtx`、`stopDone` 和每个配置 Server 的 `upstreamEntry`；它深拷贝 map/slice，拒绝空依赖、重复 name 和无效 transport，不发起 I/O。每个 entry 在此创建一个永久 `ProxyHandle`，因此 `List` 覆盖所有配置项（包括 `auto_start: false`）。MCP Server name 必须是 `^[a-z0-9][a-z0-9-]{0,63}$`；该限制保证 canonical `mcp.<server>.<remote>` 的长度上限可验证。`Get`/`Tools` 返回快照/深拷贝，不返回当前 `Client` 指针。

### 7.1 Prepare 与 Activate

`Prepare` 在 `lifecycleMu` 下登记一个 `wg` owner；停止 gate 已关闭时立即返回 `context.Canceled`。流程固定为：

1. 若启用本地 Server，调用 `NewMCPServer` 持有 listener，并在 gate 下发布 `serverState=prepared`；失败直接返回。
2. 按 Server name 排序遍历 entries。`auto_start=false` 保持 `disconnected`，不注册 Proxy。
3. 对 auto-start entry 创建一代 Client，在 `timeout.connect` 内 `Start`，再在 `timeout.init` 内完成 initialize、initialized 和完整分页 `tools/list`。初始连接失败按 `reconnect` 预算有限尝试；耗尽后只记录 `error`，不在 Runtime Ready 后首次发布 Proxy。
4. 对完整 catalog 执行一次事务化注册：所有 Proxy 只引用该 entry 的同一个 handle；任一 name/schema 冲突都注销本批已注册项、关闭 Client 并返回启动错误。
5. 重新取得 `lifecycleMu`；若已 stopping，回滚本批并关闭 Client，不发布任何 handle。否则在 entry 锁下同时写入 `client/catalog/status/protocol/connectedAt`，随后 `handle.Store(client)`，再把 Proxy names 加入 `proxyNames`。
6. 只有发布成功后才在生命周期 gate 内 `wg.Add(1)` 并启动该 entry 唯一的 `runUpstream` goroutine。

`Activate` 只能在唯一的 `Config.Activate(binding)` 成功后调用一次。它在 gate 下读取 prepared Server、创建继承 `runCtx` 的 Serve context 和 `serverDone`，登记一个 `wg` owner 后启动阻塞 `Serve`。非取消退出写入 `unhealthy`/脱敏错误并使 `Ready()` 为 false；`Stop` 造成的退出不是故障。

### 7.2 heartbeat、reconciliation 与重连

`runUpstream` 是每个 entry 唯一的重连 owner，Client 不包含 heartbeat 或 backoff。它使用固定 `heartbeatInterval=30s`、`heartbeatTimeout=10s`，并同时等待当前 Client 的 `Done()`、该代独有的 `listChanged` channel、ticker 和 `runCtx.Done()`：

- 每代 Client 的 `Done()` 只关闭一次，是无损 disconnect 通知；`listChanged` 仅允许合并重复通知。旧代 channel 永远不会被新代复用，因此迟到 callback 不能触发新一代重连。
- `list_changed` 先按当前 generation 清空 handle，再用当前 Client 完整执行 `DiscoverTools`，并按规范化后的 name、description、schema 集合精确比较；相同、该 generation 仍存活且未 stopping 才重新发布同一 Client，差异则记录 `ErrMCPProtocolError`、关闭该 Client并保持 `error`，要求 Runtime 重启。它不能动态新增、删除或改写 Tool Proxy。
- ticker 调用 `Ping`；Ping/Recv/disconnect 的暂时错误先用 entry 当前 `generation` 做 compare-and-clear，将 handle 置 nil、状态置 `error`，再在锁外关闭旧 Client，按 `initial_delay` 的指数退避创建新 Client。
- 新 Client 必须重新 initialize 和完整发现；catalog 与冻结快照精确相等后，取得 `lifecycleMu` 检查 `stopping`，再在 entry 锁下递增 generation、发布 `client/listChanged` 和 `handle.Store(newClient)`。停止、代际不匹配或 catalog 漂移都在锁外 `Close` 新 Client，禁止发布。
- 每个 entry 只允许该一个 goroutine 重连；达到 `max_attempts` 后保持 `error` 和 unavailable，直到下次 Runtime 启动。任何已经发送的 `tools/call` 都不 replay。

`Client.Close` 只关闭它自己的 transport/dispatcher；Manager 在永久停止或代际替换时恰好调用一次。`mcp.timeout.tool` 与 `servers[].timeout` 都是可选 hard cap：有效值为非零的 server 值，否则取全局值；若有效值仍为 0，则 Proxy 只使用 Tool Manager 传入的 caller deadline。非零时取 caller deadline 与该 cap 的较早者，caller cancellation cause 优先。

### 7.3 幂等停止

```go
func (m *Manager) Stop(ctx context.Context) error {
    m.stopOnce.Do(func() {
        snapshot := m.beginStop() // 在 gate 下 stopping、cancel、清空 handle并快照资源
        go func() {
            m.stopErr = m.teardown(snapshot)
            close(m.stopDone)
        }()
    })

    // Done 已关闭时优先返回最终结果，即使传入的 ctx 也已结束。
    select {
    case <-m.stopDone:
        return m.stopErr
    default:
    }
    select {
    case <-m.stopDone:
        return m.stopErr
    case <-ctx.Done():
        return context.Cause(ctx) // teardown 仍在后台继续
    }
}

func (m *Manager) Done() <-chan struct{} { return m.stopDone }
```

`beginStop` 在 `lifecycleMu` 下关闭启动/发布 gate、取消 `runCtx`，再逐 entry 在锁下清空所有 `ProxyHandle`（调用立即得到 `ErrMCPUnavailable`），并快照本地 Server、当前 Client和 Proxy names；它释放全部锁后才返回 snapshot。`teardown` 随后关闭 snapshot 中的 Server/Client，等待 `wg`、已经创建且非 nil 的 `serverDone` 和所有 Client dispatcher 退出，再按注册逆序 `tools.Unregister`，清空 `proxyNames`，把所有 entry 标记 `disconnected`、本地 Server 标记 `stopped`，以 `errors.Join` 聚合全部错误。未启用本地 Server或 Prepare 后、Activate 前 rollback 时 `serverDone=nil`，不得等待该 channel。关闭、取消和等待不持有 entry/Manager 锁，单个错误不能跳过其他 entry 或 cleanup。`Done()` 只在这套 teardown 完成后关闭；若首次 `Stop(ctx)` 因 caller deadline 返回，Runtime 必须先等待 `Done()`，再调用一次 `Stop(context.Background())` 取得缓存的最终聚合错误，之后才能关闭 Tool Manager 等依赖资源。顶部 fast path 保证 Done 已关闭时最终结果优先于 caller context。

`Ready()` 在本地 Server 未启用时返回 true；启用时只在 `serving` 返回 true。Manager 不提供运行期 Register/Connect/Restart API；`mcp.*` 结构变化统一报告 `restart_required`。

---

## 8. 配置注意事项

| 注意点 | 说明 |
|--------|------|
| **名称唯一性** | 每个 Server 的 `name` 必须唯一，重复名称会导致启动失败 |
| **超时设置** | 握手较慢时调整 `timeout.connect/init`；Tool hard cap 默认 0，只使用 caller deadline |
| **auto_start** | 设为 `false` 时保持 disconnected；修改该字段需要重启 Runtime |
| **环境变量** | `env` 仅对 stdio 模式有效，不会影响远程 HTTP 端点 |
| **敏感信息** | API Key 等敏感信息应通过 `${VAR_NAME}` 引用环境变量，而非明文写入配置 |
| **路径分隔符** | Windows 环境下 `args` 中的路径使用反斜杠 `\` 时需注意 YAML 转义 |

---

*最后更新: 2025-07-17*
