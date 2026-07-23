# Planner 集成

> 上级: [Planner 系统设计](README.md)

---

## 1. Agent 调用顺序

Planner 只能在 Session 已取得 turn FIFO gate 后运行：

```go
func (a *Agent) runPlannedTurn(ctx context.Context, turn *session.Turn, req TurnRequest) (TurnResult, error) {
    if a.planner == nil { // resolved planner.type == disabled
        return a.runDirectLoop(ctx, turn, req)
    }

    var usage provider.Usage // 每次调用独占，不能放在 Agent 字段上。
    in := a.planningInput(req.TurnID, req.Content)
    plan, planningUsage, err := a.planner.Plan(ctx, in)
    addUsage(&usage, planningUsage)
    if err != nil {
        return TurnResult{Usage: usage}, fmt.Errorf("plan turn: %w", err)
    }
    if err := planner.ValidatePlan(plan, in); err != nil {
        return TurnResult{Usage: usage}, fmt.Errorf("validate plan: %w", err)
    }
    result, err := a.planExecutor.Execute(ctx, a.id, req.SessionID, plan)
    addUsage(&usage, result.Usage)
    if err != nil {
        return TurnResult{Usage: usage, ToolCallCount: result.ToolCallCount},
            fmt.Errorf("execute plan: %w", err)
    }
    return a.finishPlannedTurn(ctx, turn, plan, result, usage, result.ToolCallCount)
}

func addUsage(dst *provider.Usage, src provider.Usage) {
    dst.PromptTokens += src.PromptTokens
    dst.CompletionTokens += src.CompletionTokens
    dst.TotalTokens += src.TotalTokens
}
```

该 helper 只由 [Agent 唯一 turn 流程](../agent.md#4-唯一-turn-流程) 在 `turn.AppendUser` 成功后调用；`finishPlannedTurn` 使用传入的同一个 `*session.Turn` 提交 final assistant，并把最终生成 usage 加入传入的局部值后写入 `TurnResult`，不创建第二个 gate、Session handle 或 Agent 共享 accumulator。

规划失败不会静默切换到普通 Agent Loop。需要直接模式时应配置 `planner.type: disabled`；这可避免一次任务在两条执行路径上产生重复副作用。

---

## 2. 能力投影

`planningInput` 只能投影当前 Agent 已授权且可用的能力：

```text
ToolManager.ListForAgent(agentID)   -> Capability{...}
```

不得把全局 Tool 注册表、禁用项或其他 Agent 的能力放进 Prompt。Skill 在 Agent 构造 Context 时作为静态 Prompt 解析，不是 Planner capability。即使生成时做过过滤，执行时 Tool Manager 仍需按真实 scope 再次鉴权。

---

## 3. StepRunner 分发

Agent 构造唯一 `StepRunner`，按 `Action` 分发：

| Action | 分发规则 |
|--------|----------|
| `tool` | `ToolManager.Execute(ctx, tool.ExecutionScope{AgentID: agentID, SessionID: sessionID}, step.Target, input)`；实际调用后返回 `StepRunResult{Output: content/is_error, ToolCallCount: 1}`，Usage 为零值 |
| `llm` | 校验 `input.instruction`，用当前 Provider 的 `Chat`；请求不携带 Tool definitions；返回 `StepRunResult{Output: content, Usage: response.Usage}` |

Tool result 和 LLM response 都应转成 JSON 可编码值；无法编码时 Step 失败。`addUsage` 只操作本次 `HandleTurn` 栈上的局部值，逐字段相加 `PromptTokens/CompletionTokens/TotalTokens`；直接模式也用同一 helper 累计每次 Provider response。因此成功 `TurnResult.Usage` 覆盖本 turn 的 planning、LLM Step 与 final generation，不会在并行 Session 间串值。

---

## 4. Session 与 Context

- Session snapshot 不增加 `Plan` 或 `PlanResult` 字段。
- Agent 的 `RunTurn` callback 先提交 user message，再进入 Planner；Remote handler 不直接写 Session，后续失败不回滚该 user message。
- 直接 Planner Tool Step 没有 Provider 生成的前置 `assistant.tool_calls`，因此不得伪造或提交 Session Tool unit。Plan/Step 中间消息和输出全部保持 turn-local。
- 只有真实 Agent Tool Loop 才按 [Session Tool 集成](../session/integration.md#5-tool-集成) 原子提交完整 Tool unit；Planner 成功后再提交 final assistant。
- `PlanResult` 可用于构造最终回答，但不作为 system message 永久注入历史。
- Session close、stop、客户端取消和 Runtime shutdown 均通过同一个 turn context 传播。

Planner 不直接依赖根 `storage.Storage` 或 Memory Manager。

---

## 5. Remote API

Planner 没有独立路由或事件类型。Remote 客户端只观察现有 conversation contract：

- REST 请求返回本轮最终响应或统一错误 envelope。
- WebSocket 返回现有 `queued`、增量和恰好一个终态 frame。
- Session SSE 只发送该 Session 已定义的通用事件，不广播 Plan/Step 快照。

因此 Router、RBAC 和 Remote DTO 不需要新增 `plan` resource。
