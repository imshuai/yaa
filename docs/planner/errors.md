# Planner 错误处理与重试策略

> 文档路径: `docs/planner/errors.md`
> 上级: `docs/planner/README.md`

---

## 1. 错误分类

| 错误类型 | 说明 | 处理方式 |
|---------|------|---------|
| `ErrPlanTimeout` | 规划超时 | 取消规划，返回超时信息给 Agent |
| `ErrPlanParseFailed` | LLM 输出无法解析为 Plan | 重试或回退到简单模式 |
| `ErrPlanEmpty` | LLM 返回空计划 | 重试一次，仍空则直接进入 Agent Loop |
| `ErrPlanTooManySteps` | 步骤数超过上限 | 截断或拒绝，返回警告 |
| `ErrPlanCircularDep` | Step 之间存在循环依赖 | 检测并报错，拒绝执行 |
| `ErrStepExecutionFailed` | Step 执行失败 | 根据 retry 策略重试或终止 Plan |
| `ErrStepTimeout` | Step 执行超时 | 取消 Step，标记为 Failed |
| `ErrPlannerUnavailable` | 规划器不可用（Provider 故障） | 回退到 `disabled` 模式 |

---

## 2. 错误传递

```text
Planner 错误 → Agent → LLM / 用户
                    │
                    ├─ 可恢复 → 重试或调整策略
                    └─ 不可恢复 → 降级为无规划模式
```

**Agent 层错误处理：**

```go
func (a *Agent) planWithErrorHandling(ctx context.Context, task string) (*Plan, error) {
    plan, err := a.planner.Plan(ctx, task, a)
    if err != nil {
        switch {
        case errors.Is(err, ErrPlanTimeout):
            a.logger.Warn("plan timeout, falling back to direct mode", "task", task)
            return nil, nil // 返回 nil 表示跳过规划
        case errors.Is(err, ErrPlanParseFailed):
            a.logger.Warn("plan parse failed, retrying", "error", err)
            // 重试一次
            plan, err = a.planner.Plan(ctx, task, a)
            if err != nil {
                return nil, nil // 仍失败则跳过
            }
            return plan, nil
        case errors.Is(err, ErrPlannerUnavailable):
            a.logger.Error("planner unavailable", "error", err)
            return nil, nil // 降级
        default:
            return nil, err
        }
    }
    return plan, nil
}
```

---

## 3. 重试策略

| 操作 | 重试条件 | 重试次数 | 退避策略 |
|------|---------|---------|---------|
| 规划请求 | Provider 超时/错误 | 2 次 | 指数退避（2s, 4s） |
| Plan 解析 | LLM 输出格式错误 | 1 次 | 无延迟 |
| Step 执行 | Tool/Skill 调用失败 | 3 次 | 指数退避（1s, 2s, 4s） |
| Step 执行 | 超时 | 不重试 | — |

**Step 重试实现：**

```go
func (a *Agent) executeStepWithRetry(ctx context.Context, step *Step, maxRetry int) error {
    var lastErr error
    for attempt := 0; attempt <= maxRetry; attempt++ {
        if attempt > 0 {
            backoff := time.Duration(1<<attempt) * time.Second
            select {
            case <-time.After(backoff):
            case <-ctx.Done():
                return ctx.Err()
            }
            a.logger.Info("retrying step", "step", step.ID, "attempt", attempt)
        }
        err := a.executeStep(ctx, step)
        if err == nil {
            return nil
        }
        lastErr = err
        if errors.Is(err, ErrStepTimeout) {
            break // 超时不重试
        }
    }
    step.Status = StepFailed
    return fmt.Errorf("step %s failed after %d attempts: %w", step.ID, maxRetry+1, lastErr)
}
```

---

## 4. 降级策略

当 Planner 不可用时，Agent 自动降级为无规划模式：

```text
Planner 不可用 → 跳过规划 → 任务直接进入 Agent Loop → LLM 自主决策
```

| 降级场景 | 触发条件 | 行为 |
|---------|---------|------|
| Provider 故障 | 规划请求连续失败 | 跳过规划，直接执行 |
| 超时 | 规划超过 timeout | 返回空 Plan，直接执行 |
| 解析失败 | LLM 输出非结构化 | 重试一次后跳过 |
| 配置禁用 | `type: disabled` | 不调用 Planner |

---

## 5. Plan 执行中断与恢复

当 Plan 执行过程中发生不可恢复错误时：

```go
func (a *Agent) executePlan(ctx context.Context, plan *Plan) error {
    for _, step := range plan.Steps {
        // 跳过已完成的步骤（恢复场景）
        if step.Status == StepDone {
            continue
        }
        // 检查依赖是否满足
        if !a.checkDependencies(step, plan) {
            step.Status = StepFailed
            return fmt.Errorf("step %s has unmet dependencies", step.ID)
        }
        if err := a.executeStepWithRetry(ctx, step, 3); err != nil {
            // 记录失败点，支持后续恢复
            a.session.SetPlan(plan)
            return err
        }
    }
    return nil
}
```

| 场景 | 处理 |
|------|------|
| Step 失败 | 标记 Failed，终止 Plan，保存到 Session |
| Runtime 重启 | 从 Session 恢复 Plan，跳过已完成 Step |
| 手动重试 | 用户通过 Remote API 重新触发失败的 Step |
