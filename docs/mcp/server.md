# MCP Server

> 文档路径: `docs/mcp/server.md`
> 上级: [`README.md`](README.md)
> 协议版本: `2025-03-26`、legacy `2024-11-05`

---

## 1. 责任边界

Yaa! MCP Server 把明确允许的 Yaa! Tool 暴露给外部 MCP Client。它使用 `ServerTransport` 的 `Serve` 接口，不复用 Client 的拨号/`Recv` 实现。

```go
type MCPServer struct {
    tools     *tool.Manager
    agentID   string
    exposed   map[string]bool
    catalog   []MCPTool // prepared 时冻结，按 canonical name 排序
    digest    [16]byte  // catalog canonical JSON 的 SHA-256 前 16 bytes
    transport ServerTransport
}

func NewMCPServer(tools *tool.Manager, cfg config.MCPExposeConfig) (*MCPServer, error)
func (s *MCPServer) Serve(ctx context.Context) error
func (s *MCPServer) Close() error
```

## 2. 初始化

Server 按 transport 使用唯一版本：Streamable HTTP 为 `2025-03-26`，legacy SSE 为 `2024-11-05`；stdio 接受这两个版本并优先选择 Client 请求的受支持版本。请求版本不满足 transport 约束时仍返回正常 `InitializeResult`，其中携带该 transport 的版本；不接受该版本的 Client 负责关闭连接。版本差异本身不是 JSON-RPC error。正常响应的 capabilities 只包含实际实现的 `tools`。

```go
func (s *MCPServer) handle(
    ctx context.Context,
    session *ServerSession,
    msg *Message,
) (*Message, error) {
    kind, err := validateEnvelope(msg)
    if err != nil {
        return rpcError(msg.ID, -32600, "Invalid Request"), nil
    }
    if kind == KindResponse {
        return nil, ErrMCPProtocolError // Server 不发起 request，不接受孤立 response。
    }
    if kind == KindNotification {
        if msg.Method != "notifications/initialized" {
            return nil, nil // 未声明的 notification 忽略且永不响应。
        }
        if err := session.MarkInitialized(); err != nil {
            return nil, ErrMCPProtocolError
        }
        return nil, nil
    }

    if msg.Method == "notifications/initialized" {
        return rpcError(msg.ID, -32600, "Invalid Request"), nil
    }
    if msg.Method == "initialize" {
        var params InitializeParams
        if err := decodeParams(msg.Params, &params); err != nil {
            return rpcError(msg.ID, -32602, "Invalid params"), nil
        }
        version := serverVersion(session.Transport, params.ProtocolVersion)
        if err := session.Negotiate(version); err != nil {
            return rpcError(msg.ID, -32600, "Invalid Request"), nil
        }
        return rpcResult(msg.ID, InitializeResult{
            ProtocolVersion: version,
            Capabilities: map[string]any{"tools": map[string]any{"listChanged": false}},
            ServerInfo: Implementation{Name: "yaa", Version: runtimeVersion},
        }), nil
    }
    if msg.Method == "ping" {
        if !session.CanPing() {
            return rpcError(msg.ID, -32002, "Server not initialized"), nil
        }
        return rpcResult(msg.ID, struct{}{}), nil
    }
    if !session.Ready() && (msg.Method == "tools/list" || msg.Method == "tools/call") {
        return rpcError(msg.ID, -32002, "Server not initialized"), nil
    }

    switch msg.Method {
    case "tools/list":
        result, rpcErr := s.listTools(msg.Params)
        if rpcErr != nil {
            return rpcError(msg.ID, rpcErr.Code, rpcErr.Message), nil
        }
        return rpcResult(msg.ID, result), nil
    case "tools/call":
        var params CallToolParams
        if err := decodeParams(msg.Params, &params); err != nil {
            return rpcError(msg.ID, -32602, "Invalid params"), nil
        }
        result, err := s.CallTool(ctx, params)
        var rpcErr *RPCError
        if errors.As(err, &rpcErr) {
            return rpcError(msg.ID, rpcErr.Code, rpcErr.Message), nil
        }
        if err != nil {
            return nil, err
        }
        return rpcResult(msg.ID, result), nil
    default:
        return rpcError(msg.ID, -32601, "Method not found"), nil
    }
}
```

