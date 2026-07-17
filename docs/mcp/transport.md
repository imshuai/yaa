# MCP 传输层设计

## 概述

MCP（Model Context Protocol）支持三种传输方式，Yaa! Runtime 通过统一的 `Transport` 接口抽象底层通信细节，使上层逻辑无需关心具体的传输实现。

## Transport 接口

```go
// Transport 定义 MCP 传输层的统一接口
type Transport interface {
    // Start 启动传输层，建立连接
    Start(ctx context.Context) error

    // Send 发送 JSON-RPC 消息
    Send(ctx context.Context, msg *Message) error

    // Recv 接收 JSON-RPC 消息（阻塞直到收到或出错）
    Recv(ctx context.Context) (*Message, error)

    // Close 关闭传输层，释放资源
    Close() error

    // Info 返回传输层元信息
    Info() TransportInfo
}

// TransportInfo 描述传输层基本信息
type TransportInfo struct {
    Type      string   // "stdio" | "sse" | "websocket"
    Endpoint  string   // 连接端点描述
    Connected bool     // 是否已连接
    Options   map[string]any
}

// Message 是 MCP JSON-RPC 2.0 消息结构
type Message struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.Number    `json:"id,omitempty"`
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
}
```

## stdio 传输

stdio 传输通过子进程的标准输入/输出进行通信，适用于本地 MCP Server 进程。

```go
// StdioTransport 通过子进程 stdin/stdout 通信
type StdioTransport struct {
    cmd     *exec.Cmd
    stdin   io.WriteCloser
    stdout  io.ReadCloser
    encoder *json.Encoder
    decoder *json.Decoder
    mu      sync.Mutex
    closed  bool
}

func NewStdioTransport(command string, args ...string) *StdioTransport {
    return &StdioTransport{
        cmd: exec.Command(command, args...),
    }
}

func (t *StdioTransport) Start(ctx context.Context) error {
    t.cmd.Stderr = os.Stderr // 转发子进程日志

    stdin, err := t.cmd.StdinPipe()
    if err != nil {
        return fmt.Errorf("create stdin pipe: %w", err)
    }
    stdout, err := t.cmd.StdoutPipe()
    if err != nil {
        return fmt.Errorf("create stdout pipe: %w", err)
    }

    if err := t.cmd.Start(); err != nil {
        return fmt.Errorf("start subprocess: %w", err)
    }

    t.stdin = stdin
    t.stdout = stdout
    t.encoder = json.NewEncoder(stdin)
    t.decoder = json.NewDecoder(stdout)
    return nil
}

func (t *StdioTransport) Send(ctx context.Context, msg *Message) error {
    t.mu.Lock()
    defer t.mu.Unlock()
    return t.encoder.Encode(msg)
}

func (t *StdioTransport) Recv(ctx context.Context) (*Message, error) {
    var msg Message
    if err := t.decoder.Decode(&msg); err != nil {
        return nil, err
    }
    return &msg, nil
}

func (t *StdioTransport) Close() error {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.closed {
        return nil
    }
    t.closed = true
    t.stdin.Close()
    return t.cmd.Wait()
}
```

## SSE 传输

SSE（Server-Sent Events）传输通过 HTTP 长连接单向接收消息，发送则通过独立的 HTTP POST 请求完成。

```go
// SSETransport 通过 Server-Sent Events 通信
type SSETransport struct {
    url       string         // MCP Server SSE 端点
    postURL   string         // 发送消息的 POST 端点
    scanner   *bufio.Scanner // SSE 事件流读取器
    resp      *http.Response
    client    *http.Client
    closed    bool
}

func NewSSETransport(url string) *SSETransport {
    return &SSETransport{
        url:    url,
        client: &http.Client{Timeout: 0}, // 长连接不超时
    }
}

func (t *SSETransport) Start(ctx context.Context) error {
    req, _ := http.NewRequestWithContext(ctx, "GET", t.url, nil)
    req.Header.Set("Accept", "text/event-stream")

    resp, err := t.client.Do(req)
    if err != nil {
        return fmt.Errorf("connect SSE: %w", err)
    }

    // 从首个 event 中获取 POST 端点
    t.resp = resp
    t.scanner = bufio.NewScanner(resp.Body)
    for t.scanner.Scan() {
        line := t.scanner.Text()
        if strings.HasPrefix(line, "endpoint:") {
            t.postURL = strings.TrimSpace(strings.TrimPrefix(line, "endpoint:"))
            break
        }
    }
    if t.postURL == "" {
        return errors.New("SSE: endpoint not received")
    }
    return nil
}

func (t *SSETransport) Send(ctx context.Context, msg *Message) error {
    data, _ := json.Marshal(msg)
    req, _ := http.NewRequestWithContext(ctx, "POST", t.postURL, bytes.NewReader(data))
    req.Header.Set("Content-Type", "application/json")
    resp, err := t.client.Do(req)
    if err != nil {
        return err
    }
    resp.Body.Close()
    return nil
}

func (t *SSETransport) Recv(ctx context.Context) (*Message, error) {
    for t.scanner.Scan() {
        line := t.scanner.Text()
        if strings.HasPrefix(line, "data:") {
            data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
            var msg Message
            if err := json.Unmarshal([]byte(data), &msg); err != nil {
                return nil, err
            }
            return &msg, nil
        }
    }
    return nil, io.EOF
}
```

