# MCP 传输层设计

> 文档路径: `docs/mcp/transport.md`
> 上级: [`README.md`](README.md)
> 依赖: [`decisions.md`](decisions.md) MC-002、MCP 协议版本 `2025-03-26` 与 legacy `2024-11-05`

---

## 1. 传输枚举

MCP Client/Server 的 `transport` 只接受以下值：

| 值 | 状态 | 用途 |
|----|------|------|
| `stdio` | MVP | 本地子进程，JSON-RPC 消息按行传输 |
| `sse` | legacy | MCP 2024-11-05 兼容传输：GET 事件流 + POST 消息 |
| `streamable_http` | MVP | MCP 2025-03-26 Streamable HTTP |

`ws`、`websocket` 不是 MCP transport 值。Yaa! Remote API 自身仍支持 WebSocket，但它与 MCP 协议分开。

## 2. 协议版本与 JSON-RPC 消息

MVP 支持 `2025-03-26` 和 legacy `2024-11-05`。版本与 transport 的唯一矩阵为：Streamable HTTP 只使用 `2025-03-26`，legacy SSE 只使用 `2024-11-05`，stdio 发送 `2025-03-26` 并接受这两个版本。Server 在正常 `InitializeResult` 中返回该 transport 允许的版本；Client 若不支持返回值，关闭连接并返回 `ErrMCPProtocolError`。版本差异本身不是 JSON-RPC error。

```go
type Message struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id,omitempty"` // string 或 number，通知无 ID
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"` // 仅内部诊断，响应前脱敏
}

func (e *RPCError) Error() string { return "mcp rpc error" }

type Implementation struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

type InitializeParams struct {
    ProtocolVersion string         `json:"protocolVersion"`
    Capabilities    map[string]any `json:"capabilities"`
    ClientInfo      Implementation `json:"clientInfo"`
}

type InitializeResult struct {
    ProtocolVersion string         `json:"protocolVersion"`
    Capabilities    map[string]any `json:"capabilities"`
    ServerInfo      Implementation `json:"serverInfo"`
}

type MCPTool struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    InputSchema json.RawMessage `json:"inputSchema"`
}

type ListToolsParams struct {
    Cursor string `json:"cursor,omitempty"`
}

type ListToolsResult struct {
    Tools      []MCPTool `json:"tools"`
    NextCursor string    `json:"nextCursor,omitempty"`
}

type CallToolParams struct {
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments,omitempty"`
}

type Content struct {
    Type string `json:"type"` // v1 只接受 text
    Text string `json:"text"`
}

type CallToolResult struct {
    Content []Content `json:"content"`
    IsError bool      `json:"isError,omitempty"`
}

type CallToolRequest = CallToolParams
```

`Message` 只是 wire DTO；任何 handler 或 pending 分发前必须调用同一个 `validateEnvelope`，严格分类且不允许字段混用：

| kind | 必须存在 | 必须不存在 |
|------|----------|------------|
| request | `jsonrpc="2.0"`、非空 method、非 null string/number ID | result、error |
| notification | `jsonrpc="2.0"`、非空 method | ID、result、error |
| response | `jsonrpc="2.0"`、非 null string/number ID、result/error 恰有一个 | method、params |

空 ID、`null` ID、batch array、同时含 result/error、响应携带 method、请求携带 result/error 都是 `ErrMCPProtocolError`。Client 只签发 `1..math.MaxUint64` 的 JSON number ID，因此对应 response 也必须回显同类正整数；Server 仍按 JSON-RPC 接受 string 或 number request ID并逐字节回显。notification 永不产生 JSON-RPC response。Client 收到 Server request 时只处理 `ping`，其他 method 返回 `-32601`；它们不得进入业务 request 的 pending map。

v1 使用固定 wire 上限，不增加配置面：

| 对象 | 上限 |
|------|------|
| 单个 JSON-RPC body、stdio line、SSE frame | 4 MiB |
| `RPCError.data` | 16 KiB |
| 单个 Tool description | 4 KiB UTF-8 |
| 单个 input schema | 256 KiB JSON |
| 单次 `tools/list` 总 Tool 数 / page 数 | 4096 / 128 |
| opaque cursor | 4 KiB |
| 单连接待处理 request | 1024 |

HTTP Server request body 使用 `http.MaxBytesReader`；HTTP Client response、stdio 和 SSE 在分配完整 buffer 前使用有界 reader/decoder。超过上限立即关闭该连接并返回 `ErrMCPProtocolError`。schema 必须严格解码为单个 JSON object；用 `json.Decoder.UseNumber` + EOF 检查后重新 `json.Marshal` 得到比较快照。重连时比较规范化后的 Tool name、description、schema 集合，不比较分页或对象 key 顺序。原始 error message/data 只保留在 16 KiB 的内部诊断对象中，稳定 `Error()` 不包含它们。

## 3. ClientTransport

Client transport 负责拨号、发送和接收 JSON-RPC 消息；它不提供 `Serve` 或监听语义。

```go
type ClientTransport interface {
    Start(ctx context.Context) error
    Send(ctx context.Context, msg *Message) error
    Recv(ctx context.Context) (*Message, error)
    Close() error
    Info() TransportInfo
}

