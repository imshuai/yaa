# MCP 实现检查清单

> 文档路径: `docs/mcp/checklist.md`
> 上级: `docs/mcp/README.md`

---

## 1. MCP Manager

- [x] `Manager` 结构体定义（entries、run/stop context、done、logger、mu） — v1 起点已落地；Tool Manager 字段已签名透传，待后续 commit 接 Client / Proxy 后实际使用；本地 MCPServer 字段已在 progress #19 引入 (mcpServer *MCPServer + mcpServerDone chan struct{}). Prepare 持有本地 Server transport；Activate 起 Serve goroutine；Stop 关它.
- [x] `Get()` / `Tools()` — 返回 ServerStatus 副本（含 ConnectedAt 指针深拷贝）+ Tool 列表深拷贝；Tools 在 server 未连接或无 Tool 时返 (nil, false)
- [x] `List() []ServerStatus` — 列出所有配置的上游连接状态（从 status 字段投影，Prepare 后含 connected / error 真实状态）
- [x] `Prepare()` — 自动启动 stdio auto_start Client 并注册稳定 Proxy（Connect → Initialize → DiscoverTools → Register MCPToolProxy → 启 runUpstream heartbeat goroutine）；SSE / Streamable HTTP 待 §5/§6 commit；单 server 失败仅标 LastError + Status=Error, 不阻断其他
- [x] `Activate()` — progress #19 已接入本地 MCPServer.Serve: 未启用 local Server 返 nil; 启用 → 起 goroutine 调 mcpServer.Serve 继承 runCtx; Serve 非取消退出置 Ready=false (unhealthy, docs §7.2)
- [x] `Ready()` — 本地 Serve 未启用时恒 true; 启用后意外退出 → false (unhealthy, docs §7.2); Stop 后 false (progress #19 验证)
- [x] `Stop()` / `Done()` — 同步幂等 cancelRun；关闭每条已建立的 *Client（Close 幂等）+ 置 handle nil；upstreamWG.Wait 等 runUpstream goroutine 退出；close(done) + ready=false；二次 Stop 返 cache error
- [x] `runUpstream` heartbeat ticker — stdio auto_start 成功后启动每 entry 唯一 goroutine（30s ticker + 10s Ping timeout）；client.Done() 或 Ping 失败时按 generation compare-and-clear 将 handle.Store(nil)+status=Error+关闭 client；Stop 经 upstreamWG.Wait 同步退出全部 goroutine
- [x] `runUpstream` 重连 (Step 2 已落地) — Ping/Done() 失败后按 mcp.reconnect 指数退避 (initial * 2^(attempt-1) cap max) 由同一 goroutine 重连创建新 Client (重新 Connect+Initialize+完整 DiscoverTools)；Tool 三元 (canonical name + description + canonical-marshal InputSchema) 精确一致才原子替换 handle (handle.Store + entry 锁下递增 generation)；差异保持 unavailable + LastError 记 catalog drift; max_attempts 耗尽后保持 Error 要求 Runtime 重启
- [x] `runUpstream` listChanged 事件路径 (Step 3 已落地) — `notifications/tools/list_changed` notification 由 Client.onListChanged 非阻塞投递到该代独有 listChanged cap-1 channel (旧代不复用); runUpstream select 命中 → catalogReconcile 用当前代 Client 完整 DiscoverTools + 三元严格比对; 一致保持 Connected/不替换 client; 不一致关闭该 Client + handle.Store(nil) + 标 ErrMCPProtocolError 保持 error (不可自愈, 要求 Runtime 重启)

## 2. MCP Client

- [x] `Client` 结构体定义（name、单代 transport、status、mu 与 cancel/done lifecycle）
- [x] `Connect()` — 建立连接（先 transport.Start 再 startLoops，避免 StdioClient recvReady 阻塞 race）
- [x] `Close()` — 关闭连接（closeOnce + wg.Wait + status 复位 disconnected）
- [x] `Initialize()` — MCP 握手协议（initialize 请求 + 校验 protocolVersion + capabilities.tools + notifications/initialized）
- [x] `DiscoverTools()` — 从空 cursor 完整分页获取并规范化 Tool catalog（128 pages / 4096 tools / 4 KiB cursor 上限保护）
- [x] `CallTool()` — 调用 Server 端 Tool（State 校验 + 严格 wire DTO）
- [x] `Done()` / `Err()` — 向 Manager 无损报告该代连接终止
- [x] Resource / Prompt 不发现、不注册、不调用（v1 不实现 resource/prompt capability）
- [x] `Ping()` — 心跳检测
- [x] 连接状态枚举（Disconnected, Connecting, Connected, Error）
- [x] 请求超时控制（ctx 取消 + failClose；clients/runUpstream 层再加 per-tool hard cap）

### §2 Client 11 项全部实现 + 副债

v1 副债：listTools 严格 DTO 的 DisallowUnknownFields（当前 request Unmarshal 包含 EOF 检查）。onListChanged 已在 Step 3 接入并向该代独有 listChanged channel 非阻塞投递.

## 3. MCP Server（Yaa! 作为 Server）

- [x] `MCPServer` 结构体定义（tools、agentID、exposed、transport） — progress #19 落地 (internal/mcp/server.go)
- [x] `Serve(ctx)` — 阻塞运行已 prepared 的 Server transport — `MCPServer.Serve` 调 `transport.Serve(ctx, handle)`
- [x] `Close()` — 幂等关闭 transport并解除 Serve — `MCPServer.Close` 调 `transport.Close()`
- [x] `handleInitialize()` — 响应客户端 initialize 请求 — `handleInitialize` decodeParams + serverVersion + Negotiate + InitializeResult
- [x] `handleListTools()` — 响应 tools/list 请求 — `listTools` cursor 分页 + digest 校验 + offset 页边界
- [x] `handleCallTool()` — 响应 tools/call 请求 — `handleCallTool` decodeParams + exposed check + ToolManager.Execute + toMCPResult/ErrorResult
- [x] Resource / Prompt request 返回 JSON-RPC `-32601` — handle dispatch default 分支
- [x] `handlePing()` — 响应 ping 请求 — CanPing 检查 + 空 result
- [x] Server 信息声明（name, version, capabilities） — ServerInfo{name:"yaa", version:runtimeVersion="0.1.0"}, Capabilities{tools:{listChanged:false}}
- [x] 会话管理（多客户端连接隔离） — stdio 单 session (progress #19) + SSEServer 多 session session_id (progress #20) + StreamableHTTPServer 多 session by 32-byte Mcp-Session-Id (session map + 30min idle sweep + 1024 上限 + DELETE 销毁) (progress #21) 全部已交付

## 4. Transport — stdio

- [x] `StdioClient` / `StdioServer` 结构体定义（cmd, stdin, stdout） — `StdioClient` 已落地（Start/Send/Recv/Close/Info 实现 ClientTransport）；`StdioServer` 已落地 progress #19 (internal/mcp/server_transport.go, 可注入 io.Reader/io.Writer)
- [x] 子进程启动（`exec.Command`） —— `cmd := exec.Command(command, args...)` + cmd.Env 注入
- [x] stdin/stdout 管道建立 —— cmd.StdinPipe / StdoutPipe / StderrPipe + cmd.Start
- [x] JSON-RPC 消息读写（行分隔） —— Send: json.Marshal + 写 stdin 加行尾；Recv: bufio.Reader.ReadString('\n') → json.Unmarshal
- [x] stderr 日志捕获与转发 —— pumpStderr goroutine 行级转发至 slog.Info（带 server 标签，不混入协议流）
- [x] 子进程退出检测与通知 —— Close 关 stdin → process.Wait with 5s timeout → Kill；开启 recvLoop exit on transport close
- [x] 子进程环境变量注入 —— composeStdioEnv：白名单 PATH/HOME/USER/LANG/LC_ALL + 用户 env 注入（docs/mcp/integration.md §7 过滤）
- [x] 优雅关闭（关闭 stdin → 等待退出 → 超时 kill；不发送未定义的 `shutdown` RPC） —— CloseClose 先 stdin.Close → wait with stdioCloseGraceTimeout=5s → cmd.Process.Kill

### §4 Client 8 项全部实现 + 集成测试覆盖

集成：fake stdio MCP server（python3 inline 脚本）+ Client 端到端 lifecycle / CallTool / Send 超 4 MiB / 不存在的命令 / Close 幂等 / Info 状态 / 子进程 kill → Recv / env 注入通过 serverInfo.name 回显 = 8 例全绿

## 5. Transport — SSE

- [x] `SSEClient` 结构体定义（URL、HTTP client、event stream） — `SSEClient` 已落地（Start/Send/Recv/Close/Info); `SSEServer` 已落地 progress #20 (internal/mcp/sse_server.go) 多 session by session_id + endpoint 帧 + heartbeat 30s + Last-Event-ID 接收 (v1 不续传与 SSEClient 决策一致)
- [x] SSE 连接建立（GET + Accept: text/event-stream） — `Start` 发 GET, Accept: text/event-stream, 解析首帧 event:endpoint
- [x] 事件流解析（data: / event: / id: 字段） — `readSSEFrame` 支持多行 data:, event:, id:, comment heartbeat (`: ping`), 空行结束 frame
- [x] POST 请求发送 JSON-RPC 消息 — `Send` POST 到 endpoint, Content-Type: application/json, Accept: application/json, text/event-stream; 非 2xx → ErrMCPTransportWrite
- [x] `Last-Event-ID` 解析/续传；事件流恢复不得 replay request — lastID 字段记录 id:; v1 不实现重连/续传 (Manager attemptReconnect 会重新 initialize 完成新代 catalog), Send 不重放已发 request
- [x] 连接超时与心跳处理 — 启动由 startupCtx 控制; 流上 comment heartbeat 忽略; Recv ctx 取消优先返 ctx.Err()
- [ ] SSE 事件分发（message, error, close） — v1 仅处理 message (SSE 标准 default event); error/close 等自定义事件不在本 commit 范围 (docs §3.2 未明确表示)
- [x] HTTPS 与 `tls.ca_file` 校验（不提供 `insecure_skip_verify`） — 调用方在 http.Client 配置 TLS; SSEClient struct 不引入 insecure_skip_verify, docs §5 明示不提供

## 6. Transport — Streamable HTTP

- [x] Client transport: `StreamableHTTPClient` 实现 ClientTransport (Server transport 待 §3 本地 Server)
- [x] POST JSON-RPC，Accept 支持 JSON 与 SSE — POST Accept: application/json, text/event-stream; Content-Type: application/json; response body JSON 单对象或 SSE 流多帧解析 parseSSEFrame
- [x] 可选 `Mcp-Session-Id`：上游未返回时 Client 保持 stateless；获得时后续 POST 必带 — `sessionID` 字段记录; NewStreamableHTTPClient 不强制要求 server 返 (stateless 模式)
- [x] 同一 endpoint 的 POST/GET/DELETE 语义 — v1 POST 同步响应 (stateless + session 复用) + initialize 拿到 Mcp-Session-Id 后 async GET 试探 Server-to-Client SSE 流 (200+text/event-stream → runSSERecvLoop 投 recvCh; 405/非 SSE → graceful 退出不影响 POST) + Close cancel SSE loop + 发一次 DELETE 终止 session (404/405 幂等忽略) (progress #22)
- [x] TLS 与认证 Header 注入 — http.Client 由调用方配 TLS (Manager 用 defaults); headers 注入每条 POST
- [x] 任何已经发送的 `tools/call` 都不自动重放 — Send POST 无重试; 失败直接 recvCh err; Manager attemptReconnect 路径重建 Client 重新 initialize + Discover 才服务新调用

### §5/§6 v1 transport 完整度
- Client side: stdio + SSE + StreamableHTTP 全部完成 ClientTransport 接口
- Manager buildTransport 选 transport by config; Prepare 走 stdio/sse/streamable_http 同 connectAndDiscover+registerProxies+publishGeneration+runUpstream path
- Server side: SSEServer 已落地 progress #20 (listener + endpoint GET SSE 帧 + POST messages 入站 + session map + heartbeat 30s + 端到端测试 7 例); StreamableHTTPServer 已落地 progress #21 (listener + single endpointPath 处理 POST/GET/DELETE + 32-byte crypto/rand URL-safe ID + 1024 上限 503 + 30min idle sweeper + DELETE 204 销毁 + GET 405 only-close SSE v1 + Origin allowlist 403 DNS-rebinding 防护 + batch 数组 400 + 缺 ID 400 + 未知 ID 404 + 数组/batch + 413 + 端到端测试 10 例)
- StreamableHTTP 状态映射 docs §3.3 错误表完整覆盖 14 子用例 (401/403 Auth / init 404/405 Config / init 408/504 ConnTimeout / init 429/5xx Unavailable / business POST 5xx/429 TransportWrite / session-POST 400/404/410 TransportClosed / 413 ProtocolError / 3xx CheckRedirect 拒)

## 7. Tool 映射

- [x] MCP Tool → Yaa! Tool 接口适配 — MCPToolProxy 实现 tool.Tool 接口（Name/Description/Parameters/Execute）
- [x] Tool 名称前缀策略（`mcp.<server>.<tool>`） — canonicalToolName + normalizeTool 级校验
- [x] JSON Schema 参数透传 — MCPToolProxy.Parameters 深拷贝不可变 schema
- [x] Tool 调用结果转换（MCP result → Yaa! ToolResult） — toToolResult 多 text 块以 \\n 连接 + isError 透传
- [x] Tool 错误处理（MCP wire err 透传 + mapRPCError 把 -32601 → ErrMCPToolNotFound，其余 → ErrMCPToolExecFailed）
- [x] Tool 首次发现后注册稳定 Proxy；断线置 unavailable 不动态注销 — Manager.Stop 置 handle.Store(nil)，下一调用返 ErrMCPUnavailable
- [x] 同一上游全部 Proxy 共享一个 `ProxyHandle`；批量注册中途失败关 Client + 标 LastError + Status=Error（收敛，未实现 ToolManager Unregister 回滚，YAGNI 留后续 commit）
- [x] 重连仅在 Tool 名称、description、input schema 精确一致时原子替换 client handle — catalogMatches: canonical name + description + canonical-marshal InputSchema 三元严格比对（不比分页/key 顺序）
- [x] Tool 调用超时透传 — MCPToolProxy.timeout 通过 WithCancelCause + AfterFunc 实施 hard cap（Go 1.20 无 WithTimeoutCause）
- [x] 大量 Tool 的分页与过滤 — DiscoverTools 已完整分页至 4096 tools 上限；过滤按 transport 阶段 normalizeTool 完成

## 8. 配置

- [ ] 全局 MCP 配置（`mcp.*` in yaa.yaml）
- [ ] `mcp.servers[]` 配置字段（name, transport, command, args, env, headers, tls, url, timeout, auto_start）
- [ ] 本地 `mcp.server` 配置字段（enabled, agent_id, transport, addr, path, messages_path, exposed_tools, origin_allowlist）
- [ ] Agent 级别只通过 `agents[].tools` 引用 `mcp.<server>.<tool>`，不增加隐含 `agents[].mcp` 字段
- [ ] `mcp.*` 变更返回 `restart_required`，重启后按 `auto_start` 连接
- [ ] 默认超时配置（`mcp.timeout.connect/init/tool`，其中 `tool=0` 只使用 caller deadline）
- [ ] 自动启动与重连配置（`auto_start`, `mcp.reconnect.*`）

## 9. 集成

- [x] 与 Tool Manager 集成 — Prepare 调 m.tm.Register(MCPToolProxy) 注册每个发现的 Tool；Tools(name) 返 ToolInfo 深拷贝给调用方
- [x] MCP Tool Proxy 在 Skill / Config.Activate 之前注册 — Runtime 启动序 MCP Prepare 阶段同步完成（internal/runtime §Prepare 先于 Skill Load 与 Activate）
- [ ] 与 Session 集成（MCP Tool 在 Session 上下文中可用）
- [ ] 与 Provider 集成（MCP Tool 作为 Function 暴露给 LLM）
- [x] Remote API: `GET /api/v1/mcp/servers` — 列出 MCP Server (handler handleListMCPServers + mcpServerListData{items}; Manager.List 投影; routes 已注册; 测试 TestMCPEndpointsReturn200And404)
- [x] Remote API: `GET /api/v1/mcp/servers/:name` — 获取 MCP Server 详情 (ServerDetail = ServerStatus 嵌入 + Tools []tool.ToolInfo; Manager.Detail 一次拼装; handler 返 ServerDetail 非 ServerStatus; 测试 TestMCPEndpointsDetailWithTools 覆盖 wire 投影)
- [x] Remote API 不提供 MCP Server 动态 CRUD 或直接 Tool 调用 (handler 只 GET 只读; 无 POST/PUT/DELETE 路由)
- [x] Runtime shutdown — MCP Manager.Stop(cancelRun + 关闭 clients + close done)；Runtime 已等 Done 后以 context.Background() 再调 Stop 拿 cacheErr，再 fallback 关 tool Manager
- [x] 本地 MCP Server 使用 `mcp.server.agent_id` 调用 Tool Manager，并校验 Agent Tool 白名单 — NewMCPServerRaw 构造时调 tools.ListForAgent(cfg.AgentID) 取 allowAll 集; ExposedTools 每项必须 enabled + 通过 Agent allowlist, 否则 ErrMCPConfig (server.go §NewMCPServer); handleCallTool 走 s.tools.Execute(scope{AgentID:s.agentID, SessionID:""}) 二次校验 (server.go §handleCallTool); 测试 TestNewMCPServerRejectsExposedToolNotInAgentAllowlist (allowlist=["echo"], ExposedTools=["echo","private"] → ErrMCPConfig 含 "private"+"restricted") + TestNewMCPServerAcceptsAllExposedToolsInAllowlist (正向 Echo+Ls 全过)
- [ ] 内置 Tool: `mcp_list` — 列出 MCP Server
- [ ] 指标按 `observability.md` 唯一表实现（`yaa_mcp_servers`, `yaa_mcp_tool_calls_total`, `yaa_mcp_tool_call_duration_seconds`, `yaa_mcp_reconnects_total`, `yaa_mcp_tools`）
- [ ] 调用链追踪（span: mcp.call_tool, mcp.list_tools）
