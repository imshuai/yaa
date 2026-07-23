# MCP Client

> 文档路径: `docs/mcp/client.md`
> 上级: [`README.md`](README.md)
> 协议版本: `2025-03-26`、legacy `2024-11-05`

---

## 1. 状态与结构

```go
type ConnectionStatus string

const (
    StatusDisconnected ConnectionStatus = "disconnected"
    StatusConnecting   ConnectionStatus = "connecting"
    StatusConnected    ConnectionStatus = "connected"
    StatusError        ConnectionStatus = "error"
)

type Client struct {
    name          string
    runCtx        context.Context // Manager 生命周期；不是 startup timeout ctx
    transport     ClientTransport
    status        ConnectionStatus
    cancel        context.CancelFunc
    done          chan struct{} // failOnce 关闭；Manager 的无损断线信号
    control       chan *Message
    closeOnce     sync.Once
    failOnce      sync.Once
    closeErr      error
    wg            sync.WaitGroup
    nextID        uint64
    issuedHighWater uint64
    pending       map[uint64]*pendingCall
    closedErr     error
    closing       bool
    pendingMu     sync.Mutex
    mu            sync.RWMutex
    onListChanged func()
}

type clientResponse struct {
    msg *Message
    err error
}

type pendingCall struct {
    ch chan clientResponse // 容量 1；摘除 map 的 goroutine 恰好投递一次
}
```

`Client` 只拥有一次 transport 连接；它不拥有稳定 Proxy、catalog、退避或重连。Manager 为每次重连创建新的 Client，旧 Client 的 `closeOnce` 不复用。`Done()` 是该代连接的无损关闭信号，`Err()` 返回触发关闭的稳定错误；`onListChanged` 只向该代容量为 1 的合并 channel 非阻塞投递，回调不得在 `recvLoop` 中发起 request。

```go
func (c *Client) request(ctx context.Context, method string, params, out any) error {
    rawParams, err := marshalParams(params) // nil => omitted params
    if err != nil {
        return err
    }
    call := &pendingCall{ch: make(chan clientResponse, 1)}
    c.pendingMu.Lock()
    if c.closedErr != nil {
        err := c.closedErr
        c.pendingMu.Unlock()
        return err
    }
    if c.nextID == math.MaxUint64 || len(c.pending) >= 1024 {
        c.pendingMu.Unlock()
        c.fail(ErrMCPProtocolError)
        return ErrMCPProtocolError
    }
    c.nextID++
    id := c.nextID
    c.issuedHighWater = id
    c.pending[id] = call
    c.pendingMu.Unlock()
    defer func() {
        c.retire(id, call)
    }()

    if err := c.transport.Send(ctx, &Message{
        JSONRPC: "2.0", ID: json.RawMessage(strconv.FormatUint(id, 10)),
        Method: method, Params: rawParams,
    }); err != nil {
        if ctx.Err() != nil {
            return context.Cause(ctx)
        }
        writeErr := fmt.Errorf("%w: %v", ErrMCPTransportWrite, err)
        c.fail(writeErr)
        return writeErr // Send 后结果可能不确定；上层不得 replay。
    }
    select {
    case response := <-call.ch:
        if response.err != nil {
            return response.err
        }
        if response.msg.Error != nil {
            return mapRPCError(response.msg.Error)
        }
        if len(response.msg.Result) == 0 {
            return ErrMCPProtocolError
        }
        if err := json.Unmarshal(response.msg.Result, out); err != nil {
            c.fail(ErrMCPProtocolError)
            return ErrMCPProtocolError
        }
        return nil
    case <-ctx.Done():
        c.bestEffortCancel(id, context.Cause(ctx))
        return context.Cause(ctx)
    }
}
```

`marshalParams` 严格编码 object 或 nil；`retire` 只有在 map 中仍指向同一个 `pendingCall` 时才删除。`bestEffortCancel` 使用从 `runCtx` 派生、最多 100ms 的 context 发送一次 `notifications/cancelled`，参数只含原 request ID 和固定 reason；失败不覆盖 caller cause，notification 不进入 pending、不重试也不在新连接 replay。`recvLoop` 解析 response ID 为正 `uint64`，在 `pendingMu` 下摘除 entry，解锁后向容量为 1 的 channel 投递一次；未知但 `id <= issuedHighWater` 的 response 是 caller timeout 后的 late/duplicate，丢弃并计数，不毒化连接；`id==0`、格式非法或 `id > issuedHighWater` 才是协议错误。这样不需要无界 tombstone。

```go
func (c *Client) fail(err error) {
    c.failOnce.Do(func() {
        c.pendingMu.Lock()
        c.closedErr = err
        calls := c.pending
        c.pending = make(map[uint64]*pendingCall)
        c.pendingMu.Unlock()

        for _, call := range calls {
            call.ch <- clientResponse{err: err}
        }
        c.mu.Lock()
        c.status = StatusError
        closing := c.closing
        c.mu.Unlock()
        if c.cancel != nil {
            c.cancel()
        }
        transportErr := c.transport.Close()
        if closing {
            c.closeErr = transportErr
        }
        close(c.done)
    })
}

func (c *Client) retire(id uint64, call *pendingCall) {
    c.pendingMu.Lock()
    if current := c.pending[id]; current == call {
        delete(c.pending, id)
    }
    c.pendingMu.Unlock()
}
```

