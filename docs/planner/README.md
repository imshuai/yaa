# Planner 系统设计

> Planner 是当前 turn 内可选的规划步骤；本目录是 Planner v1 的权威契约。

---

## 1. 边界

Planner 接收当前用户任务和 Agent 已授权能力，返回一个临时 DAG。Agent 在持有该 Session 的 turn FIFO gate 时执行 DAG，随后只把普通消息和 Tool 结果写入 Session。

```text
Session turn gate
  -> 构造 PlanningInput
  -> Planner.Plan
  -> ValidatePlan
  -> Executor.Execute
  -> 结果进入本轮 Agent Context
  -> 生成最终 assistant message
```

v1 明确不提供以下能力：

- Plan 不属于 Session snapshot，不写入根 Storage，也不跨重启恢复。
- 不存在独立 Task、Queue、Worker 或 Scheduler 子系统。
- 不提供 Plan 的 REST、SSE 或 WebSocket 专用端点。
- 不支持暂停、恢复、人工修改或重放 Plan。
- Planner 不拥有 Provider 或 Tool 的重试；调用链各层只执行自己的既定策略。

取消请求、Session close、客户端断开或 turn 超时都会取消同一个 turn `context.Context`。已经产生的外部副作用不会回滚。

---

## 2. 权威模型

```go
type Planner interface {
    Plan(ctx context.Context, in PlanningInput) (Plan, provider.Usage, error)
}

type Plan struct {
    ID    string `json:"id"`
    Task  string `json:"task"`
    Steps []Step `json:"steps"`
}

type Step struct {
    ID      string         `json:"id"`
    Action  string         `json:"action"`
    Target  string         `json:"target,omitempty"`
    Input   map[string]any `json:"input,omitempty"`
    Depends []string       `json:"depends,omitempty"`
}
```

`Plan` 是不可变的规划结果，不包含运行状态、输出或时间戳。执行状态只存在于本轮 `PlanResult` 中。完整类型和生成约束见 [Planner 接口](planner.md)，DAG 与执行语义见 [执行流程](execution.md)。

---

## 3. 所有权

| 对象 | 创建者 | 生命周期 | 持久化 |
|------|--------|----------|:------:|
| `PlanningInput` | Agent | 单次 `Plan` 调用 | 否 |
| `Plan` | Planner | 当前 turn | 否 |
| DAG 节点状态 | Executor | 单次 `Execute` 调用 | 否 |
| `PlanResult` | Executor | 当前 turn | 否 |
| 普通消息 / Tool 结果 | Session / Agent | Session 生命周期 | 按 Session 配置 |

Executor 接收当前 turn 的真实 `agentID` 和 `sessionID`。Tool 权限仍由 Tool Manager 用该 principal 校验，不能信任模型生成的 `Target`；空 SessionID 不允许进入 Planner Executor。Skill 已在 turn 开始时作为静态 Prompt 解析，不形成 Planner action 或子循环。

---

## 4. 文件索引

| 文件 | 内容 |
|------|------|
| [planner.md](planner.md) | 权威接口、输入和 LLM 生成规则 |
| [execution.md](execution.md) | DAG 校验、输入绑定和执行语义 |
| [task.md](task.md) | 为什么 v1 没有独立 Task/Scheduler |
| [integration.md](integration.md) | Agent、Provider、Tool、Session 集成 |
| [config-ref.md](config-ref.md) | `PlannerConfig`、默认值和覆盖规则 |
| [errors.md](errors.md) | 错误分类和所有权 |
| [observability.md](observability.md) | 日志、指标和敏感信息约束 |
| [decisions.md](decisions.md) | 已冻结设计决策 |
| [checklist.md](checklist.md) | 最小实现与验收清单 |
