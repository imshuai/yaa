# Plan 校验与执行

> 上级: [Planner 系统设计](README.md)

---

## 1. DAG 校验

```go
func ValidatePlan(plan Plan, in PlanningInput) error
```

校验必须在启动任何 goroutine 或外部调用之前完成，并一次收集或返回首个确定性错误。规则如下：

1. 可信输入的 `TurnID`、`AgentID`、`Task`、`Model` 非空，`MaxSteps > 0`，Tool capability 名称唯一。
2. `Plan.ID == in.TurnID + ":plan"`、`Plan.Task == in.Task`，且 `1 <= len(Steps) <= in.MaxSteps`。
3. Step ID 非空且全局唯一。
4. `Action` 只能是 `tool`、`llm`。
5. `tool` 的 `Target` 非空且属于 `in.Capabilities`；`llm` 的 `Target` 必须为空。
6. `Depends` 不得重复、引用自身或引用不存在的 Step。
7. 用 Kahn 算法检查整个图无环；不得要求依赖必须出现在数组前方。
8. `Input` 中的 `$step` 引用只能指向该 Step 的直接依赖。

数组顺序只用于确定性调度：同一时刻多个节点 ready 时，按它们在 `Plan.Steps` 中的顺序启动。

---

## 2. 输入绑定

`Step.Input` 默认是字面 JSON 值。任意层级中，若一个 object 只含 `$step` 和可选 `key`，它是依赖输出引用：

```json
{
  "body": {"$step": "fetch"},
  "url": {"$step": "discover", "key": "url"}
}
```

- `{"$step":"fetch"}` 替换为 `fetch` 的完整输出。
- `{"$step":"discover","key":"url"}` 读取输出 object 的直接键 `url`。
- `key` 只支持一层 object key；v1 不实现表达式或 JSONPath。
- 被引用 Step 必须列在当前 Step 的 `Depends` 中。
- 缺少输出、输出不是 object 或键不存在时，该 Step 失败，且不会调用目标能力。
- 为防止歧义，含 `$step` 的 object 不得再包含 `$step`、`key` 以外字段。

解析器递归复制 `Input` 后替换引用，不得原地修改 `Plan`。

---

## 3. Executor API

```go
type StepStatus string

const (
    StepSucceeded StepStatus = "succeeded"
    StepFailed    StepStatus = "failed"
    StepCanceled  StepStatus = "canceled"
    StepSkipped   StepStatus = "skipped"
)

type StepResult struct {
    StepID   string        `json:"step_id"`
    Status   StepStatus    `json:"status"`
    Output   any           `json:"output,omitempty"`
    Error    string        `json:"error,omitempty"`
    Duration time.Duration `json:"duration"`
}

type StepRunResult struct {
    Output        any
    Usage         provider.Usage // tool step 为零值；不进入依赖绑定
    ToolCallCount int            // 仅实际调用 Tool 后为 1
}

type PlanResult struct {
    PlanID   string                `json:"plan_id"`
    Status   string                `json:"status"` // completed | failed | canceled
    Steps    map[string]StepResult `json:"steps"`
    Duration time.Duration         `json:"duration"`
    Usage    provider.Usage        `json:"-"`
    ToolCallCount int              `json:"-"`
}

type StepRunner func(
    ctx context.Context,
    agentID string,
    sessionID string,
    step Step,
    input map[string]any,
) (StepRunResult, error)

type Executor struct {
    maxConcurrent int
    run           StepRunner
}

func NewExecutor(maxConcurrent int, run StepRunner) (*Executor, error)
func (e *Executor) Execute(ctx context.Context, agentID, sessionID string, plan Plan) (PlanResult, error)
```

`NewExecutor` 拒绝 `maxConcurrent <= 0` 或 nil runner。`Execute` 假定调用方已完成 `ValidatePlan`，但仍校验空 `agentID` 和 `sessionID`；Planner 只在真实 Session turn 中运行，不能借空 Session 绕过 Tool 的 per-session gate。

### 3.1 Step 输入与输出

输入绑定完成后，两类 Step 使用以下唯一契约：

| Action | 必填输入 | Runner 行为 | 成功输出 |
|--------|----------|-------------|----------|
| `tool` | 目标 Tool Schema 要求的字段 | 以 `tool.ExecutionScope{AgentID: agentID, SessionID: sessionID}` 调用 Tool Manager | `{"content": string, "is_error": bool}` |
| `llm` | 非空字符串 `instruction` | 固定执行提示作为 system message；`instruction` 与其余输入的 JSON 作为 user message；`Tools=nil` | `{"content": string}` |

`instruction` 是保留键，引用替换后仍必须是字符串。Tool 的 `Meta`、Provider reasoning 和 usage 不进入依赖输出；LLM Step 的 `Usage` 只累计到 `PlanResult.Usage`。Tool Step 只有在 `ToolManager.Execute` 实际开始后才返回 `ToolCallCount=1`，包括软错误或硬错误；查参/绑定在调用前失败时为 0。`ToolResult.IsError=true` 是成功 Step 的软错误输出；只有 Tool 返回的硬 `error` 才使 Step 失败。这样 `$step` 的完整对象和 `key:"content"` 在所有 Action 上都有确定语义。

---

## 4. 调度算法

Executor 为本次调用建立 `remaining`、`dependents` 和结果 map，然后：

1. 建立 `planCtx, cancel := context.WithCancel(ctx)`。
2. 将 `remaining == 0` 的节点按 Plan 顺序放入本地 ready slice。
3. 在 `running < maxConcurrent` 时启动 ready 节点；worker 只执行输入绑定和 `StepRunner`，并向单一结果 channel 写一次结果。
4. 调度 goroutine 收到每个 worker 结果后，先累计 `StepRunResult.Usage` 与 `ToolCallCount`，再判断 error/status；因此 Provider 已返回但后续编码失败时仍保留 usage。成功时保存 Output，将每个 dependent 的 `remaining--`，降为 0 时加入 ready slice。
5. 第一个失败发生时记录该 Step，调用 `cancel()`，不再启动新节点。
6. 等待所有已启动 worker 返回。因失败而未启动的节点记为 `skipped`；收到取消的运行节点记为 `canceled`。
7. 全部成功返回 `status=completed, err=nil`；调用方取消返回 `status=canceled` 和 `context.Cause(ctx)`；Step 失败返回 `status=failed` 和 `*ExecutionError`。

结果 channel 容量至少为 `len(plan.Steps)`，确保取消后 worker 不会因无人接收而泄漏。所有结果 map 写入都发生在调度 goroutine 中。

---

## 5. 副作用与重试

- Executor 不自动重试 Step。
- Provider 的有限重试由 Provider Manager 负责；流式输出开始后不得重试。
- Tool 自身只按 Tool 契约重试；Planner 不叠加次数。
- 失败前已经完成的 Tool 外部副作用不会回滚，也不会在下一 turn 自动恢复。