`recvLoop` 是唯一调用 `transport.Recv` 的 goroutine；它先用 `validateEnvelope` 分类 response/request/notification。Server request 进入固定容量 32 的 `control` channel，由独立 `controlLoop` 回复 `ping` 或 `-32601`；notification 只合并投递 `tools/list_changed`，不在 dispatcher 内同步调用 Manager。control queue 满、Recv 失败或 envelope 错误调用 `fail`。两个 loop 都只在构造时加入 `wg`，退出时 `Done`；不得在 `Close` 已开始后 `wg.Add`。

```go
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
    c.pendingMu.Lock()
    defer c.pendingMu.Unlock()
    return c.closedErr
}

func (c *Client) Close() error {
    c.closeOnce.Do(func() {
        c.mu.Lock()
        c.closing = true
        c.mu.Unlock()
        c.fail(ErrMCPTransportClosed)
        c.wg.Wait()
        c.mu.Lock()
        c.status = StatusDisconnected
        c.mu.Unlock()
    })
    return c.closeErr
}
```

`Close` 必须从 Client loop 之外调用；它先使调用入口 unavailable，再取消连接 context、关闭 transport并等待 dispatcher/control goroutine。`failOnce` 确保 pending completion、transport Close 和 `done` close 都恰好一次。正常 Manager Close 不生成重连事件；Manager 已在等待同一个 `Done()`/`runCtx`，并通过 stopping/generation 判定是否重连。

## 2. 建立连接

```text
disconnected
  → connecting
  → transport.Start
  → initialize(protocolVersion=transport 首选版本)
  → 校验响应 protocolVersion
  → notifications/initialized
  → connected
```

`NewClient` 接收 Manager 的长期 `runCtx`。`Connect(startupCtx)` 只用 startupCtx 约束 transport.Start/initialize/initialized notification；它先从 `runCtx` 派生连接 context 并启动 recv/control loops，握手成功后取消 startupCtx 不得关闭连接。任一步失败都调用 `Close`。连接成功后 `StatusConnected` 只在 initialized notification 已成功发送后发布。

Server 按协议在正常 InitializeResult 中选择版本。Client 只有在返回版本同时满足支持列表和 transport 约束时才能继续，否则关闭连接并返回 `ErrMCPProtocolError`：

| transport | initialize 发送 | 可接受响应 |
|-----------|-----------------|------------|
| `streamable_http` | `2025-03-26` | `2025-03-26` |
| legacy `sse` | `2024-11-05` | `2024-11-05` |
| `stdio` | `2025-03-26` | `2025-03-26`、`2024-11-05` |

```go
const (
    ProtocolVersion       = "2025-03-26"
    LegacyProtocolVersion = "2024-11-05"
)

func (c *Client) Initialize(ctx context.Context) error {
    var result InitializeResult
    err := c.request(ctx, "initialize", InitializeParams{
        ProtocolVersion: preferredVersion(c.transport.Info().Type),
        Capabilities:    map[string]any{},
        ClientInfo:      Implementation{Name: "yaa", Version: runtimeVersion},
    }, &result)
    if err != nil {
        return err
    }
    if !acceptsVersion(c.transport.Info().Type, result.ProtocolVersion) {
        return fmt.Errorf("%w: server selected %s", ErrMCPProtocolError, result.ProtocolVersion)
    }
    if _, ok := result.Capabilities["tools"]; !ok {
        return fmt.Errorf("%w: server does not advertise tools", ErrMCPProtocolError)
    }
    return c.notify(ctx, "notifications/initialized", nil)
}

func (c *Client) Ping(ctx context.Context) error {
    var result struct{}
    return c.request(ctx, "ping", nil, &result)
}
```

`Ping` 只验证当前代连接，不启动重连；Manager 使用独立 heartbeat deadline 调用它，并根据返回值决定是否替换该代 Client。

## 3. Tool 发现

`tools/list` 使用 opaque cursor；Client 循环请求直到 `nextCursor` 为空，并为每个 Tool 创建统一命名：

```text
mcp.<server_name>.<tool_name>
```

