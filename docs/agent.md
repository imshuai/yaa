# Agent 执行与生命周期契约

> Agent 是 Provider、Session、Context、Memory、Tool、Skill 和 Planner 的唯一编排 owner。其他模块不得复制 turn loop。

---

## 1. 边界

Agent 负责：

- 从 Config 构造每个 Agent 的不可变绑定，并在每个 turn 捕获 hot policy；
- 校验 Agent/Session 归属并在 Session FIFO gate 中执行完整 turn；
- 投影 Skill Prompt、Memory、Tool definitions，调用 Context 和 Provider；
- 聚合流式响应、执行 Tool、提交完整 Tool unit 和 final assistant；
- 管理 `running|paused|stopped` 状态及 turn 取消。

Session 仍是消息、`turn_id`、FIFO、持久化和 cancel handle 的 owner。Agent 不保存第二份历史；Provider 不聚合流；Remote 只做认证、DTO、事件映射和错误映射。

## 2. 类型与 Manager API

```go
package agent

import (
    "context"
    "errors"

    "golang.org/x/exp/slog"
)

type Status string

const (
    StatusRunning Status = "running"
    StatusPaused  Status = "paused"
    StatusStopped Status = "stopped"
)

var (
    ErrAgentNotFound         = errors.New("agent not found")
    ErrAgentInvalidRequest   = errors.New("invalid agent turn request")
    ErrAgentInvalidState     = errors.New("invalid agent state")
    ErrAgentPaused           = errors.New("agent paused")
    ErrAgentStopped          = errors.New("agent stopped")
    ErrAgentToolRoundLimit   = errors.New("agent tool round limit exceeded")
    ErrAgentProviderProtocol = errors.New("invalid provider protocol")
    ErrAgentManagerClosed    = errors.New("agent manager closed")
)

type Info struct {
    ID       string `json:"id"`
    Name     string `json:"name"`
    Provider string `json:"provider"`
    Model    string `json:"model"`
    Status   Status `json:"status"`
}

type Detail struct {
    Info
    Tools          []string `json:"tools"`
    Skills         []string `json:"skills"`
    MemoryEnabled  bool     `json:"memory_enabled"`
    PlannerEnabled bool     `json:"planner_enabled"`
}

type TurnRequest struct {
    SessionID string
    TurnID    string
    Content   string
    Metadata  map[string]any
    Stream    bool
    Emit      func(TurnEvent) // nil 表示不发布增量
}

type TurnEvent struct {
    Kind       string // queued | assistant_start | reasoning_delta | assistant_delta | tool_call | tool_result
    Position   *int   // 只在 queued 时非 nil
    Delta      string
    ToolCall   *provider.ToolCall
    ToolResult *ToolResultEvent
}

type ToolResultEvent struct {
    ToolCallID string
    Name       string
    Content    string
    IsError    bool
}

type TurnResult struct {
    Message       session.SessionMessage
    Usage         provider.Usage // 本 turn 全部 Provider 调用之和
    ToolCallCount int            // 本 turn 经 Tool Manager 实际执行的 call 总数
}

type Dependencies struct {
    Config    *config.ReloadManager
    Sessions  *session.Manager
    Context   *ctxwindow.Manager
    Memory    *memory.Manager
    Tools     *tool.Manager
    Skills    *skill.Manager
    Providers *provider.Manager
    Logger    *slog.Logger
}

func NewManager(deps Dependencies, agents []config.AgentConfig) (*Manager, error)
func (m *Manager) Get(id string) (Info, error)
func (m *Manager) Inspect(id string) (Detail, error)
func (m *Manager) List(status *Status) []Info
func (m *Manager) Start(ctx context.Context, id string) error
func (m *Manager) Pause(ctx context.Context, id string) error
func (m *Manager) Stop(ctx context.Context, id string) error
func (m *Manager) HandleTurn(ctx context.Context, agentID string, req TurnRequest) (TurnResult, error)
func (m *Manager) CancelTurn(ctx context.Context, agentID, sessionID, turnID string) error
func (m *Manager) Quiesce()
func (m *Manager) Shutdown(ctx context.Context) error
```

