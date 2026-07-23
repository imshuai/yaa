# Session 实现检查清单

> 每项都应能指向代码、测试或静态门禁证据。

---

## 类型与不变量

- [ ] `State` 使用 `created|active|paused|closed`，Closed 为终态。
- [ ] `Session` 包含 `ID`、`AgentID`、`State`、三个时间字段、`Messages`、`Metadata`、resolved `Policy` 和 `SchemaVersion`。
- [ ] ID 格式为 `ses_<ULID>` 与 `msg_<ULID>`。
- [ ] `SessionMessage` 含受控 `TurnID`，Payload 直接使用完整 canonical `provider.Message`，保留 `ReasoningContent`、`Refusal`、Tool calls/results。
- [ ] assistant ToolCalls 与非空 tool `Name` 只接收 Agent 反查后的 canonical 值；alias map 不进入 Session/metadata。
- [ ] system prompt 不写入 Session；Manager 返回深拷贝或只读快照。

## 生命周期

- [ ] Create 校验 Agent 存在、policy 和 `max_sessions_per_agent`，并原子写入 `created`。
- [ ] 首个合法 user 批次原子触发 `created -> active`。
- [ ] Pause、Resume、Close 使用统一状态转换守卫。
- [ ] Resume 达到 max lifetime 返回 `ErrSessionExpired` 且不变更；cleanup/Restore 才关闭。
- [ ] Close 对 Closed 幂等且不重复事件；其他 Closed 写操作返回 `ErrSessionClosed`。
- [ ] TTL 使用 `LastActivityAt`，max lifetime 使用 `CreatedAt`，max lifetime 优先。
- [ ] cleanup 只控制检查频率；恢复先重评估过期再建索引。

## 消息与 Tool unit

- [ ] `AppendInput` 只承载 Provider message 和 metadata；`Turn.AppendUser/Append` 自动写入当前 Turn ID。
- [ ] role/字段组合、JSON arguments、ToolCall ID 关联校验齐全。
- [ ] Tool name 校验 canonical 格式但不查当前注册表，Restore 可保留 history-only Tool。
- [ ] assistant Tool call 与全部 tool results 作为一个原子批次提交。
- [ ] 每条 Provider message 的序列化字节数不超过 `max_message_bytes`。
- [ ] 批次超过 `max_messages` 时整体拒绝，不裁剪历史。
- [ ] 查询支持 role、after ID、page/page_size，返回深拷贝。
- [ ] DeleteMessage 删除 Tool unit，且候选历史为空或首条为 user；ClearMessages 保留 Session state/policy/metadata。

## 持久化与恢复

- [ ] 只依赖根 `storage.Storage`，key 为 `session:<id>`。
- [ ] snapshot 使用唯一 `snapshotV1` DTO，含 schema、resolved policy、完整消息、`used_turn_ids` 和三个时间字段。
- [ ] JSON 编码后硬校验 16 MiB；超限同时包装 persistence/snapshot-too-large 错误。
- [ ] 每次变更同步 `Set`/`Delete`，失败不改变内存、不静默降级。
- [ ] Storage 调用前检查 context；无 ctx 的 Set/Delete 开始后完成对应内存提交。
- [ ] `persist=false` 完全不访问 Storage，重启后不恢复。
- [ ] Restore 严格校验所有记录；任一坏记录或状态修正写失败使 Runtime Not Ready。
- [ ] Restore 不因历史 Tool disabled/unregistered/未授权而失败，且不恢复其执行权限。
- [ ] Session snapshot 不使用 Storage TTL；生命周期 TTL 由 Manager 处理。

## 并发与关闭

- [ ] 每个 Session 有 FIFO runner/gate，完整 Agent turn 在同一 gate 内。
- [ ] `RunTurn` 保存派生 context 的 `CancelCauseFunc`；CancelTurn/CancelAgentTurns 可寻址 queued/running turn。
- [ ] `CancelAgentTurns` 在 Manager `closing`/`closed` 状态仍可调用；无活动 handle 时幂等返回 `nil`，有活动 handle 时按 caller cause 等待收拢。
- [ ] turn handle 含 `done`，RunTurn 的统一 defer 覆盖 enqueue/取消/Delete/panic/callback 的全部退出路径。
- [ ] user 提交前取消释放 Turn ID；提交后删除消息、Clear 和 Restore 均保持永久判重。
- [ ] 不同 Session 可并行；队列等待可被 context 取消。
- [ ] 不定义 `ErrSessionBusy` 或 `ErrLockTimeout`。
- [ ] 全局锁不跨 Storage、Provider、Tool 或 Event Bus 调用。
- [ ] Tool 可在 turn 内并行，但结果按 call 顺序组成一个 unit。
- [ ] Runtime 关闭先停止新任务、等待/取消 runner，再关闭 Storage。
- [ ] 唯一 `Manager.Shutdown(ctx)` 停止 cleanup，并等待所有 runner 退出。
- [ ] `go test -race ./...` 覆盖同 Session 并发、跨 Session 并行和关闭竞争。

## 集成与 API

- [ ] Agent 在 `RunTurn(sessionID, turnID, onQueued, callback)` 中先 `AppendUser`，再组装完整 ChatRequest 并调用 Context Build。
- [ ] Agent 先完成 turn-local alias 投影，Context 只读取已投影 request；不写回裁剪/摘要结果。
- [ ] Tool result 追加后重新 Build；最终 assistant 追加后 turn 才完成。
- [ ] Close 不隐式生成 Memory 摘要；Memory promotion 显式调用。
- [ ] Remote API 提供 Create、List、Get、Pause、Resume、Close、Delete、messages、clear。
- [ ] `/context/compress` 不存在；对话 SSE/WS token frame 与 Session mutation event 分离。
- [ ] DELETE message 对 Tool unit 原子生效，Clear 的语义和事件已实现。

## 错误与可观测性

- [ ] 稳定错误集合与 `errors.Is` 包装见 [errors.md](errors.md)。
- [ ] Persistence/restore 失败不降级；Runtime readiness 反映 Restore 结果。
- [ ] 事件名称只使用 canonical 六类，提交后最多一次。
- [ ] 指标全部使用 `yaa_session_*`，不含 session/message/tool ID 作为 label。
- [ ] 日志不记录内容、ReasoningContent、Tool arguments/result 或 secret。
- [ ] SSE 订阅者慢不会阻塞 Session 提交。

## 静态门禁

- [ ] `rg -n 'sess_|SessionStore|persist_interval|history_trim_policy|auto_close|idle_timeout|ErrSessionBusy|ErrLockTimeout|context/compress' docs/session docs/config docs/architecture.md docs/remote-api` 无旧契约残留（解释性迁移说明除外）。
- [ ] 所有 Session 文档相对链接存在。
- [ ] fenced JSON/YAML 可解析，`git diff --check` 通过。

---

*最后更新: 2026-07-23*