```go
func (c *Client) DiscoverTools(ctx context.Context) ([]MCPTool, error) {
    var all []MCPTool
    cursor := ""
    seenCursors := map[string]struct{}{"": {}}
    names := make(map[string]struct{})
    for pageNo := 0; pageNo < 128; pageNo++ {
        page, err := c.listTools(ctx, cursor)
        if err != nil {
            return nil, err
        }
        if len(page.Tools) > 4096-len(all) {
            c.fail(ErrMCPProtocolError)
            return nil, ErrMCPProtocolError
        }
        for _, candidate := range page.Tools {
            normalized, err := normalizeTool(candidate) // name/description/schema/size
            if err != nil {
                c.fail(ErrMCPProtocolError)
                return nil, ErrMCPProtocolError
            }
            if _, duplicate := names[normalized.Name]; duplicate {
                c.fail(ErrMCPProtocolError)
                return nil, ErrMCPProtocolError
            }
            names[normalized.Name] = struct{}{}
            all = append(all, normalized)
        }
        if page.NextCursor == "" {
            sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
            return all, nil
        }
        if len(page.NextCursor) > 4096 {
            c.fail(ErrMCPProtocolError)
            return nil, ErrMCPProtocolError
        }
        if _, repeated := seenCursors[page.NextCursor]; repeated {
            c.fail(ErrMCPProtocolError)
            return nil, ErrMCPProtocolError
        }
        seenCursors[page.NextCursor] = struct{}{}
        cursor = page.NextCursor
    }
    c.fail(ErrMCPProtocolError)
    return nil, ErrMCPProtocolError
}
```

私有 `listTools` 使用专用 wire DTO 严格解码：`tools` 必须存在、非 null 且为 array；`nextCursor` 只能省略或为 string，null/其他类型都拒绝；整个 result 只允许这两个字段并执行 EOF 检查。任一页失败丢弃本次临时 `all`，不得修改已冻结 catalog或发布部分结果。

`normalizeTool` 要求远端 name 是 1..128 UTF-8 bytes且不含控制字符，description 不超过 4 KiB，inputSchema 不超过 256 KiB并按 [transport.md](transport.md#2-协议版本与-json-rpc-消息) 的规则严格规范化。完整 canonical 名称 `mcp.<server>.<remote>` 还必须是合法 UTF-8、无控制字符且不超过 256 bytes。重复远端 name、重复 cursor、超出 page/Tool 上限或 wire shape 非法都关闭连接并返回 `ErrMCPProtocolError`。完整名称与已有 Tool 重复是配置错误，不能静默覆盖。

## 4. Tool 调用

```go
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
    if c.Status() != StatusConnected {
        return nil, ErrMCPUnavailable
    }
    var result *CallToolResult
    err := c.request(ctx, "tools/call", CallToolParams{
        Name:      name,
        Arguments: arguments,
    }, &result)
    if err != nil {
        if ctx.Err() != nil {
            return nil, context.Cause(ctx)
        }
        return nil, err
    }
    if result == nil {
        c.fail(ErrMCPProtocolError)
        return nil, ErrMCPProtocolError
    }
    return result, nil
}
```

`Close` 必须幂等：先使调用入口 unavailable，再设置 `closing`、取消连接 context、关闭 transport，并等待该 Client 拥有的 dispatcher/control goroutine 退出后返回。Client 没有 heartbeat/reconnect loop；Manager 关闭或替换连接时只调用一次 `Client.Close`。

- 上游 JSON-RPC `-32602` 统一映射为 `ErrMCPInvalidParams`；不能解析 message 猜测 Tool 不存在。`ErrMCPToolNotFound` 只用于本地 catalog 查找。
- Tool 下游执行失败：响应仍是 JSON-RPC result，但 `isError=true`。
- Tool hard cap 为 0 时不创建额外 deadline，只使用 Tool Manager/caller context；非零时取两者较早者，caller cause 优先。
- `CallTool` 发现 context 已结束时只返回 `context.Cause(ctx)`，不得把 caller 的 `context.DeadlineExceeded` 重映射为 `ErrMCPToolTimeout`；Proxy 的非零 hard cap 在 Go 1.20 使用 `context.WithCancelCause` + `time.AfterFunc` 设置 cause，因此只有该 hard cap 到期时 child cause 才是 `ErrMCPToolTimeout`。

## 5. Manager 重连语义

Transport 断开时状态进入 `error`，稳定 Proxy 的 atomic client handle 置空，调用立即返回 `ErrMCPUnavailable`。Manager 按 `mcp.reconnect` 的指数退避配置重新执行 initialize 和完整分页 `tools/list`。新结果的 Tool 名称、description 与 input schema 必须和当前 Proxy 快照精确一致；全部一致才原子替换 client handle。任一差异都保持 unavailable、记录 `ErrMCPProtocolError`，等待 Runtime 重启重新建立 catalog。任何已经发送但未确认的 Tool 调用都返回结果不确定错误，绝不在新连接上自动重放；重连只服务之后的新调用。

| 尝试 | 延迟 |
|------|------|
| 1 | 1s |
| 2 | 2s |
| 3 | 4s |

默认最多 3 次，初始延迟 1s、最大延迟 60s；达到上限后保持 `error`，Runtime 继续运行。

## 6. 配置

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      timeout: 0
      auto_start: true
    - name: remote
      transport: streamable_http
      url: https://mcp.example.com/mcp
      timeout: 0
      auto_start: true
```

---

*最后更新: 2025-07-17*
