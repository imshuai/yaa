# MCP 实现检查清单

> 文档路径: `docs/mcp/checklist.md`
> 上级: `docs/mcp/README.md`

---

## 1. MCP Manager

- [ ] `Manager` 结构体定义（entries、Tool Manager、本地 MCPServer、run/stop context、done、logger、mu）
- [ ] `Get()` / `Tools()` — 返回状态和 Tool 深拷贝，不暴露当前 Client
- [ ] `List() []ServerStatus` — 列出所有配置的上游连接状态
- [ ] `Prepare()` — 校验/持有本地 transport，启动 auto-start Client 并注册稳定 Proxy，但不运行本地 Serve
- [ ] `Activate()` — 仅在 `Config.Activate(binding)` 成功后运行本地 MCP Server
- [ ] `Ready()` — 本地 Serve 意外退出后返回 false，推动 Runtime unhealthy/Not Ready
- [ ] `Stop()` / `Done()` — 同步清空 handles，后台用 `errors.Join` 完成 teardown；Done 后再次 Stop 立即返回缓存的最终错误
- [ ] `runUpstream` — Manager 唯一拥有 heartbeat、catalog reconciliation 和指数退避重连

## 2. MCP Client

- [ ] `Client` 结构体定义（name、单代 transport、status、mu 与 cancel/done lifecycle）
- [ ] `Connect()` — 建立连接
- [ ] `Close()` — 关闭连接
- [ ] `Initialize()` — MCP 握手协议（initialize 请求）
- [ ] `DiscoverTools()` — 从空 cursor 完整分页获取并规范化 Tool catalog
- [ ] `CallTool()` — 调用 Server 端 Tool
- [ ] `Done()` / `Err()` — 向 Manager 无损报告该代连接终止
- [ ] Resource / Prompt 不发现、不注册、不调用
- [ ] `Ping()` — 心跳检测
- [ ] 连接状态枚举（Disconnected, Connecting, Connected, Error）
- [ ] 请求超时控制（per-request timeout）

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

- [ ] `StdioClient` / `StdioServer` 结构体定义（cmd, stdin, stdout）
- [ ] 子进程启动（`exec.Command`）
- [ ] stdin/stdout 管道建立
- [ ] JSON-RPC 消息读写（行分隔）
- [ ] stderr 日志捕获与转发
- [ ] 子进程退出检测与通知
- [ ] 子进程环境变量注入
- [ ] 优雅关闭（关闭 stdin → 等待退出 → 超时 kill；不发送未定义的 `shutdown` RPC）

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

- [ ] MCP Tool → Yaa! Tool 接口适配
- [ ] Tool 名称前缀策略（`mcp.<server>.<tool>` 避免冲突）
- [ ] JSON Schema 参数透传
- [ ] Tool 调用结果转换（MCP result → Yaa! ToolResult）
- [ ] Tool 错误处理（MCP error code → Yaa! error）
- [ ] Tool 首次发现后注册稳定 Proxy；暂时断线置 unavailable，不动态注销
- [ ] 同一上游全部 Proxy 共享一个 `ProxyHandle`；初始批量注册失败完整回滚
- [ ] 重连仅在 Tool 名称、description、input schema 精确一致时原子替换 client handle
- [ ] Tool 调用超时透传
- [ ] 大量 Tool 的分页与过滤

## 8. 配置

- [ ] 全局 MCP 配置（`mcp.*` in yaa.yaml）
- [ ] `mcp.servers[]` 配置字段（name, transport, command, args, env, headers, tls, url, timeout, auto_start）
- [ ] 本地 `mcp.server` 配置字段（enabled, agent_id, transport, addr, path, messages_path, exposed_tools, origin_allowlist）
- [ ] Agent 级别只通过 `agents[].tools` 引用 `mcp.<server>.<tool>`，不增加隐含 `agents[].mcp` 字段
- [ ] `mcp.*` 变更返回 `restart_required`，重启后按 `auto_start` 连接
- [ ] 默认超时配置（`mcp.timeout.connect/init/tool`，其中 `tool=0` 只使用 caller deadline）
- [ ] 自动启动与重连配置（`auto_start`, `mcp.reconnect.*`）

## 9. 集成

- [ ] 与 Tool Manager 集成（MCP Tool 注册到 Tool Manager）
- [ ] MCP Tool Proxy 在 Skill binding 前注册（Skill 只声明 Tool 名称依赖）
- [ ] 与 Session 集成（MCP Tool 在 Session 上下文中可用）
- [ ] 与 Provider 集成（MCP Tool 作为 Function 暴露给 LLM）
- [ ] Remote API: `GET /api/v1/mcp/servers` — 列出 MCP Server
- [ ] Remote API: `GET /api/v1/mcp/servers/:name` — 获取 MCP Server 详情
- [ ] Remote API 不提供 MCP Server 动态 CRUD 或直接 Tool 调用
- [ ] Runtime 调用 `MCP Manager.Stop(ctx)` 后等待 `Done()`，再以 fresh context 调用 Stop 取得最终错误，之后关闭 Tool Manager 等依赖
- [ ] 本地 MCP Server 使用 `mcp.server.agent_id` 调用 Tool Manager，并校验 Agent Tool 白名单
- [ ] 内置 Tool: `mcp_list` — 列出 MCP Server
- [ ] 指标按 `observability.md` 唯一表实现（`yaa_mcp_servers`, `yaa_mcp_tool_calls_total`, `yaa_mcp_tool_call_duration_seconds`, `yaa_mcp_reconnects_total`, `yaa_mcp_tools`）
- [ ] 调用链追踪（span: mcp.call_tool, mcp.list_tools）
