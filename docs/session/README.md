# Session 系统设计

> 文档路径: `docs/session/`
> 依赖: [Provider](../provider.md)、[Storage](../storage/README.md)、[Context](../context/README.md)

---

## 1. 职责与边界

Session 是一次对话的持久状态单元，负责：

- 保存完整、有序的 Provider 消息历史；
- 管理 `created | active | paused | closed` 生命周期；
- 为同一 Session 的完整对话 turn 提供 FIFO 串行边界；
- 按创建时解析出的 policy 同步持久化和恢复；
- 发布提交后的状态、消息和删除事件。

Session 不负责组装 System Prompt、裁剪上下文、调用 Provider、执行 Tool 或生成 Memory。Context Manager 每次从 Session 快照组装完整 `provider.ChatRequest`，且不得修改 Session 历史。

## 2. 权威数据模型

```go
package session

type State string

const (
    StateCreated State = "created"
    StateActive  State = "active"
    StatePaused  State = "paused"
    StateClosed  State = "closed"
)

type Session struct {
    ID             string
    AgentID        string
    State          State
    CreatedAt      time.Time
    UpdatedAt      time.Time
    LastActivityAt time.Time
    Messages       []SessionMessage
    Metadata       map[string]any
    Policy         config.SessionPolicy
    SchemaVersion  int
}

type SessionMessage struct {
    ID        string
    TurnID    string
    Payload   provider.Message
    CreatedAt time.Time
    Metadata  map[string]any
}
```

约束：

- Session ID 为 `ses_<ULID>`；Message ID 为 `msg_<ULID>`。
- `TurnID` 由 `RunTurn` 控制并写入该 turn 的每条消息；调用方不能通过 metadata 伪造。
- `AgentID`、`CreatedAt`、`Policy` 和 `SchemaVersion` 创建后不可变。
- `Payload` 直接保存完整 `provider.Message`，不得复制到会丢失 `ReasoningContent`、`Refusal` 或 Tool 字段的本地 DTO。
- `Payload` 中的 assistant `ToolCalls[].Function.Name` 与非空 tool `Name` 始终是 canonical Tool name；Provider-safe alias 只存在于 Agent 发送 Provider 的临时请求副本，绝不持久化。
- `system` 消息不写入 Session；System Prompt、Skill Prompt 和 Memory 注入由 Context 负责。
- `Policy` 是根配置、Agent override 和 Create override 的解析结果，定义见 [配置参考](config-ref.md)。
- `SchemaVersion` 的 v1 值固定为 `1`。
- Manager 返回深拷贝或只读快照，调用方不得持有并修改内部 slice、map 或消息对象。

REST DTO 可以将 `SessionMessage.Payload` 的字段展开为扁平 JSON，但存储模型始终保留完整、canonical 的 `provider.Message`。Restore 只验证 canonical 名称的格式、消息序列和 Tool unit 完整性，不要求历史 Tool 当前仍注册、enabled 或属于当前 Agent allowlist。

## 3. Manager 契约

```go
type CreateRequest struct {
    AgentID  string
    Policy   *config.SessionOverride
    Metadata map[string]any
}

type AppendInput struct {
    Message  provider.Message
    Metadata map[string]any
}

type ListQuery struct {
    State    *State
    Page     int
    PageSize int
}

// Turn 只在 RunTurn callback 生命周期内有效，方法不再次进入 FIFO gate。
type Turn struct { /* unexported fields */ }

func (t *Turn) Snapshot() *Session
func (t *Turn) AppendUser(content string, metadata map[string]any) (SessionMessage, error)
func (t *Turn) Append(inputs []AppendInput) ([]SessionMessage, error)

func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Session, error)
func (m *Manager) Get(ctx context.Context, sessionID string) (*Session, error)
func (m *Manager) List(ctx context.Context, agentID string, q ListQuery) ([]*Session, int, error)
func (m *Manager) Pause(ctx context.Context, sessionID string) error
func (m *Manager) Resume(ctx context.Context, sessionID string) error
func (m *Manager) Close(ctx context.Context, sessionID string) error
func (m *Manager) Delete(ctx context.Context, sessionID string) error
func (m *Manager) DeleteMessage(ctx context.Context, sessionID, messageID string) ([]string, error)
func (m *Manager) ClearMessages(ctx context.Context, sessionID string) (int, error)
func (m *Manager) Restore(ctx context.Context, now time.Time) error
func (m *Manager) RunTurn(
    ctx context.Context,
    sessionID, turnID string,
    onQueued func(position int),
    fn func(context.Context, *Turn) error,
) error
func (m *Manager) CancelTurn(sessionID, turnID string, cause error) error
func (m *Manager) CancelAgentTurns(ctx context.Context, agentID string, cause error) error
func (m *Manager) Shutdown(ctx context.Context) error
```

