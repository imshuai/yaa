# Session 实现检查清单

> 文档路径: `docs/session/checklist.md`
> 上级: `docs/session/README.md` §文档索引

---

### Session 定义与类型

- [ ] `Session` 结构体定义（ID, AgentID, Messages, State, CreatedAt, UpdatedAt, Metadata）
- [ ] `SessionState` 枚举（SessionStateCreated, SessionStateActive, SessionStatePaused, SessionStateClosed）
- [ ] `SessionState.String()` 方法实现
- [ ] `Message` 结构体定义（ID, Role, Content, ReasoningContent, ToolCalls, ToolCallID, Name, CreatedAt, Metadata）
- [ ] `MessageRole` 常量定义（RoleUser, RoleAssistant, RoleTool, RoleSystem）
- [ ] `ToolCall` 结构体定义（ID, Name, Arguments）
- [ ] Session ID 生成（`sess_` + ULID）
- [ ] Message ID 生成（`msg_` + ULID）
- [ ] `SessionOption` 函数式选项定义
- [ ] `MessageQueryOption` 查询选项定义

### 生命周期管理

- [ ] `Create()` — 创建新 Session，初始状态 Created
- [ ] `Create()` — AgentID 有效性校验
- [ ] `Create()` — 默认元数据初始化
- [ ] `Get()` — 获取 Session 详情
- [ ] `List()` — 列出指定 Agent 的所有 Session
- [ ] `ListByState()` — 按状态过滤 Session 列表
- [ ] `Pause()` — Active → Paused 状态转换
- [ ] `Resume()` — Paused → Active 状态转换
- [ ] `Close()` — Active/Paused → Closed 状态转换
- [ ] `Delete()` — 从存储中彻底移除 Session
- [ ] 状态转换合法性校验（非法转换返回错误）
- [ ] Closed 为终态，拒绝任何后续转换
- [ ] 状态变更时更新 `UpdatedAt`
- [ ] Paused 状态拒绝新消息追加

### 持久化与恢复

- [ ] `SessionStore` 接口定义（Save, Load, Delete, ListByAgent）
- [ ] 内存存储实现（MemoryStore）
- [ ] 文件存储实现（FileStore，JSON 序列化）
- [ ] 数据库存储实现（DBStore，可选）
- [ ] `RestoreAll()` — Runtime 启动时从存储恢复所有 Session
- [ ] 恢复时重建内存索引（sessions map + agentIdx map）
- [ ] 消息历史完整持久化
- [ ] 元数据持久化与反序列化
- [ ] 持久化原子性（写入失败不污染已有数据）
- [ ] 存储路径配置化
- [ ] Closed Session 的归档策略
- [ ] 持久化并发安全（不阻塞 Session 正常操作）

### 消息管理

- [ ] `AppendMessage()` — 向 Session 追加消息
- [ ] Append-only 语义保证（消息不可修改、不可删除）
- [ ] `GetMessages()` — 获取完整消息历史
- [ ] `GetMessages()` — 分页查询支持（offset / limit）
- [ ] `GetMessages()` — 按角色过滤（仅 user / 仅 assistant 等）
- [ ] `GetMessages()` — 时间范围过滤
- [ ] 消息追加时更新 `UpdatedAt`
- [ ] assistant 消息含 ToolCalls 的正确存储
- [ ] tool 消息与对应 ToolCall ID 的关联校验
- [ ] system 消息不持久化到 Session
- [ ] 消息元数据独立于 Session 元数据
- [ ] `UpdateMetadata()` — 更新 Session 级元数据

### 并发模型

- [ ] `sync.RWMutex` 保护 sessions map 和 agentIdx map
- [ ] 读操作使用 RLock（Get / List / GetMessages）
- [ ] 写操作使用 Lock（Create / Pause / Close / AppendMessage）
- [ ] 同一 Session 内消息串行处理
- [ ] 不同 Session 之间并行处理
- [ ] 长时间操作不持有全局锁（持久化异步执行）
- [ ] 消息队列 / Channel 实现（Session 级别串行化）
- [ ] 并发安全测试（race detector 通过）
- [ ] 锁粒度优化（Session 级锁 vs Manager 级锁）

### 集成

- [ ] 与 Agent 集成 — Agent 持有 SessionManager 引用
- [ ] 与 Agent 集成 — Agent 销毁时关闭所有关联 Session
- [ ] 与 Context Manager 集成 — 消息历史作为上下文窗口数据源
- [ ] 与 Context Manager 集成 — 上下文裁剪不修改原始消息历史
- [ ] 与 Memory 集成 — Session 消息可提取到长期 Memory
- [ ] 与 Provider 集成 — Message 类型转换为 Provider API 格式
- [ ] 与 Skill Manager 集成 — Skill Prompt 作为 system 消息注入
- [ ] 与 Tool Manager 集成 — Tool 结果作为 tool 消息追加
- [ ] 与 Remote API 集成 — 所有操作暴露为 HTTP 端点
- [ ] 与 Remote API 集成 — WebSocket / SSE 流式输出绑定 Session

### 配置

- [ ] 全局 Session 配置（`session.*` in config.yaml）
- [ ] `session.max_sessions` — 最大 Session 数限制
- [ ] `session.max_messages` — 单 Session 最大消息数限制
- [ ] `session.auto_close` — 闲置自动关闭策略
- [ ] `session.auto_close_interval` — 闲置超时时间
- [ ] `session.store_type` — 存储类型选择（memory / file / db）
- [ ] `session.store_path` — 文件存储路径
- [ ] Agent 级别 Session 配置覆盖
- [ ] 配置热加载支持

### 错误处理

- [ ] `ErrSessionNotFound` — Session 不存在
- [ ] `ErrSessionAlreadyExists` — Session ID 冲突
- [ ] `ErrSessionClosed` — 操作已关闭的 Session
- [ ] `ErrSessionPaused` — 向暂停的 Session 追加消息
- [ ] `ErrSessionNotActive` — Session 未处于 Active 状态
- [ ] `ErrInvalidAgentID` — 无效的 AgentID
- [ ] `ErrInvalidStateTransition` — 非法状态转换
- [ ] `ErrMaxSessionsReached` — 超过最大 Session 数
- [ ] `ErrMaxMessagesReached` — 超过最大消息数
- [ ] `ErrStoreUnavailable` — 存储后端不可用
- [ ] 错误信息包含 SessionID 便于排查
- [ ] 持久化失败不导致内存状态不一致

### 可观测性

- [ ] Session 创建日志
- [ ] Session 状态变更日志
- [ ] Session 关闭日志
- [ ] Session 恢复日志（RestoreAll）
- [ ] 消息追加日志（debug 级别）
- [ ] 指标: `session_total` (Gauge)
- [ ] 指标: `session_active` (Gauge)
- [ ] 指标: `session_paused` (Gauge)
- [ ] 指标: `session_closed` (Gauge)
- [ ] 指标: `session_messages_total` (Counter)
- [ ] 指标: `session_duration` (Histogram)
- [ ] SSE 事件: `session.created`
- [ ] SSE 事件: `session.paused`
- [ ] SSE 事件: `session.resumed`
- [ ] SSE 事件: `session.closed`
- [ ] SSE 事件: `session.message_appended`
- [ ] SSE 事件: `session.error`
- [ ] 调用链追踪（span: session.create, session.append, session.close）