`NewManager` 拒绝任一 nil dependency；这些对象都由 Runtime 持有和关闭，Agent Manager 只借用。每个 Agent 只冻结 restart-required 的 Provider、Tool/Skill allowlist、Planner/Executor 和配置 ID；Context/Memory 的 hot effective policy 在 turn 开始时从同一个 snapshot 解析，Session policy仍在 Session 创建时解析并持久化。Runtime 不再持有一个全局 Planner。`NewManager` all-or-nothing，按 Agent ID 排序构造；任一静态引用或初始 effective policy 无效时不返回半成品。

`Get`/`List` 返回固定 `Info` 副本；`Inspect` 只追加启动时冻结的授权名称和两个 enabled 布尔值。三者都不返回 System Prompt、开放 config Map、options、Secret 或内部指针，slice 必须深拷贝。`List` 按 ID 升序，`Inspect.Tools`/`Skills` 按名称升序。未知 ID 返回 `ErrAgentNotFound`。

## 3. 生命周期

初始状态为 `running`。转换规则：

- `Start`：`stopped|paused -> running`，已 running 幂等；
- `Pause`：`running -> paused`，已 paused 幂等，stopped 返回 `ErrAgentInvalidState`；已进入 gate 的 turn 运行到提交边界，新 turn 拒绝；
- `Stop`：先原子变为 stopped 并拒绝新 turn，再调用 `session.Manager.CancelAgentTurns(ctx, id, ErrAgentStopped)` 取消并等待；ctx 到期原样返回且 Agent 保持 stopped，已 stopped 且没有 active turn 时幂等；
- `Quiesce`：Manager 原子进入 closing，按 Agent ID 升序置为 stopped 并拒绝新 turn，但不取消已经登记的 turn；只用于 Runtime 在 Session 优雅 drain 前关闭 admission，且幂等；
- `Shutdown`：先执行 `Quiesce`，再以 `ErrAgentManagerClosed` 为 cause 取消并等待仍残留的 turn；正常 Runtime 关闭会先完成 `session.Manager.Shutdown(ctx)`，因此这里通常没有残留，直接 rollback 时则由本方法负责收拢；不重复关闭共享 Provider、Tool 或 Storage。

每个 Agent 持有单调递增的 lifecycle generation；`Start`、`Pause`、`Stop` 每次实际转换都在同一锁内递增，`Quiesce` 对尚未 stopped 的 Agent 做同样处理。Manager closing 后，`Start`/`Pause`/`Stop`/`HandleTurn` 返回 `ErrAgentManagerClosed`。`HandleTurn` 在首次状态/Session 归属检查时捕获 generation，在调用 `RunTurn` 前再检查一次，并在 callback 开始时要求 `status=running` 且 generation 精确相等。这样已通过首次检查、但在 Session 登记前经历 Stop/Pause/Start/Quiesce 的旧请求只能失败，不能在新生命周期提交 user 或启动 Provider；已经进入 callback 的 turn 不再检查 Agent 状态，由 Session drain 或显式 cancel 决定终止。`CancelTurn` 先确认 Session 属于该 Agent，再委托 Session Manager；不存在或已经终态的 turn 返回 `session.ErrTurnNotActive`。

## 4. 唯一 turn 流程

`HandleTurn` 验证非空 `SessionID`、合法 `TurnID` 和非空 `Content`，深拷贝 metadata，然后调用：

