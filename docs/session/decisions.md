# Session 设计决策

> 文档路径: `docs/session/decisions.md`
> 上级: `docs/session/README.md` §9

---

## 9. 设计决策

### SSD-001: Session 以 Agent 为归属，不可跨 Agent 迁移

**决策：** 每个 Session 在创建时绑定 `AgentID`，此后不可变更，不支持跨 Agent 迁移。

**理由：**
- Agent 决定了 Provider、Tool、Skill、Memory 等全部能力上下文
- 跨 Agent 迁移需重新校验权限和资源可用性，复杂度高
- 迁移需求可通过"新建 Session + 注入历史消息"替代

**影响：** SessionManager 的 `Create()` 必须校验 AgentID 有效性；`Get()` / `List()` 均以 AgentID 为查询维度。

---

### SSD-002: 四态状态机，Closed 为终态

**决策：** Session 采用 Created → Active → Paused → Closed 四态状态机，Closed 为不可逆终态。

**理由：**
- 四态覆盖了"刚创建""活跃交互""暂时挂起""彻底结束"四种真实场景
- Created 状态区分"已创建但尚未交互"的空会话，支持预创建 Session 的场景
- 不引入更多状态（如 `Error`），异常通过错误返回而非状态表达
- Closed 不可逆，避免"僵尸 Session"反复复活导致状态不一致

**影响：** 状态转换需在 Manager 层严格校验前置条件，非法转换返回 `ErrInvalidStateTransition`。

---

### SSD-003: 同一 Session 内消息串行处理

**决策：** 同一 Session 的消息处理严格串行，不并发执行；不同 Session 之间可并行。

**理由：**
- 并发写入同一 Session 会导致消息顺序混乱、状态竞争
- LLM 调用本身是请求-响应模型，串行处理符合语义
- 不同 Session 之间无共享状态，天然可并行

**影响：** Manager 需要为每个 Session 维护一个处理队列或互斥锁；并发模型在 [concurrency.md](concurrency.md) 中详细描述。

---

### SSD-004: 消息历史 append-only，不支持编辑

**决策：** Session 的消息历史一旦写入即不可修改（不支持 `EditMessage()`），但支持删除单条消息（通过 Remote API `DELETE /sessions/:id/messages/:msgid`）。

**理由：**
- append-only 保证消息历史的完整性和可审计性（不支持编辑）
- 编辑历史消息会破坏与 LLM 实际交互的对应关系
- 删除单条消息用于隐私/合规场景（如用户要求遗忘特定内容），是必要的运维操作
- 删除后上下文窗口同步更新，不影响后续对话

**影响：** 不提供 `EditMessage()` 接口；提供 `DeleteMessage()` 用于 Remote API 的 DELETE 端点；如需"重新开始"可新建 Session 或通过 Metadata 标记忽略旧消息。

---

### SSD-005: Session 持久化默认开启，使用 Storage 层

**决策：** Session 默认持久化到 Storage（SQLite），Runtime 重启后自动恢复；可通过配置禁用（纯内存模式）。

**理由：**
- 持久化是 Runtime 的基本职责，用户不应感知重启
- SQLite 零依赖，与项目技术选型一致
- 纯内存模式用于测试和轻量场景，提供灵活性

**影响：** SessionManager 需实现 `SessionStore` 接口；`RestoreAll()` 在 Runtime 启动时调用；存储格式在 [persistence.md](persistence.md) 中定义。

---

### SSD-006: Session ID 使用 ULID，前缀 `sess_`

**决策：** Session ID 格式为 `sess_` + ULID（26 字符），全局唯一且按时间排序。

**理由：**
- ULID 按时间排序，便于按创建顺序遍历和调试
- 前缀 `sess_` 便于在日志和存储中快速识别类型
- ULID 无需中心化分配，适合分布式场景

**影响：** ID 生成依赖 ULID 库；Message ID 同理使用 `msg_` + ULID。

---

### SSD-007: Metadata 使用 `map[string]any`，不做强类型约束

**决策：** Session 和 Message 的 Metadata 字段类型为 `map[string]any`，由业务层定义键值含义，核心层不做校验。

**理由：**
- 不同业务场景对元数据的需求差异大，强类型约束会限制扩展性
- 核心层只需透明存储和恢复，不需要理解 Metadata 语义
- JSON 序列化天然支持 `map[string]any`，无需额外编解码