type TransportInfo struct {
    Type      string // stdio / sse / streamable_http
    Endpoint  string
    Connected bool
}
```

`Recv` 在每个 ClientTransport 实例上只有一个调用者（Client 的 dispatcher goroutine）；`Send` 可以并发。Transport 必须把所有 response/notification 原样送入 Recv，不自行匹配 request ID；streamable HTTP 的同步 POST response、SSE message event和 stdio 行都进入同一条接收流。已经发送的请求不能由 transport 重试。

### 3.1 stdio

```go
func NewStdioClient(command string, args []string, env map[string]string) *StdioClient
```

`Start` 启动子进程并连接 stdin/stdout；每条 JSON-RPC 消息独占一行，stderr 只转发到 Runtime 日志，不混入协议流。`Close` 先关闭 stdin，再等待进程退出，超时后 Kill。

### 3.2 legacy SSE

```go
func NewSSEClient(url string, httpClient *http.Client) *SSEClient
```

建立 `GET url`，请求头包含 `Accept: text/event-stream`。首个事件必须按 SSE frame 解析：

```text
event: endpoint
data: /message?session_id=abc

```

`data` 使用 `url.Parse` 后由原始 SSE URL 的 `ResolveReference` 解析，保留正确的 base path、query、scheme 和 host，作为后续 POST 地址；拒绝解析后跨 host/scheme 的 endpoint。消息事件格式为：

```text
id: 42
event: message
data: {"jsonrpc":"2.0","id":1,"result":{}}

```

解析器必须支持多行 `data:`、空行结束 frame、heartbeat comment（`: ping`）和 `Last-Event-ID`。重连时只携带最后收到的事件 ID；任何已经发送的 Tool 请求都不自动重放。

### 3.3 Streamable HTTP

```go
func NewStreamableHTTPClient(url string, httpClient *http.Client) *StreamableHTTPClient
```

每个 JSON-RPC 请求通过 HTTP POST 发送；初始化请求头至少包含：

```text
Accept: application/json, text/event-stream
Content-Type: application/json
```

含 request 的 POST 返回单个 JSON 响应或 `text/event-stream`；只含 notification/response 的 POST 成功时返回 `202 Accepted` 和空 body。Server 可以在 initialize 响应中返回 `Mcp-Session-Id`；收到后 Client 必须在该会话的后续 POST/GET/DELETE 中携带同名 header，未收到则保持 stateless 且不发送 GET/DELETE。Yaa! 自己的 Server 固定创建 session，但通用 Client 不能要求上游一定返回该 header。客户端以 JSON-RPC `id` 关联响应。Transport 不自动重试任何已经发送的 POST；连接管理器只能在新连接上重新执行 initialize 和 `tools/list`。

收到 session ID 后，GET 可打开可选的 Server-to-Client SSE 流；服务端不提供该流时返回 `405 Method Not Allowed`。DELETE 带 `Mcp-Session-Id` 终止会话，成功返回 `200 OK` 或 `204 No Content`；未知/失效会话返回 `404 Not Found`，客户端需要重新 initialize。stateless Client 不发送 GET/DELETE。每个 HTTP body 只允许一个 JSON-RPC message；数组/batch 返回 HTTP 400 和 JSON-RPC `-32600`。连接恢复后不得重发任何已经发送的 POST `tools/call`。

Client 使用 `http.Client.CheckRedirect` 拒绝所有 3xx，防止 endpoint 或 Authorization 被带到其他地址。先检查 caller context；已结束时始终返回 `context.Cause(ctx)`。其余 HTTP 状态只按下表映射，错误 body 最多有界丢弃 16 KiB，不进入稳定错误或日志：

| 阶段 / 状态 | Runtime error / 行为 |
|-------------|----------------------|
| 任意 `401` / `403` | `ErrMCPAuthFailed`，永久错误 |
| initialize `404` / `405` | `ErrMCPConfig`，endpoint 不支持该 transport |
| initialize `408` / `504` | `ErrMCPConnTimeout`，可按连接预算重试 |
| initialize `429` / `5xx` | `ErrMCPUnavailable`，可按连接预算重试 |
| 已分配 session 后 POST `400` / `404` / `410` | `ErrMCPTransportClosed`；只为未来调用重新 initialize |
| 已发送业务 POST 后 `408` / `429` / `5xx` | `ErrMCPTransportWrite`；结果不确定，原请求不 replay |
| `413`、3xx、错误 Content-Type、超限或非法 body | `ErrMCPProtocolError`，关闭连接 |
| 可选 GET `405` | 只关闭 Server-to-Client SSE；POST transport 仍可用 |
| Close 的 DELETE `404` / `405` | 幂等忽略 |
| 其他非预期非 2xx | `ErrMCPProtocolError` |

## 4. ServerTransport

Server transport 负责监听并把收到的消息交给 handler；它与 ClientTransport 类型分离。

```go
type ServerSessionState string