```go
var result TurnResult
var onQueued func(int)
if req.Emit != nil {
    onQueued = func(position int) {
        req.Emit(TurnEvent{Kind: "queued", Position: &position})
    }
}
err := sessions.RunTurn(ctx, req.SessionID, req.TurnID, onQueued,
    func(turnCtx context.Context, turn *session.Turn) error {
        // 所有下游调用只使用 turnCtx。
        // 首个动作必须是 turn.AppendUser(req.Content, req.Metadata)。
        var err error
        result, err = a.runTurn(turnCtx, turn, req)
        return err
    })
return result, err
```

`TurnID` 使用 Session 的统一校验（1..128 UTF-8 bytes，不能含控制字符）。`onQueued` 只把 position 映射为 Remote `queued` frame；没有订阅者的内部调用可令 `req.Emit=nil`。`req.Emit` 必须是非阻塞 enqueue，订阅者慢或断开不能回滚 Session，也不能阻塞 Agent。

直接模式的每一轮固定为：

1. `turn.AppendUser` 原子提交当前 user；失败不调用任何下游。
2. 在 turn 开始时读取一个 Config snapshot；从它解析本轮 Context/Memory policy，调用 `SkillManager.ResolveForAgent`，并投影 Agent base prompt、Skill prompts、Memory 和 Session snapshot。本 turn 的所有 Memory 调用显式传同一个 policy。
3. 从同一个 Session snapshot 取得 canonical history，调用 `ToolManager.ToToolDefs(agentID, history)`，冻结 current definitions + history 的 turn-local [Provider Tool 投影](tool/provider.md)。`tool.ErrToolAliasCollision` 直接终止，不调用 Context/Provider。
4. 用 canonical history 组装 `Tools` 为空的完整 `provider.ChatRequest`；调用 `projection.ProjectRequest` 深拷贝并注入 alias definitions、投影历史和 `specific` ToolChoice，再把已投影请求交给 Context Manager Build。Context estimator 必须看到最终 wire alias。
5. `Stream=false` 调 `Provider.Chat`；`Stream=true` 调 `Provider.StreamChat` 并按 §5 聚合。两者先产出含完整 wire alias 的响应，再共用同一个 all-or-nothing 反查步骤生成 canonical `ChatResponse`。
6. 无 Tool call：先 `turn.Append` final assistant，成功后返回 `TurnResult`；Remote 此后才能发送 `assistant_done`。
7. 有 canonical Tool call：调用 `ToolManager.ExecuteBatch(turnCtx, tool.ExecutionScope{AgentID: a.id, SessionID: req.SessionID}, calls)`。Agent 按输入顺序把每项转换为带正确 `ToolCallID` 和 canonical `Name` 的 `role=tool` message，并将 assistant ToolCalls 与全部 results 一批提交。
8. 重新从 `turn.Snapshot()` 构造下一轮 canonical 请求，并复用本 turn 冻结的 projection。v1 固定最多 8 个 Tool round；超过后返回 `ErrAgentToolRoundLimit`，不提交不完整 unit。

已提交 user 或完整 Tool unit 在后续失败、取消或 disconnect 时保留。Provider delta、半个 Tool call、Planner 中间值和未提交 batch 永不写 Session。`HandleTurn` 在栈上创建本 turn 独占的 result/usage/Tool call accumulator，`RunTurn` 同步等待 callback 后才读取它们；Agent/Manager 结构体不得保存共享的 `turnUsage`。`Usage` 对每次 Provider 响应逐字段求和；即使后续解析/提交失败也供内部计量。`ToolCallCount` 统计实际进入 Tool Manager 的直接 Batch calls 与 Planner Tool steps，不统计调用前的绑定/参数失败。

Planner enabled 时，在 user 提交后按 [Planner 集成](planner/integration.md) 运行一次临时 DAG。Planner capability 只包含当前 Agent 的 canonical Tool definitions，不使用 Provider alias；PlanResult 只存在于当前 turn，并作为请求副本输入一次无 Planner 递归的最终生成。规划、校验或执行失败不回退到直接模式，避免重复副作用。