`decodeParams` 使用 `json.Decoder.DisallowUnknownFields`、`UseNumber` 和 EOF 检查，并要求 params 是 object。`rpcResult`/`rpcError` 原样复制合法 request ID；error message 只使用上面固定文本，不拼接内部 error。所有 per-connection 初始化状态都来自 transport 创建的 `ServerSession`，不能放在 `MCPServer` 全局字段中。

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2025-03-26",
    "capabilities": {"tools": {"listChanged": false}},
    "serverInfo": {"name": "yaa", "version": "0.1.0"}
  }
}
```

## 3. tools/list

支持 opaque cursor 分页。服务端不能在没有 cursor 契约的情况下截断列表：

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "tools": [
      {
        "name": "shell",
        "description": "Execute an allow-listed command",
        "inputSchema": {"type": "object", "properties": {}}
      }
    ]
  }
}
```

`NewMCPServer` 从 `exposed_tools` 构造一次不可变 catalog，按 canonical name 字节升序排列并对规范化后的 name/description/schema JSON 计算 digest；运行期不重新查询 Tool Manager。固定 page size 为 100。省略 params 或传 `{}` 都表示 offset 0；其他字段使用严格解码。

非空 cursor 是固定 21 bytes 的 `base64.RawURLEncoding`：`version(1) || digest(16) || offset(uint32 big-endian)`。解码后必须按同一编码重编码得到原字符串，version 必须为 1、digest 必须匹配当前 catalog，offset 必须是此前可能返回的页边界且小于 catalog 长度；否则返回 `-32602`。只有仍有下一页时才返回 `nextCursor`。空 catalog 和末页都明确编码 `"tools":[]` 或非空 array，不能输出 null；该规则使同一 catalog 的分页顺序稳定且 cursor 不依赖进程地址或 map 顺序。

```go
func (s *MCPServer) listTools(raw json.RawMessage) (ListToolsResult, *RPCError) {
    cursor, err := decodeListCursor(raw, s.digest, len(s.catalog))
    if err != nil {
        return ListToolsResult{}, rpcInvalidParams("invalid cursor")
    }
    end := cursor.Offset + 100
    if end > len(s.catalog) {
        end = len(s.catalog)
    }
    out := ListToolsResult{Tools: cloneTools(s.catalog[cursor.Offset:end])}
    if end < len(s.catalog) {
        out.NextCursor = encodeListCursor(s.digest, end)
    }
    return out, nil
}
```

`decodeListCursor` 接受省略 params、`{}` 或空 cursor；params 中未知字段、`null`、非 object、cursor 非 string、非规范 base64、offset 溢出或非页边界都返回错误。`cloneTools`、`catalogDigest` 是本模块内的无状态小 helper，不读取 Tool Manager；因此一页处理期间不会因其他状态变化而改变 catalog。

## 4. tools/call

```go
func (s *MCPServer) CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error) {
    if !s.exposed[req.Name] {
        return nil, rpcInvalidParams("unknown tool") // -32602
    }
    result, err := s.tools.Execute(ctx, tool.ExecutionScope{
        AgentID: s.agentID,
        // MCP Server 请求不是 Yaa! Session turn。
        SessionID: "",
    }, req.Name, req.Arguments)
    if err != nil {
        if ctx.Err() != nil {
            return nil, context.Cause(ctx)
        }
        if errors.Is(err, tool.ErrInvalidParams) || errors.Is(err, tool.ErrToolNotFound) {
            return nil, rpcInvalidParams("invalid tool arguments")
        }
        // 其余硬错误使用 Tool 模块唯一的安全投影，不暴露原始 error。
        return toMCPResult(tool.ErrorResult(err)), nil
    }
    return toMCPResult(result), nil
}
```

未知 method（包括所有 `resources/*`、`prompts/*` request）使用 `-32601`；未知 Tool、参数类型错误和非法 cursor 使用 `-32602`；下游软错误及其他已分类硬错误使用 `result.isError=true`。caller 取消/超时返回 `context.Cause(ctx)` 给 transport，transport 不再尝试写业务 response。

## 5. ping

`ping` 返回空 JSON-RPC result，不触发 Tool Manager 操作。健康检查由 Runtime Manager 统计 transport/进程状态完成。

## 6. Transport 启动