## WebSocket 传输

WebSocket 传输提供全双工双向通信，适用于需要低延迟、高频交互的场景。

```go
// WebSocketTransport 通过 WebSocket 双向通信
type WebSocketTransport struct {
    url    string
    conn   *websocket.Conn
    mu     sync.Mutex
    closed bool
}

func NewWebSocketTransport(url string) *WebSocketTransport {
    return &WebSocketTransport{url: url}
}

func (t *WebSocketTransport) Start(ctx context.Context) error {
    conn, _, err := websocket.DefaultDialer.DialContext(ctx, t.url, nil)
    if err != nil {
        return fmt.Errorf("connect websocket: %w", err)
    }
    t.conn = conn
    return nil
}

func (t *WebSocketTransport) Send(ctx context.Context, msg *Message) error {
    t.mu.Lock()
    defer t.mu.Unlock()
    data, _ := json.Marshal(msg)
    return t.conn.WriteMessage(websocket.TextMessage, data)
}

func (t *WebSocketTransport) Recv(ctx context.Context) (*Message, error) {
    _, data, err := t.conn.ReadMessage()
    if err != nil {
        return nil, err
    }
    var msg Message
    if err := json.Unmarshal(data, &msg); err != nil {
        return nil, err
    }
    return &msg, nil
}

func (t *WebSocketTransport) Close() error {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.closed {
        return nil
    }
    t.closed = true
    return t.conn.Close()
}
```

## 三种传输对比

| 特性 | stdio | SSE | WebSocket |
|------|-------|-----|-----------|
| 通信方向 | 双向（stdin/stdout） | 单向接收 + POST 发送 | 全双工 |
| 适用场景 | 本地子进程 | 远程 HTTP 服务 | 远程实时交互 |
| 连接方式 | 子进程管道 | HTTP 长连接 | WebSocket 握手升级 |
| 延迟 | 最低（进程间管道） | 较高（HTTP 轮询+重连） | 低（持久连接） |
| 鉴权 | 无需（本地进程） | HTTP Header / Token | HTTP Header / Token |
| 心跳保活 | 不需要 | 需要（SSE comment） | 需要（Ping/Pong） |
| 重连机制 | 重新启动子进程 | 自动重连 + Last-Event-ID | 自动重连 |
| 并发支持 | 单连接串行 | 单连接串行 | 单连接串行 |
| 依赖 | 无 | net/http | gorilla/websocket |
| 配置复杂度 | 低 | 中 | 中 |

## 传输选择指南

```
MCP Server 是本地子进程？
  └─ 是 → stdio
  └─ 否 → 需要全双工实时通信？
            └─ 是 → WebSocket
            └─ 否 → SSE
```

## TransportManager

Yaa! Runtime 通过 `TransportManager` 统一管理所有 MCP 传输连接：

```go
// TransportManager 管理多个 MCP 传输连接
type TransportManager struct {
    transports map[string]Transport // key: server name
    mu        sync.RWMutex
}

func (m *TransportManager) Connect(ctx context.Context, name string, t Transport) error {
    if err := t.Start(ctx); err != nil {
        return err
    }
    m.mu.Lock()
    m.transports[name] = t
    m.mu.Unlock()
    return nil
}

func (m *TransportManager) Get(name string) (Transport, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    t, ok := m.transports[name]
    return t, ok
}

func (m *TransportManager) CloseAll() {
    m.mu.Lock()
    defer m.mu.Unlock()
    for name, t := range m.transports {
        t.Close()
        delete(m.transports, name)
    }
}
```

## 配置示例

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"]

    - name: remote-tools
      transport: sse
      url: https://mcp.example.com/sse
      headers:
        Authorization: "Bearer {{token}}"

    - name: realtime
      transport: websocket
      url: ws://mcp.example.com/ws
      headers:
        Authorization: "Bearer {{token}}"
```