**影响：** Metadata 的类型安全由业务层负责；核心层提供 `UpdateMetadata()` 增量更新接口（合并而非覆盖）。

---

### SSD-008: 流式对话绑定到 Session，通过 WebSocket / SSE 传输

**决策：** 流式对话通过 WebSocket（`/api/v1/sessions/:id/stream`）或 SSE（`/api/v1/sessions/:id/events`）实现，均绑定到特定 Session。

**理由：**
- 流式输出需要与 Session 的消息处理串行模型一致
- WebSocket 支持双向（用户可中途取消），SSE 适合只读推送
- 绑定 Session ID 确保流式输出写入正确的消息历史

**影响：** Remote API 层需将流式连接与会话生命周期关联；Session 关闭时主动断开对应流式连接。

---

## 10. 模块关系

```text
┌──────────────────────────────────────────────────────────────┐
│                        Runtime                                │
│                                                                │
│  ┌──────────────┐         ┌──────────────────────┐          │
│  │ Agent Manager │────────►│   Session Manager     │          │
│  │ (创建/调度)    │  拥有    │  (创建/查询/恢复/关闭)  │          │
│  └──────────────┘         └──────────┬───────────┘          │
│                                        │                       │
│                           ┌────────────┼────────────┐         │
│                           │            │            │         │
│                    ┌──────▼───┐  ┌─────▼────┐  ┌────▼─────┐  │
│                    │ Session A │  │ Session B │  │ Session C │  │
│                    │ (Active)  │  │ (Paused)  │  │ (Closed)  │  │
│                    │ Messages  │  │ Messages  │  │ Archived  │  │
│                    │ Metadata  │  │ Metadata  │  │           │  │
│                    └──────┬───┘  └──────────┘  └───────────┘  │
│                           │                                    │
│                           │ AppendMessage / GetMessages       │
│                           ▼                                    │
│                    ┌──────────────┐                            │
│                    │Context Mgr   │  构建 LLM 上下文窗口         │
│                    │(截断/压缩)    │  ← System Prompt + History  │
│                    └──────┬───────┘    + Memory + Tool 结果    │
│                           │                                    │
│                           ▼                                    │
│                    ┌──────────────┐                            │
│                    │  Provider    │  调用 LLM API              │
│                    │  (Chat/Stream)│  → 返回文本 / ToolCall     │
│                    └──────┬───────┘                            │
│                           │                                    │
│                           │ ToolCall → Tool Manager 执行        │
│                           │ 结果回写 Session 消息历史            │
│                           ▼                                    │
│                    ┌──────────────┐                            │
│                    │  Storage     │  持久化 Session + Messages  │
│                    │  (SQLite)    │  RestoreAll() 启动恢复      │
│                    └──────────────┘                            │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐     │
│  │  Remote API                                             │     │
│  │  POST /agents/:id/sessions        → Create              │     │
│  │  GET  /agents/:id/sessions        → List                │     │
│  │  GET  /sessions/:id               → Get detail          │     │
│  │  DEL  /sessions/:id               → Close              │     │
│  │  POST /sessions/:id/messages     → Send (非流式)       │     │
│  │  WS   /sessions/:id/stream       → 流式对话             │     │
│  │  GET  /sessions/:id/events        → SSE 事件流          │     │
│  └──────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────┘

依赖方向:
  Agent Manager → Session Manager (拥有、创建 Session)
  Session Manager → Storage (持久化、恢复)
  Session Manager → Context Manager (消息历史供上下文构建)
  Context Manager ← Session (读取 Messages 构建 LLM 上下文)
  Context Manager → Provider (发送 Chat 请求)
  Provider → Tool Manager (执行 ToolCall)
  Tool Manager → Session Manager (回写 Tool 结果消息)
  Remote API → Session Manager (所有 Session 操作的入口)
```

**关键依赖关系：**
- Session Manager 是 Session 子系统的核心，被 Agent Manager 创建、被 Remote API 调用
- Session 不直接依赖 Provider / Tool Manager，而是通过 Agent Loop 间接交互
- Context Manager 读取 Session 的消息历史，但不修改 Session 状态
- Storage 是唯一的持久化出口，Session Manager 负责序列化和恢复
- 流式连接（WS / SSE）绑定 Session 生命周期，Session 关闭时主动断开
