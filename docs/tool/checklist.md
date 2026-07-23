# 实现检查清单

> 文档路径: docs/tool/checklist.md
> 上级: README.md 14

---

## 14. 实现检查清单

### 14.1 Tool Manager

- [ ] Tool 注册 / 注销 / 查找；disabled 项保留在 List、拒绝 Execute
- [ ] 权限只使用 `agents[].tools []string` allowlist，空值表示全部
- [ ] 参数 JSON Schema 校验
- [ ] gate 取消返回 `context.Cause(ctx)`，保留 Agent Stop/Runtime shutdown 等 caller cause
- [ ] Go 1.20 `WithCancelCause` + `time.AfterFunc` 以 `ErrToolTimeout` 覆盖 Tool/退避/重试；caller cause 检查优先于 child cause
- [ ] 并发执行 + 并发上限
- [ ] 结果截断
- [ ] 重试使用 `var retryable RetryableError` + `errors.As(err, &retryable)`，并在同一 `callCtx` 内指数退避
- [ ] 结构化日志
- [ ] `ToToolDefs` 冻结 current definitions + Session history 的不可变 Provider 投影
- [ ] canonical name 校验覆盖合法 UTF-8、1..256 bytes、无控制字符
- [ ] Provider-safe alias 算法、完整 SHA-256 base32、联合碰撞检查与 `ErrToolAliasCollision`
- [ ] definitions 只含 enabled/authorized Tool；history-only alias 不进入 executable 反查表
- [ ] 请求历史和 `specific` ToolChoice 深拷贝投影；Context estimator 看见最终 wire alias
- [ ] direct/stream 共用精确 alias 反查；unknown/非法 alias 不进入 ExecuteBatch
- [ ] Execute/ExecuteBatch 使用 `ExecutionScope`，Agent turn 传真实 SessionID
- [ ] Batch 使用有界 worker，结果保持输入顺序；MCP 空 Session 只走全局 gate

### 14.2 内置 Tool

- [ ] Shell Tool（命令白/黑名单、超时、输出截断）
- [ ] HTTP Tool（域名白/黑名单、重定向、响应截断）
- [ ] File Read Tool（路径校验、大小限制）
- [ ] File Write Tool（路径校验、创建目录）
- [ ] File List Tool（路径校验、递归选项）
- [ ] File Delete Tool（路径校验、安全确认）
- [ ] Config Query Tool（完整 `config.RedactedView` 后路径查询，脱敏不可关闭）
- [ ] Config Reload Tool（统一主配置路径、原子应用、restart_required 摘要）
- [ ] Runtime Status Tool（版本/uptime/内存/goroutine/统计）
- [ ] Agent List Tool（状态过滤、摘要信息）
- [ ] Agent Inspect Tool（详细信息、Session/Context/Tool/Skill 绑定）
- [ ] Session List Tool（Agent 过滤、状态过滤、Token 统计）
- [ ] Session Inspect Tool（消息历史、上下文统计、Tool 结果可选）
- [ ] Tool List Tool（source 过滤、enabled 过滤）
- [ ] Skill List Tool（`loaded|disabled` 安全摘要）
- [ ] Provider List Tool（canonical ID/type/model 列表）
- [ ] MCP List Tool（canonical `ServerStatus` 安全摘要）

### 14.3 自定义 Tool

- [ ] Plugin RPC Tool capability 与 Proxy 注册
- [ ] 配置文件声明注册
- [ ] Runtime 内置 Tool 的静态 Go 注册

### 14.4 Context 集成

- [ ] Tool 结果 → `role="tool"` Message 转换
- [ ] 原子单元（assistant+tool）截断保护
- [ ] 深度思考模式下 reasoning_content 保留
- [ ] Session、Remote、Planner 和 Tool Manager 只使用 canonical name；MCP Proxy 接收 canonical name、只把保存的 `remoteName` 发往上游，任何边界都不持久化 alias

### 14.5 可观测性

- [ ] 执行日志（tool/agent/session/duration/result_tokens）
- [ ] Prometheus 指标
- [ ] Remote API 事件推送
- [ ] alias 不作为日志/指标 label；协议错误不记录 Provider 返回的原始 name

---

*最后更新: 2026-07-23*
