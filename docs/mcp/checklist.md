# MCP 实现检查清单

> 文档路径: `docs/mcp/checklist.md`
> 上级: `docs/mcp/README.md`

---

## 1. MCP Manager

- [x] `Manager` 结构体定义（entries、run/stop context、done、logger、mu） — v1 起点已落地；Tool Manager 字段已签名透传，待后续 commit 接 Client / Proxy 后实际使用；本地 MCPServer 字段尚未引入（待 §3 commit）
- [x] `Get()` / `Tools()` — 返回 ServerStatus 副本（含 ConnectedAt 指针深拷贝）+ Tool 列表深拷贝；Tools 在 server 未连接或无 Tool 时返 (nil, false)
- [x] `List() []ServerStatus` — 列出所有配置的上游连接状态（从 status 字段投影，Prepare 后含 connected / error 真实状态）
- [x] `Prepare()` — 自动启动 stdio auto_start Client 并注册稳定 Proxy（Connect → Initialize → DiscoverTools → Register MCPToolProxy → 启 runUpstream heartbeat goroutine）；SSE / Streamable HTTP 待 §5/§6 commit；单 server 失败仅标 LastError + Status=Error, 不阻断其他
- [x] `Activate()` — v1：仅当 cfg.Server.Enabled=true 时返 ErrMCPConfig（本地 Serve 实现未交付不静默启用）；disabled 时返 nil
- [x] `Ready()` — v1 恒 true（无本地 Serve），Stop 后置 false；本地 Serve 意外退出→ unhealthy 待 §3 commit
- [x] `Stop()` / `Done()` — 同步幂等 cancelRun；关闭每条已建立的 *Client（Close 幂等）+ 置 handle nil；upstreamWG.Wait 等 runUpstream goroutine 退出；close(done) + ready=false；二次 Stop 返 cache error
- [x] `runUpstream` heartbeat ticker — stdio auto_start 成功后启动每 entry 唯一 goroutine（30s ticker + 10s Ping timeout）；client.Done() 或 Ping 失败时按 generation compare-and-clear 将 handle.Store(nil)+status=Error+关闭 client；Stop 经 upstreamWG.Wait 同步退出全部 goroutine
- [ ] `runUpstream` 重连 + catalog reconciliation — 仍待后续 commit：Ping/Done() 失败后按 mcp.reconnect 指数退避创建新 Client（重新 Initialize + 完整 DiscoverTools）；比对 Tool 三元（name+description+inputSchema）精确一致才原子替换 handle, 差异保持 unavailable + ErrMCPProtocolError 要求重启；listChanged channel 启合并 tools/list_changed 通知触发完整 DiscoverTools

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

v1 副债：onListChanged callback（tools/list_changed 目前容忍无声 callback）；listTools 严格 DTO 的 DisallowUnknownFields（当前 request Unmarshal 包含 EOF 检查）。

## 3. MCP Server（Yaa! 作为 Server）

- [ ] `MCPServer` 结构体定义（tools、agentID、exposed、transport）
- [ ] `Serve(ctx)` — 阻塞运行已 prepared 的 Server transport
- [ ] `Close()` — 幂等关闭 transport并解除 Serve
- [ ] `handleInitialize()` — 响应客户端 initialize 请求
- [ ] `handleListTools()` — 响应 tools/list 请求
- [ ] `handleCallTool()` — 响应 tools/call 请求
- [ ] Resource / Prompt request 返回 JSON-RPC `-32601`
- [ ] `handlePing()` — 响应 ping 请求
- [ ] Server 信息声明（name, version, capabilities）
- [ ] 会话管理（多客户端连接隔离）

## 4. Transport — stdio

- [x] `StdioClient` / `StdioServer` 结构体定义（cmd, stdin, stdout） — `StdioClient` 已落地（Start/Send/Recv/Close/Info 实现 ClientTransport）；`StdioServer` 待 §3 本地 MCP Server commit
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

- [ ] `SSEClient` / `SSEServer` 结构体定义（URL、HTTP client、event stream）
- [ ] SSE 连接建立（GET + Accept: text/event-stream）
- [ ] 事件流解析（data: / event: / id: 字段）
- [ ] POST 请求发送 JSON-RPC 消息
- [ ] `Last-Event-ID` 解析/续传；事件流恢复不得 replay request
- [ ] 连接超时与心跳处理
- [ ] SSE 事件分发（message, error, close）
- [ ] HTTPS 与 `tls.ca_file` 校验（不提供 `insecure_skip_verify`）

## 6. Transport — Streamable HTTP

- [ ] Client/Server transport 接口分离
- [ ] POST JSON-RPC，Accept 支持 JSON 与 SSE
- [ ] 可选 `Mcp-Session-Id`：上游未返回时 Client 保持 stateless；Yaa Server 固定签发
- [ ] 同一 endpoint 的 POST/GET/DELETE 语义（含 `Mcp-Session-Id`、202/404/405）
- [ ] TLS 与认证 Header 注入
- [ ] 任何已经发送的 `tools/call` 都不自动重放

## 7. Tool 映射

- [x] MCP Tool → Yaa! Tool 接口适配 — MCPToolProxy 实现 tool.Tool 接口（Name/Description/Parameters/Execute）
- [x] Tool 名称前缀策略（`mcp.<server>.<tool>`） — canonicalToolName + normalizeTool 级校验
- [x] JSON Schema 参数透传 — MCPToolProxy.Parameters 深拷贝不可变 schema
- [x] Tool 调用结果转换（MCP result → Yaa! ToolResult） — toToolResult 多 text 块以 \\n 连接 + isError 透传
- [x] Tool 错误处理（MCP wire err 透传 + mapRPCError 把 -32601 → ErrMCPToolNotFound，其余 → ErrMCPToolExecFailed）
- [x] Tool 首次发现后注册稳定 Proxy；断线置 unavailable 不动态注销 — Manager.Stop 置 handle.Store(nil)，下一调用返 ErrMCPUnavailable
- [x] 同一上游全部 Proxy 共享一个 `ProxyHandle`；批量注册中途失败关 Client + 标 LastError + Status=Error（收敛，未实现 ToolManager Unregister 回滚，YAGNI 留后续 commit）
- [ ] 重连仅在 Tool 名称、description、input schema 精确一致时原子替换 client handle
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
- [ ] Remote API: `GET /api/v1/mcp/servers` — 列出 MCP Server
- [ ] Remote API: `GET /api/v1/mcp/servers/:name` — 获取 MCP Server 详情
- [ ] Remote API 不提供 MCP Server 动态 CRUD 或直接 Tool 调用
- [x] Runtime shutdown — MCP Manager.Stop(cancelRun + 关闭 clients + close done)；Runtime 已等 Done 后以 context.Background() 再调 Stop 拿 cacheErr，再 fallback 关 tool Manager
- [ ] 本地 MCP Server 使用 `mcp.server.agent_id` 调用 Tool Manager，并校验 Agent Tool 白名单
- [ ] 内置 Tool: `mcp_list` — 列出 MCP Server
- [ ] 指标按 `observability.md` 唯一表实现（`yaa_mcp_servers`, `yaa_mcp_tool_calls_total`, `yaa_mcp_tool_call_duration_seconds`, `yaa_mcp_reconnects_total`, `yaa_mcp_tools`）
- [ ] 调用链追踪（span: mcp.call_tool, mcp.list_tools）
