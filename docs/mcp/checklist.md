# MCP 实现检查清单

> 文档路径: `docs/mcp/checklist.md`
> 上级: `docs/mcp/README.md`

---

## 1. MCP Manager

- [ ] `Manager` 结构体定义（clients map, configs map, logger, mu）
- [ ] `Register()` — 注册 MCP Server 配置
- [ ] `Unregister()` — 注销 MCP Server
- [ ] `Get()` — 查找 MCP Client
- [ ] `List()` — 列出所有已注册 MCP Server
- [ ] `Start()` — 启动所有 MCP Client 连接
- [ ] `Stop()` — 停止所有 MCP Client 连接
- [ ] `Restart()` — 重启指定 MCP Client
- [ ] `HealthCheck()` — 健康检查（检测所有 Client 连接状态）
- [ ] `GetTools()` — 聚合所有 MCP Server 暴露的 Tool
- [ ] `GetPrompts()` — 聚合所有 MCP Server 暴露的 Prompt
- [ ] `GetResources()` — 聚合所有 MCP Server 暴露的 Resource
- [ ] `GetStats()` — 获取 MCP 运行时统计

## 2. MCP Client

- [ ] `Client` 结构体定义（config, transport, conn, status, logger）
- [ ] `Connect()` — 建立连接
- [ ] `Close()` — 关闭连接
- [ ] `Initialize()` — MCP 握手协议（initialize 请求）
- [ ] `ListTools()` — 获取 Server 端 Tool 列表
- [ ] `CallTool()` — 调用 Server 端 Tool
- [ ] `ListPrompts()` — 获取 Server 端 Prompt 列表
- [ ] `GetPrompt()` — 获取指定 Prompt 内容
- [ ] `ListResources()` — 获取 Server 端 Resource 列表
- [ ] `ReadResource()` — 读取指定 Resource 内容
- [ ] `Ping()` — 心跳检测
- [ ] 连接状态枚举（Disconnected, Connecting, Connected, Error）
- [ ] 自动重连逻辑（指数退避，最大重试次数可配）
- [ ] 请求超时控制（per-request timeout）

## 3. MCP Server（Yaa! 作为 Server）

- [ ] `Server` 结构体定义（mgr, transport, sessions, logger）
- [ ] `Start()` — 启动 Server 监听
- [ ] `Stop()` — 停止 Server
- [ ] `handleInitialize()` — 响应客户端 initialize 请求
- [ ] `handleListTools()` — 响应 tools/list 请求
- [ ] `handleCallTool()` — 响应 tools/call 请求
- [ ] `handleListPrompts()` — 响应 prompts/list 请求
- [ ] `handleGetPrompt()` — 响应 prompts/get 请求
- [ ] `handleListResources()` — 响应 resources/list 请求
- [ ] `handleReadResource()` — 响应 resources/read 请求
- [ ] `handlePing()` — 响应 ping 请求
- [ ] Server 信息声明（name, version, capabilities）
- [ ] 会话管理（多客户端连接隔离）

## 4. Transport — stdio

- [ ] `StdioTransport` 结构体定义（cmd, stdin, stdout）
- [ ] 子进程启动（`exec.Command`）
- [ ] stdin/stdout 管道建立
- [ ] JSON-RPC 消息读写（行分隔）
- [ ] stderr 日志捕获与转发
- [ ] 子进程退出检测与通知
- [ ] 子进程环境变量注入
- [ ] 工作目录配置支持
- [ ] 优雅关闭（发送 shutdown → 等待退出 → kill）

## 5. Transport — SSE

- [ ] `SSETransport` 结构体定义（url, httpClient, eventChan）
- [ ] SSE 连接建立（GET + Accept: text/event-stream）
- [ ] 事件流解析（data: / event: / id: 字段）
- [ ] POST 请求发送 JSON-RPC 消息
- [ ] 自动重连（Last-Event-ID 续传）
- [ ] 连接超时与心跳处理
- [ ] SSE 事件分发（message, error, close）
- [ ] HTTPS / 自签名证书支持

## 6. Transport — WebSocket

- [ ] `WSTransport` 结构体定义（url, conn, mu）
- [ ] WebSocket 握手（Dialer + 子协议协商）
- [ ] 文本帧读写（JSON-RPC over WS）
- [ ] Ping/Pong 心跳机制
- [ ] 连接关闭码处理
- [ ] 自动重连逻辑
- [ ] WSS（TLS）支持
- [ ] 自定义请求头注入（Authorization 等）

## 7. Tool 映射

- [ ] MCP Tool → Yaa! Tool 接口适配
- [ ] Tool 名称前缀策略（`<server>.<tool>` 避免冲突）
- [ ] JSON Schema 参数透传
- [ ] Tool 调用结果转换（MCP result → Yaa! ToolResult）
- [ ] Tool 错误处理（MCP error code → Yaa! error）
- [ ] Tool 动态注册（MCP Client 连接后自动注册）
- [ ] Tool 动态注销（MCP Client 断开后自动注销）
- [ ] Tool 调用超时透传
- [ ] 大量 Tool 的分页与过滤

## 8. 配置

- [ ] 全局 MCP 配置（`mcp.*` in config.yaml）
- [ ] per_server 覆盖（`mcp.servers.<name>.*`）
- [ ] Server 配置字段（type, command, args, env, url, timeout, auto_start）
- [ ] Agent 级别 MCP 白名单（`agents[].mcp: [...]`）
- [ ] Agent 级别 MCP 黑名单（`agents[].mcp.deny: [...]`）
- [ ] 配置热更新支持
- [ ] 默认超时配置（request_timeout, dial_timeout）
- [ ] 自动启动配置（auto_start, default enabled）

## 9. 集成

- [ ] 与 Tool Manager 集成（MCP Tool 注册到 Tool Manager）
- [ ] 与 Skill Manager 集成（Skill 可声明依赖 MCP Server）
- [ ] 与 Session 集成（MCP Tool 在 Session 上下文中可用）
- [ ] 与 Provider 集成（MCP Tool 作为 Function 暴露给 LLM）
- [ ] Remote API: `GET /api/v1/mcp/servers` — 列出 MCP Server
- [ ] Remote API: `POST /api/v1/mcp/servers` — 注册 MCP Server
- [ ] Remote API: `DELETE /api/v1/mcp/servers/:name` — 注销 MCP Server
- [ ] Remote API: `POST /api/v1/mcp/servers/:name/restart` — 重启 MCP Server
- [ ] Remote API: `GET /api/v1/mcp/servers/:name/health` — 健康检查
- [ ] Remote API: `GET /api/v1/mcp/servers/:name/tools` — 列出 Tool
- [ ] SSE 事件: `mcp.server.connected`
- [ ] SSE 事件: `mcp.server.disconnected`
- [ ] SSE 事件: `mcp.server.error`
- [ ] 内置 Tool: `mcp_list` — 列出 MCP Server
- [ ] 内置 Tool: `mcp_restart` — 重启 MCP Server
- [ ] 指标: `mcp_server_total` (Gauge)
- [ ] 指标: `mcp_tool_calls_total` (Counter)
- [ ] 指标: `mcp_tool_call_duration` (Histogram)
- [ ] 指标: `mcp_reconnect_total` (Counter)
- [ ] 调用链追踪（span: mcp.call_tool, mcp.list_tools）
