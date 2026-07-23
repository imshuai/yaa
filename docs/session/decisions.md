# Session 设计决策

> 上级: [Session 系统设计](README.md)

---

## SSD-001：Session 归属 Agent 且不可迁移

创建时必须绑定存在的 `AgentID`。Session 不跨 Agent 迁移，因为 Provider、Model、Tool、Skill、Memory 和权限都由 Agent 决定；迁移需求通过新建 Session 并显式导入消息实现。

## SSD-002：四态状态机，Closed 终态

状态值使用稳定字符串 `created`、`active`、`paused`、`closed`。首个合法 user 批次触发 Created→Active；TTL 只暂停，`max_lifetime` 或 Close 才关闭。Closed 的 Close 幂等，其余写操作拒绝。

## SSD-003：ID 使用 `ses_` / `msg_` + ULID

Session ID 为 `ses_<ULID>`，Message ID 为 `msg_<ULID>`。ULID 的时间排序只用于可读性和默认排序，不作为权限或正确性的依据。

## SSD-004：保存完整 Provider 消息

SessionMessage 外层只增加 ID、受控 Turn ID、创建时间和 metadata，Payload 直接使用 `provider.Message`。不定义会丢失 `ReasoningContent`、`Refusal` 或 Tool 字段的第二套消息类型；system prompt 不进入历史。

## SSD-005：消息历史按批次原子提交

普通 user/final assistant 是单条批次；assistant Tool call 与全部 tool result 是不可拆分 unit。达到消息数或单消息字节限制时拒绝整个批次，Session 不自动裁剪或摘要。

## SSD-006：Resolved policy 在创建时冻结

根 `SessionConfig`、Agent `*SessionOverride` 和 Create `*SessionOverride` 使用 presence-aware 合并。解析出的 `SessionPolicy` 与 schema version 写入 snapshot；热更新只影响新建 Session。

## SSD-007：复用根 Storage，逐次同步 snapshot

唯一持久化接口是 `storage.Storage`，key 为 `session:<id>`。每次成功变更先 `Set` 候选 snapshot，再替换内存状态；失败不静默降级、不后台丢弃。`persist=false` 完全不读写 Storage。

## SSD-008：完整 turn 通过 Session FIFO gate

同一 Session 的 user 接收、Context Build、Provider、Tool loop 和所有消息提交串行；不同 Session 并行。等待可被 context 取消，不返回 busy/lock-timeout。

## SSD-009：Context 是临时视图

Context Manager 每次从 Session 快照、Agent system/skill prompt、Memory items 和 Tool definitions 组装完整 `provider.ChatRequest`。它可以压缩/截断请求副本，但不能修改或写回 Session，也没有 Session 的手动 `context/compress` endpoint。

## SSD-010：删除是显式且可审计的

历史不支持编辑。隐私删除通过 DeleteMessage（Tool unit 原子删除）或 ClearMessages；两者同步持久化并发布 `session.message.deleted`。物理 Delete 与 Close 分离。

## SSD-011：事件只描述已提交事实

唯一 Session mutation 事件为 `session.created`、`session.state.changed`、`session.message.appended`、`session.message.deleted`、`session.deleted`；失败另发 `session.error`。事件在提交后最多一次，恢复不重放历史事件。

## SSD-012：Session 不自动写 Memory

Session 保存完整短期历史；Memory 是 Agent 级长期数据。Memory promotion 是显式的 Agent/Memory 操作，不在 Close 或每条消息的隐式副作用中生成摘要。

## SSD-013：Turn ID 在 user 提交后永久判重

`RunTurn` 在排队时预留 client Turn ID，并保存可取消 handle。user 尚未提交的 queued turn 取消后释放 ID；`AppendUser` 成功时把 ID 写入消息和 snapshot tombstone 集。后续失败、消息删除或 Clear 都不释放，Restore 必须重建相同判重索引。

## SSD-014：Session 只保存 canonical Tool name

Agent 在 Session Append 前完成 Provider alias 的 all-or-nothing 反查。Snapshot 中的
assistant ToolCalls 与 tool message `Name` 永远是 canonical；Provider-safe alias、alias
map 和 Provider 选择不进入 Session。Restore 只做格式/序列/Tool unit 校验，即使历史 Tool
已 disabled、unregistered 或不再属于当前 Agent，也不因缺少注册表条目而失败。

---

*最后更新: 2026-07-23*