const (
    SessionNew        ServerSessionState = "new"
    SessionNegotiated ServerSessionState = "negotiated"
    SessionReady      ServerSessionState = "ready"
    SessionClosed     ServerSessionState = "closed"
)

type ServerSession struct {
    ID              string
    Transport       string
    ProtocolVersion string
    State           ServerSessionState
    mu              sync.RWMutex
}

func (s *ServerSession) Negotiate(version string) error
func (s *ServerSession) MarkInitialized() error
func (s *ServerSession) CanPing() bool
func (s *ServerSession) Ready() bool

type ServerHandler func(ctx context.Context, session *ServerSession, msg *Message) (*Message, error)

type ServerTransport interface {
    Serve(ctx context.Context, handler ServerHandler) error
    Close() error
    Info() TransportInfo
}

func NewStdioServer() *StdioServer
func NewSSEServer(listener net.Listener, endpointPath, messagesPath string) *SSEServer
func NewStreamableHTTPServer(listener net.Listener, endpointPath string, origins []string) *StreamableHTTPServer
```

状态转移唯一为 `new --initialize--> negotiated --notifications/initialized--> ready --close--> closed`。`Negotiate` 只接受 new，冻结版本；`MarkInitialized` 只接受 negotiated；重复/越序 initialize 是 `-32600`，越序 initialized notification 因无 ID 不写响应并关闭 session。`CanPing`/`Ready` 都在锁下读取状态；前者接受 negotiated/ready，后者只接受 ready。初始化前的 `tools/list`/`tools/call` 返回 `-32002`（session not initialized）。

Transport 的 `Serve` 负责监听、创建并销毁 `ServerSession`、HTTP/SSE framing 和把 `Message` 交给 handler。stdio 为单一 session；legacy SSE 按 SSE GET/endpoint 建立 session。Yaa! Streamable HTTP Server 只允许没有 session header 的 initialize 创建会话，生成 32-byte `crypto/rand` URL-safe ID并在响应 header 返回；其他 POST/GET/DELETE 缺少 ID返回 400，未知/过期 ID返回 404。后续请求按 ID 复用 session；只有 DELETE、30 分钟空闲或 Server Close 才将其转为 closed并删除，单次 TCP/HTTP 连接关闭不销毁 session。固定最多 1024 个并发 session，超出返回 503；创建、查找、touch、删除在 transport 的 session map 锁下完成，handler 不持该锁。`MCPServer` handler 只通过 session 保存协商版本和 initialized 状态；MVP 对 `resources/*`、`prompts/*` 返回 `-32601`。

Streamable HTTP Server 默认只绑定 loopback，并校验 `Origin`：缺失 `Origin` 时允许非浏览器客户端；存在 `Origin` 时必须精确命中非空的 `mcp.server.origin_allowlist`。allowlist 为空或不匹配都返回 `403 Forbidden`，防止 DNS rebinding。

## 5. 连接状态与重试

统一状态为 `disconnected → connecting → connected → error`。`auto_start: false` 的 Server 保持 `disconnected`；v1 不提供运行期 Connect API，修改后重启 Runtime。

| 阶段 | 可重试错误 | 不可重试错误 |
|------|------------|--------------|
| 启动/拨号 | connection refused、timeout | 配置缺失 |
| initialize | 暂时断开 | protocol version 不兼容、鉴权失败 |
| tools/call | 无；任何已发送请求一律不自动重试 | 断线、写入结果不确定、timeout、参数错误、Tool 不存在 |

## 6. 配置示例

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
      auto_start: true

    - name: remote-tools
      transport: streamable_http
      url: https://mcp.example.com/mcp
      auto_start: true
```

---

*最后更新: 2025-07-17*
