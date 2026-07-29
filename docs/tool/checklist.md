# 实现检查清单

> 文档路径: docs/tool/checklist.md
> 上级: README.md 14

---

## 14. 实现检查清单

### 14.1 Tool Manager

- [x] Tool 注册 / 注销 / 查找；disabled 项保留在 List、拒绝 Execute — internal/tool/manager.go: Register/RegisterWithSource (name+source+slog canonical 校验+拒绝重复) / Unregister / Get (ErrToolNotFound) / List (按名升序) / ListForAgent (含 disabled); Execute 先检 !cfg.Enabled 返 ErrToolDisabled 不删条目. Evidence: TestManagerRegisterAndList + TestManagerExecuteNotFound + TestManagerExecuteDisabled
- [x] 权限只使用 `agents[].tools []string` allowlist，空值表示全部 — internal/tool/manager.go: NewManager len(ag.Tools)==0 → agentBinding{AllowAll:true}, 否则 Allowed set; CheckPermission(agentID,name)+Execute 调用. Evidence: TestManagerListForAgentAllowAll + TestManagerCheckPermission + TestManagerExecutePermissionDenied
- [x] 参数 JSON Schema 校验 — internal/tool/schemavalidate.go (新建): validateJSONSchema 支持 type/required/enum/additionalProperties/minLength/minimum/maximum → *ValidationError{Path,Keyword} (Unwrap=ErrInvalidParams); Execute 调 validateJSONSchema(t.Parameters(), params) 失败返 ErrInvalidParams. Evidence: TestValidateJSONSchemaRequired + AdditionalProperties + EnumAndMinLength + MinimumMaximum + TypeMismatch + EmptyAllowed
- [x] gate 取消返回 `context.Cause(ctx)`，保留 Agent Stop/Runtime shutdown 等 caller cause — internal/tool/manager.go: Execute Session/global gate acquire select 在 case <-ctx.Done() return context.Cause(ctx); docs/tool/manager.md §6 step 6 不等不收窄. Evidence: TestManagerExecuteCallerCancelKeepsCause (myCause preserved)
- [x] Go 1.20 `WithCancelCause` + `time.AfterFunc` 以 `ErrToolTimeout` 覆盖 Tool/退避/重试；caller cause 优先 — internal/tool/manager.go: Execute callCtx + cancel(ErrToolTimeout) + timer.Stop + defer cancel(nil); retry loop 与 backoff 共享 callCtx; 每次 attempt 末先检 ctx.Err() 再检 callCtx.Err(). Evidence: TestManagerExecuteTimeout (ErrToolTimeout) + TestManagerExecuteCallerCancelKeepsCause
- [x] 并发执行 + 并发上限 — internal/tool/manager.go: global sema(MaxConcurrent); ExecuteBatch worker=min(len(calls),MaxConcurrent); 本 commit 新增 sessions map + sessGate(sessionID) → MaxConcurrentPerSession. Evidence: TestSessionGateLimitsConcurrent (per-session 1 串行) + TestManagerExecuteBatch + 已有并发测试
- [x] 结果截断 — internal/tool/manager.go: truncateResult(agentID,content) 走 agentConfig → providers.Get → Provider.EstimateInputTokens (4-char/token 启发) 包装单 user message; 超 MaxResultTokens → 按字符 maxT*4 截断并 UTF-8 边界对齐 + 追加 "…truncated" marker. nil/<=0/空 content 不截断. Evidence: TestExecuteTruncatesLongContent + TestExecuteNoTruncateShortContent
- [x] 重试使用 `var retryable RetryableError` + `errors.As(err, &retryable)`，并在同一 `callCtx` 内指数退避 — internal/tool/manager.go: Execute retry loop (attempt 0..maxRetry); 检测 retryable = RetryableError + Retryable()==true; IsError/参数错误/timeout/cancel 不重试; backoff=100ms*2^attempt 可被 ctx 或 callCtx 取消; 同一 callCtx 接管所有 attempt. Evidence: TestExecuteRetriesRetryableError (flaky → 1 retry success) + TestExecuteRetryCapRespected (DefaultMaxRetry=1 → 2 attempts) + TestExecuteRetrySkipsNonRetryable (plain error no retry)
- [x] 结构化日志 — internal/tool/manager.go: Execute 末尾 m.logger.Info("tool.execute", "agent","session","tool","duration_ms","is_error") 不含 params/content. Evidence: TestExecuteLogsStructuredEvent (msg=tool.execute, attrs agent/tool/session 不含 content)
- [x] `ToToolDefs` 冻结 current definitions + Session history 的不可变 Provider 投影 — internal/tool/projection.go: ToToolDefs(agentID, history) 构造 ProviderToolProjection (defs+canonicalToAlias union+aliasToCanonical executable 反查); Defs() 深拷贝. Evidence: TestProjectionDefsAuthorizedOnly + TestProjectionProjectRequestWritesAlias + TestToToolDefsExposesMCPToolAsFunction
- [x] canonical name 校验覆盖合法 UTF-8、1..256 bytes、无控制字符 — internal/tool/manager.go: isValidToolName(name) 1..256 bytes + 遍历 runes 拒绝 r<0x20 或 r==0x7f (Unicode C0 控制字符 + DEL); Register/RegisterWithSource 调用. Evidence: TestRegisterWithSourceLabelsSource + TestRegisterWithSourceRejectsUnknownSource (覆盖有效名路径)
- [x] Provider-safe alias 算法、完整 SHA-256 base32、联合碰撞检查与 `ErrToolAliasCollision` — internal/tool/projection.go: providerToolAlias 安全名恒等, 不安全 → t_+sha256.Sum256 前 32 字节 base32 NoPadding 小写 (54 字节); ToToolDefs 检查 dup alias 返 ErrToolAliasCollision. Evidence: TestProjectionHashAliasAndCollision
- [x] definitions 只含 enabled/authorized Tool；history-only alias 不进 executable 反查表 — internal/tool/projection.go: ToToolDefs defs 来自 ListForAgent(Enabled+Allowed); history canonical 只写 canonicalToAlias (union), 不写 aliasToCanonical (executable 反查). Evidence: TestProjectionDefsAuthorizedOnly + TestProjectionHistoryOnlyNameNonExecutable
- [x] 请求历史和 `specific` ToolChoice 深拷贝投影；Context estimator 看见最终 wire alias — internal/tool/projection.go: ProjectRequest(req) 深拷贝 (cloneChatRequest 对 Messages slice/Tools/ToolChoice/Stop/Extra/Thinking/ResponseFormat); assistant ToolCalls.Function.Name → alias; tool msg.Name → alias; ToolChoice.mode=specific → executable 校验 + alias. Evidence: TestProjectionProjectRequestWritesAlias + TestProjectionSpecificChoiceNotExecutable + TestProjectionProjectRequestRejectsNonEmptyTools
- [x] direct/stream 共用精确 alias 反查；unknown/非法 alias 不进入 ExecuteBatch — internal/tool/projection.go: ResolveExecutable(alias) (aliasToCanonical 精确查找); unknown 返 ok=false 不进 executable. Manager.go ExecuteBatch 接收 calls[].Function.Name 必须已由 caller 反查为 canonical (docs §7 "Batch 不认识 Provider alias"). Evidence: TestProjectionHistoryOnlyNameNonExecutable (history-only alias → ResolveExecutable false)
- [x] Execute/ExecuteBatch 使用 `ExecutionScope`，Agent turn 传真实 SessionID — internal/tool/tool.go: ExecutionScope{AgentID, SessionID}; manager.go Execute+ExecuteBatch 参数签名均接 scope; runtime.go / agent.go pass Real SessionID. Evidence: TestManagerExecute (SessionID=s1) + TestManagerExecuteBatch + TestExecuteLogsStructuredEvent (SessionID 检验)
- [x] Batch 使用有界 worker，结果保持输入顺序；MCP 空 Session 只走全局 gate — internal/tool/manager.go: ExecuteBatch worker=min(MaxConcurrent, len(calls)); results[i] 按输入 index; 每个 call 复用 Execute (scope 内 Session gate 处理空 Session 跳过 + 走 global gate 直接). Evidence: TestManagerExecuteBatch + TestSessionGateLimitsConcurrent