```go
func NewMCPServer(tools *tool.Manager, cfg config.MCPExposeConfig) (*MCPServer, error) {
    if tools == nil || cfg.AgentID == "" {
        return nil, fmt.Errorf("%w: mcp.server.agent_id is required", ErrMCPConfig)
    }
    allowed := make(map[string]bool)
    for _, info := range tools.ListForAgent(cfg.AgentID) {
        allowed[info.Name] = true
    }
    exposed := make(map[string]bool, len(cfg.ExposedTools))
    catalog := make([]MCPTool, 0, len(cfg.ExposedTools))
    for _, name := range cfg.ExposedTools {
        if exposed[name] {
            return nil, fmt.Errorf("%w: duplicate mcp.server.exposed_tools entry %q", ErrMCPConfig, name)
        }
        exposed[name] = true
        if !allowed[name] {
            return nil, fmt.Errorf("%w: exposed tool %q is not enabled or not allowed for agent %q", ErrMCPConfig, name, cfg.AgentID)
        }
        instance, err := tools.Get(name)
        if err != nil {
            return nil, fmt.Errorf("%w: exposed tool %q: %v", ErrMCPConfig, name, err)
        }
        catalog = append(catalog, MCPTool{
            Name: instance.Name(), Description: instance.Description(),
            InputSchema: append(json.RawMessage(nil), instance.Parameters()...),
        })
    }
    sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
    s := &MCPServer{
        tools: tools, agentID: cfg.AgentID, exposed: exposed,
        catalog: catalog, digest: catalogDigest(catalog),
    }
    switch cfg.Transport {
    case "stdio":
        s.transport = NewStdioServer()
    case "sse", "streamable_http":
        listener, err := net.Listen("tcp", cfg.Addr)
        if err != nil {
            return nil, fmt.Errorf("%w: listen %s: %v", ErrMCPConfig, cfg.Addr, err)
        }
        if cfg.Transport == "sse" {
            s.transport = NewSSEServer(listener, cfg.Path, cfg.MessagesPath)
        } else {
            s.transport = NewStreamableHTTPServer(listener, cfg.Path, cfg.OriginAllowlist)
        }
    default:
        return nil, fmt.Errorf("%w: mcp.server.transport=%q", ErrMCPConfig, cfg.Transport)
    }
    return s, nil
}

func (s *MCPServer) Serve(ctx context.Context) error {
    return s.transport.Serve(ctx, s.handle)
}

func (s *MCPServer) Close() error {
    if s.transport == nil {
        return nil
    }
    return s.transport.Close()
}
```

`NewMCPServer` 在绑定网络 listener 前复制 `agent_id` 和 `exposed_tools`，显式拒绝重复 Tool 名称，并校验每个 Tool 当前 enabled 且通过该 Agent allowlist。这样即使调用方绕过根 Config Validator，构造期也不会生成重复 catalog；根校验与构造防御使用同一唯一性契约。构造成功只表示 prepared，不得自行启动 goroutine。`Serve` 是阻塞调用；MCP Manager 仅在 `Config.Activate(binding)` 成功后于受控 goroutine 中运行它，并在 `Stop` 时取消 context、调用 `Close`、等待 goroutine 退出。stdio 不创建 listener。

Server transport 的构造器和 wire 细节见 [transport.md](transport.md)。MCP Server 不监听 WebSocket；需要 WebSocket 的客户端连接 Yaa! Remote API。

## 7. 暴露配置

```yaml
mcp:
  server:
    enabled: true
    agent_id: "default"
    transport: streamable_http
    addr: "127.0.0.1:9090"
    path: "/mcp"
    exposed_tools: ["shell", "file_read"]
```

根 Config Validator 负责 `exposed_tools` 的非空与唯一性；`Config.Activate(binding)` 在 Serve 前校验 `agent_id` 和每个 Tool 引用；`NewMCPServer` 构造时仍独立拒绝重复项，并确认每个 Tool 当前 enabled、存在且通过该 Agent allowlist。三层使用同一唯一性与权限契约，但构造防御不能替代根校验或 binding。`tools/call` 始终把这个固定 principal 传入 Tool Manager，再次执行权限、超时和审计检查。网络 transport 默认只绑定 loopback，并按 [`transport.md`](transport.md) 校验 Origin。

---

*最后更新: 2025-07-17*