所有有等待或 I/O 的 Manager 方法接受 `context.Context`。同一 Session 的写操作与完整 Agent turn 进入同一个 FIFO gate；等待期间只由 context 取消，不定义 `ErrSessionBusy` 或锁超时。不同 Session 可以并行。`RunTurn` 为 caller context 派生并登记可取消的 `turnCtx`，callback 和所有下游只能使用它。`Turn` 不能保存到 callback 外，也不能在 callback 内再次调用同一 Session 的 Manager 写方法。`Shutdown` 是 Manager 总生命周期入口，不能与单个 Session 的 `Close` 混用。

`turnID` 必须为 1..128 UTF-8 bytes 且不含控制字符。Manager 在接受排队时预留 `(sessionID, turnID)`；重复的 queued/running 或已提交 ID 返回 `ErrTurnIDConflict`。`AppendUser` 必须是 callback 的首个写操作，并将 ID 原子加入 snapshot 的永久判重集。queued/callback 在提交 user 前取消会释放预留，ID 可重用；user 一旦提交，即使后续失败、取消、Clear 或 DeleteMessage，该 ID 在 Session 物理 Delete 前都不可重用。`RunTurn` 在 turn context 被取消时返回 `context.Cause(turnCtx)`，从而保留 caller deadline、客户端取消、Agent Stop 和 Runtime shutdown 的不同原因。`CancelAgentTurns` 是管理型收拢操作，不受 Manager admission 状态限制：即使 Manager 已进入 `closing` 或 `closed` 仍可调用；它先取消该 Agent 的全部 handle，再等待它们从 registry 移除。调用时没有活动 handle 则幂等返回 `nil`；仍有 handle 而等待 context 结束时返回 `context.Cause(ctx)`。

## 4. 核心不变量

1. 创建时解析并校验 policy；热更新不改变已存在 Session。
2. `persist=true` 时，先同步写入候选快照，再发布内存状态和事件；写入失败则操作失败且状态不变。
3. `persist=false` 时完全不读写 Storage，进程重启后消失。
4. 首个合法持久消息将 `created` 与消息追加原子提交为 `active`。
5. Assistant Tool call 与其全部 Tool result 是不可拆分 unit；追加和删除必须整组处理。
6. 达到 `max_messages` 或 `max_message_bytes` 时拒绝整个追加批次；Session 不裁剪历史。
7. `Close` 对已关闭 Session 幂等，不重复持久化或发布事件；其余 Closed 写操作拒绝。
8. 事件仅在状态提交成功后发布一次。恢复已有快照不重放历史事件。
9. `persist=true` 的完整 JSON snapshot 不得超过 16 MiB；该限制独立于单条消息 policy。
10. 每个 turn 的消息使用同一个 `TurnID`；已提交 user 的 ID 持久判重，恢复后语义不变。

## 5. 文档索引

| 文件 | 内容 |
|------|------|
| [config-ref.md](config-ref.md) | 根配置、override、默认值和校验 |
| [lifecycle.md](lifecycle.md) | 状态机、TTL、最大生命周期和恢复 |
| [messaging.md](messaging.md) | 消息校验、Tool unit、查询和删除 |
| [persistence.md](persistence.md) | Snapshot 格式与同步持久化 |
| [concurrency.md](concurrency.md) | FIFO turn gate、锁顺序和关闭 |
| [integration.md](integration.md) | Agent、Context、Tool、Memory 集成 |
| [errors.md](errors.md) | 错误集合和 Remote API 映射 |
| [observability.md](observability.md) | 日志、指标和事件 |
| [decisions.md](decisions.md) | 已确定的设计决策 |
| [checklist.md](checklist.md) | 可执行实现清单 |

---

*最后更新: 2026-07-23*
