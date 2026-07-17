# Planner 集成设计

> 文档路径: `docs/planner/integration.md`
> 上级: `docs/planner/README.md`
> 依赖: `docs/architecture.md` §3.5, `docs/tool/`, `docs/skill/`

---

## 1. 概述

Planner 是 Yaa! Runtime 中连接"任务输入"与"执行编排"的中间层。它接收用户任务，将其分解为有序步骤（Plan），然后由 Agent 按步骤执行。

Planner 与以下核心模块紧密集成：

| 模块 | 集成方向 | 说明 |
|------|---------|------|
| Agent | 双向 | Agent 调用 Planner 生成计划；Planner 借助 Agent 的 Provider 能力 |
| Tool | 单向 | Plan 中的 Step 可引用 Tool 执行原子操作 |
| Skill | 单向 | Plan 中的 Step 可触发 Skill 完成多步骤工作流 |
| Session | 单向 | Planner 在 Session 上下文中运行，Plan 挂载到 Session |
| Context | 单向 | Planner 输出的 Plan 可注入 Context 供 LLM 参考 |

---

## 2. 与 Agent 集成

Agent 是 Planner 的主要调用方。当 Agent 收到用户任务后，判断是否需要规划：

```go
func (a *Agent) handleTask(ctx context.Context, task string) error {
    // 1. 判断是否需要规划（简单任务跳过 Planner）
    if a.shouldPlan(task) {
        plan, err := a.planner.Plan(ctx, task, a)
        if err != nil {
            return fmt.Errorf("planning failed: %w", err)
        }
        // 2. 将 Plan 挂载到当前 Session
        a.session.SetPlan(plan)
        // 3. 按步骤执行
        return a.executePlan(ctx, plan)
    }
    // 简单任务直接进入 Agent Loop
    return a.runLoop(ctx, task)
}

func (a *Agent) executePlan(ctx context.Context, plan *Plan) error {
    for _, step := range plan.Steps {
        if err := a.executeStep(ctx, step); err != nil {
            return err
        }
    }
    return nil
}
```

**Planner 使用 Agent 的 Provider：**

默认的 LLM 驱动规划器通过 Agent 绑定的 Provider 调用 LLM 生成计划：

```go
func (p *LLMPlanner) Plan(ctx context.Context, task string, a *agent.Agent) (*Plan, error) {
    prompt := p.buildPlanPrompt(task, a.Tools, a.Skills)
    resp, err := a.Provider.Chat(ctx, &provider.ChatRequest{
        Messages: []provider.Message{
            {Role: "system", Content: p.systemPrompt},
            {Role: "user", Content: prompt},
        },
    })
    if err != nil {
        return nil, err
    }
    return p.parsePlan(resp.Content)
}
```

---

## 3. 与 Tool 集成

Plan 中的 Step 可以指定调用某个 Tool：

```go
type Step struct {
    ID       string
    Action   string         // "tool:shell", "skill:web-scraper", "llm"
    Input    map[string]any
    Depends  []string
    Status   StepStatus
}
```

**Step 执行时调用 Tool：**

```go
func (a *Agent) executeStep(ctx context.Context, step *Step) error {
    switch {
    case strings.HasPrefix(step.Action, "tool:"):
        toolName := strings.TrimPrefix(step.Action, "tool:")
        result, err := a.toolMgr.Execute(ctx, toolName, step.Input)
        if err != nil {
            step.Status = StepFailed
            return fmt.Errorf("step %s failed: %w", step.ID, err)
        }
        step.Output = result
        step.Status = StepDone
    case strings.HasPrefix(step.Action, "skill:"):
        // 见下文 Skill 集成
    }
    return nil
}
```

---

## 4. 与 Skill 集成

Plan 中的 Step 可以触发一个完整 Skill 工作流，而非单个 Tool：

```go
func (a *Agent) executeStep(ctx context.Context, step *Step) error {
    if strings.HasPrefix(step.Action, "skill:") {
        skillName := strings.TrimPrefix(step.Action, "skill:")
        // 激活 Skill → 注入 Prompt → 进入 Agent Loop 执行
        if err := a.skillMgr.Activate(skillName, a); err != nil {
            step.Status = StepFailed
            return err
        }
        step.Status = StepDone
    }
    return nil
}
```

| Step Action | 执行方式 | 粒度 |
|-------------|---------|------|
| `tool:<name>` | 调用单个 Tool | 原子操作 |
| `skill:<name>` | 激活 Skill 工作流 | 多步骤编排 |
| `llm` | 纯 LLM 推理 | 无外部调用 |

---

## 5. 与 Session 集成

Plan 生成后挂载到 Session，支持跨轮次执行和恢复：

```go
func (s *Session) SetPlan(plan *Plan) {
    s.Plan = plan
    s.Metadata["plan_created_at"] = time.Now()
}

func (s *Session) GetPlan() *Plan {
    return s.Plan
}
```

**Session 恢复时自动加载未完成的 Plan：**

```text
Session 恢复 → 检查 Plan → 有未完成 Step → 从中断处继续执行
```

---

## 6. 与 Context 集成

Planner 可将 Plan 摘要注入 Context，让 LLM 在后续推理中感知整体计划：

```go
func (p *LLMPlanner) injectPlanContext(ctx *context.Context, plan *Plan) {
    summary := p.summarizePlan(plan)
    ctx.AppendMessage(provider.Message{
        Role:    "system",
        Content: fmt.Sprintf("## Current Plan\n\n%s", summary),
    })
}
```

**Context 占用控制：** Plan 摘要限制在 500 tokens 以内，超出时仅保留当前和后续步骤。