`ToolChoice{Mode:"specific"}.Tool` 在 Agent 请求侧是 canonical name，必须命中 projection 的 executable definitions；`ProjectRequest` 才把它换为与 `Tools[].Function.Name` 相同的 alias。alias map 不放进 Manager/Agent 字段、Session 或 metadata，turn 返回后即丢弃。

## 5. Stream accumulator

Provider 不聚合 chunk。Agent 对每次 `StreamChat` 建立一个局部 accumulator，并持续读取 channel 直到关闭：

1. channel 建立成功后发布一次 `assistant_start`；同步错误不发布 start。
2. `Delta.Role` 只能为空或 `assistant`。Content、ReasoningContent、Refusal 分别按到达顺序拼接，并发布对应 content/reasoning delta；Refusal 不单独暴露增量。
3. Tool call fragment 的 ID 每段必填；使用 index 的厂商由 Provider adapter 维护 `index -> call ID` 并补齐稳定 ID。Agent 按首次出现顺序建立 entry；同一 ID 的 Type 必须始终为 `function`，wire `Function.Name` alias 与 Arguments 分别拼接。不同 ID 不得混写；冲突返回 `ErrAgentProviderProtocol`。
4. 只接受一个逻辑 finish reason 和一个最终 Usage；重复相同 finish 可忽略，冲突值返回 `ErrAgentProviderProtocol`。channel 正常关闭但没有 finish reason 也是协议错误。
5. `finish_reason=tool_calls` 时必须至少有一个完整 call，且每个 Arguments 严格解码为单个 JSON object、拒绝 trailing token；存在 ToolCalls 时 finish reason 也必须是 `tool_calls`。完整 alias 拼接后才检查 provider-safe regex，并通过 projection 的 executable map 精确反查一次；全部成功后才按顺序发布含 canonical name 的 `tool_call`。
6. 收到 `ChatChunk.Error`、`turnCtx` 取消或协议错误时继续遵守 channel/cancel 契约并返回错误；accumulator 丢弃，任何 partial 都不提交。Remote 最终发送一个 `error` frame。
7. 非 Tool finish 组装 final assistant。只有 Session Append 成功后，调用方才发送包含持久化 Message ID 的 `assistant_done`。

`ExecuteBatch` 全部 worker 收拢后，Agent 按 call 输入顺序发布对应的 `tool_result` event；它只是进度，不是逐项实时通知或提交证明。Event Hub 丢弃连接不会取消由 REST 或其他连接发起的 turn；发起 WS 连接断开时，Remote 显式调用 `CancelTurn`。

Direct response 使用与 stream accumulator 相同的 call 校验和反查函数。非法、unknown 或 history-only alias 返回 `ErrAgentProviderProtocol`；任何 call 失败时整批不执行、不发布 `tool_call`、不提交 partial。不得把 wire alias 交给 `ExecuteBatch` 后依靠 `ErrToolNotFound` 兜底。

## 6. 最小验证

- direct 与 stream 对相同逻辑响应提交相同 final/Tool unit；chunk 任意切分不改变聚合结果；
- safe/hashed name、history-only name、`specific` ToolChoice、alias 碰撞和 unknown alias 覆盖 [Tool alias 验证矩阵](tool/provider.md#7-最小验证矩阵)；
- stream error、取消、无 finish、冲突 Tool fragment 和非法 Arguments 都不提交 partial；
- 所有 Tool 调用携带真实 AgentID/SessionID 和 canonical name，结果顺序与 calls 一致；
- 同一 Session 串行、不同 Session 并行；Pause/Stop/Cancel/Shutdown 可由 race test 验证；
- `Quiesce` 只关闭 admission，不取消已登记 turn；Session shutdown deadline 到期后才取消并收拢；
- System Prompt、Skill options、Tool options 和 Secret 不进入 Agent Info、日志或 Remote DTO；
- `go test -race ./...` 覆盖生命周期与 cancel registry。

---

*最后更新: 2026-07-23*