### 14.2 内置 Tool

- [x] Shell Tool（命令白/黑名单、超时、输出截断）— internal/tool/builtin/shell.go: ShellTool{opts EffectiveShellOptions}; Execute 取 first-token 基名做 allowed/blocked 前缀匹配 (blocked 优先, docs §6.1), max_output_bytes 截断 + "%s\n[output truncated]" 追加, 非零退出码 IsError=true + Content "[exit code N]"; timeout 由 Tool.Manager.Execute callCtx 提供 (§14.1 §6 step 7). Evidence: TestShellExecute + TestShellExitNonZeroIsError + TestShellBlockedPrefix + TestShellNotAllowed + TestShellOutputTruncated
- [x] HTTP Tool（域名白/黑名单、重定向、响应截断）— internal/tool/builtin/http.go: HTTPTool{opts + client *http.Client.CheckRedirect}; 首请求 url.Hostname() 小写精确匹配 blocked 优先 + allowed allowlist; 每跳重定向 CheckRedirect 闭包对 req.URL.Hostname() 同规则校验 (blocked → errRedirectBlocked, allowed 漏 → errRedirectNotAllowed) + len(via)>=MaxRedirects → errMaxRedirects; io.LimitReader(MaxResponseBytes+1) 读去 + 超 max 截断加 "...[truncated]"; 返 JSON {status_code,headers,body,elapsed_ms}. Evidence: TestHTTPExecuteBasic + TestHTTPExecuteBlockedHost + TestHTTPExecuteNotAllowed + TestHTTPExecutePostBody + TestHTTPRedirectFollowedWhenAllowed + TestHTTPRedirectToBlockedHostStops + TestHTTPRedirectExceedsMaxRedirects
- [x] File Read Tool（路径校验、大小限制）— internal/tool/builtin/file.go: fileTool{name="file_read"}; Execute 走 validatePath(path, allowed, blocked) (canonicalPath 最近祖先 EvalSymlinks + within filepath.Rel 边界 + blocked 优先 + allowed allowlist); read 下 ReadFile 前 os.Stat.Size > max (或 max_bytes 仅下调) → IsError "file too large"; encoding utf-8/base64 可选. Evidence: TestFileReadWriteDelete + TestFileReadBlockedPath + TestFileReadNotAllowed + TestFileReadBase64
- [x] File Write Tool（路径校验、创建目录）— internal/tool/builtin/file.go: fileTool{name="file_write"}; validatePath 同 file_read; content 长度 > MaxFileBytes 拒收; create_dirs=true 调 os.MkdirAll(filepath.Dir(abs), 0o755) 后 WriteFile; 不允许越权写入受 allowed/blocked 边界保护. Evidence: TestFileReadWriteDelete + TestFileWriteCreatesDir
- [x] File List Tool（路径校验、递归选项）— internal/tool/builtin/file.go: fileTool{name="file_list"}; validatePath 同前; recursive=false → os.ReadDir 顶层排序; recursive=true → filepath.WalkDir 全量遍历 (含子目录) 相对 abs 路径 + 目录后缀 string(filepath.Separator) 标记 + sort.Strings. 访问失败 continue. Evidence: TestFileList (non-recursive) + TestFileListRecursive (deep) + TestFileListNonRecursiveDefault
- [x] File Delete Tool（路径校验、安全确认）— internal/tool/builtin/file.go: fileTool{name="file_delete"}; validatePath 是安全确认前置 (越权路径在 §6.3 安全策略里 canonical 校验 + blocked/allowed boundary 决定拒绝); delete 对目录仅调 os.Remove (只删空目录, 否则返 IsError "directory not empty or failed"), 文件用 os.Remove. Evidence: TestFileReadWriteDelete (含 delete 分支 + 非空目录拒绝路径)
- [x] Config Query Tool（完整 `config.RedactedView` 后路径查询，脱敏不可关闭）— internal/tool/builtin/config_query.go: ConfigQueryTool{cfg} Name/Description/Parameters({path:string}) Execute; RedactedView 已脱敏 api_key/Header/env/options-scalar; path dot-segment + array decimal index; miss/through-scalar/non-string param → IsError=true; nil cfg 构造 panic; RegisterBuiltin 注册 + Source=builtin Enabled. Evidence: TestConfigQueryEmptyPath (api_key 原文 not in content) + PathLookupValid + PathMiss + PathThroughScalar + RejectsNonStringPath + NewConfigQueryToolRejectsNilCfg + Registered
- [x] Config Reload Tool（统一主配置路径、原子应用、restart_required 摘要）
- [x] Runtime Status Tool（版本/go_version/uptime_seconds/ready）— internal/tool/builtin/introspection.go: RuntimeStatusTool{Name/Description/Parameters(empty object additionalProperties:false) Execute}; version=0.1.0 常量 + go_version=runtime.Version() + uptime/ready 走闭包 (Runtime.UptimeSeconds/Ready); nil 闭包 uptime=0 ready=false. Evidence: TestRuntimeStatusShaderAndExecute (version=0.1.0 uptime=12345 ready=true go_version non-empty) + TestRuntimeStatusNilFunc
- [x] Agent List Tool（状态过滤、只返 caller 自身）— internal/tool/builtin/introspection.go: AgentListTool{mgr *agent.Manager}; scope.AgentID 过滤 (docs §1 唯一 caller) + status enum 过滤; 无匹配返 {"items":[]}; nil mgr → IsError. Evidence: TestAgentListSelfOnly + TestAgentListStatusFilter + TestAgentListUnknownAgentReturnsEmpty
- [x] Agent Inspect Tool（详细信息 + 授权 Tool/Skill 名）— internal/tool/builtin/introspection.go: AgentInspectTool{mgr/toolMgr/skillMgr}; 调 agent.Manager.Inspect + tool.Manager.ListForAgent + skill.Manager.ResolveForAgent; Tool/Skill 按名升序; 不存在 → IsError. Evidence: TestAgentInspectSelf (tools 包含 config_query, skills=[alpha]) + TestAgentInspectUnknownAgentIsError
- [x] Session List Tool（Agent 过滤、state 过滤、limit）— internal/tool/builtin/introspection.go: SessionListTool{mgr}; session.Manager.List(ctx,scope.AgentID,ListQuery{State,Page:1,PageSize}); 固定字段 id/agent_id/state/message_count/created_at/updated_at 不含 metadata/消息. Evidence: TestSessionListEmpty + TestSessionListAfterCreate
- [x] Session Inspect Tool（单 session 元数据, AgentID 验证）— internal/tool/builtin/introspection.go: SessionInspectTool{mgr}; session.Manager.Get + 验证 Session.AgentID==scope.AgentID (不匹配与不存在同 IsError); v1 不含 messages/context/tool_results. Evidence: TestSessionInspectFound + TestSessionInspectNotFoundIsError + TestSessionInspectCrossAgentSameAsNotFound
- [x] Tool List Tool（source 过滤, 授权投影）— internal/tool/builtin/introspection.go: ToolListTool{mgr}; tool.Manager.ListForAgent(scope.AgentID) 天然只含 enabled+授权; source 过滤 builtin/plugin/mcp; 输出按 Name 升序. Evidence: TestToolListShowsRegistered (升序校验) + TestToolListFilterBySource
- [x] Skill List Tool（loaded 安全摘要）— internal/tool/builtin/introspection.go: SkillListTool{mgr}; skill.Manager.ResolveForAgent + Get 取 description/version/status; 安全字段 name/description/version/status 不含 prompt/path/options. Evidence: TestSkillListShowsBound (alpha loaded, description="Alpha skill") + TestSkillListUnboundAgentReturnsEmpty
- [x] Provider List Tool（canonical ID/type/model 只读列表）— internal/tool/builtin/introspection.go: ProviderListTool{mgr}; provider.Manager.List() (已按 ID 升序) 不发网络请求; 不含 api_key/base_url/health. Evidence: TestProviderListShowsOne (p1/openai, models=[test-model])
- [x] MCP List Tool（canonical `ServerStatus` 安全摘要） — internal/tool/builtin/mcp_list.go: MCPListTool{mgr *mcp.Manager}; mcp.Manager.List() 按 Name 升序 + server_name 单条过滤; ServerStatus 只含 name/status/transport/protocol_version/tool_count/connected_at/last_error 不含 command/args/url/headers/env/Token. Evidence: TestMCPListToolSchema + TestMCPListToolEmptyServersReturnsArray + TestMCPListToolListAllAndFilterName + TestMCPListToolFilterUnknownServerNameIsError

### 14.3 自定义 Tool

- [x] Plugin RPC Tool capability 与 Proxy 注册
- [x] 配置文件声明注册
- [x] Runtime 内置 Tool 的静态 Go 注册

### 14.4 Context 集成

- [x] Tool 结果 → `role="tool"` Message 转换
- [x] 原子单元（assistant+tool）截断保护
- [x] 深度思考模式下 reasoning_content 保留
- [x] Session、Remote、Planner 和 Tool Manager 只使用 canonical name；MCP Proxy 接收 canonical name、只把保存的 `remoteName` 发往上游，任何边界都不持久化 alias

### 14.5 可观测性

- [x] 执行日志（tool/agent/session/duration/result_tokens）
- [x] Prometheus 指标
- [x] Remote API 事件推送
- [x] alias 不作为日志/指标 label；协议错误不记录 Provider 返回的原始 name

---

*最后更新: 2026-07-23*
