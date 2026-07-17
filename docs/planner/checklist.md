# Planner 实现检查清单

> 文档路径: `docs/planner/checklist.md`
> 上级: `docs/planner/README.md`

---

## 核心接口与类型

- [ ] `Planner` 接口定义（`Plan(ctx, task, agent) (*Plan, error)`）
- [ ] `Plan` 结构体定义（Steps, CreatedAt, Task）
- [ ] `Step` 结构体定义（ID, Action, Input, Depends, Status, Output）
- [ ] `StepStatus` 枚举（Pending, Running, Done, Failed, Skipped）
- [ ] `PlannerConfig` 结构体定义（Type, Model, Temperature, MaxTokens, MaxConcurrent, Timeout, AutoSkip）
- [ ] 错误变量定义（ErrPlanTimeout, ErrPlanParseFailed, ErrPlanEmpty 等）

## 规划器实现

- [ ] `LLMPlanner` 结构体（默认 LLM 驱动规划器）
- [ ] 规划 Prompt 模板构建（包含任务描述、可用 Tool/Skill 列表）
- [ ] LLM 调用（通过 Agent 的 Provider）
- [ ] LLM 输出解析为 Plan 结构
- [ ] 解析失败处理（重试一次后回退）
- [ ] `RulePlanner` 结构体（规则驱动规划器，可选实现）
- [ ] `DisabledPlanner` 结构体（空实现，直接返回 nil）
- [ ] 规划器工厂函数（根据配置类型创建实例）

## Step 依赖与 DAG

- [ ] Step 依赖解析（`Depends []string`）
- [ ] 循环依赖检测（DAG 校验）
- [ ] 拓扑排序生成执行顺序
- [ ] 依赖未满足时的错误处理
- [ ] 可并行 Step 的识别（无相互依赖的 Step）

## Plan 执行

- [ ] `executePlan()` — 按拓扑顺序执行 Step
- [ ] `executeStep()` — 单个 Step 执行（分发到 Tool/Skill/LLM）
- [ ] `executeStepWithRetry()` — 带重试的 Step 执行
- [ ] Step 超时控制（context.WithTimeout）
- [ ] 已完成 Step 跳过（恢复场景）
- [ ] Plan 执行中断处理（保存失败点到 Session）

## Agent 集成

- [ ] `shouldPlan()` — 简单任务检测逻辑
- [ ] `planWithErrorHandling()` — 规划错误处理与降级
- [ ] Agent 启动时初始化 Planner 实例
- [ ] Plan 摘要注入 Context（限制 token 数）
- [ ] auto_skip 配置生效

## Session 集成

- [ ] `Session.SetPlan()` — 挂载 Plan 到 Session
- [ ] `Session.GetPlan()` — 获取当前 Plan
- [ ] Session 恢复时加载未完成 Plan
- [ ] Plan 持久化到 Storage
- [ ] 从 Storage 恢复 Plan 状态

## 配置

- [ ] 全局 `planner` 配置段解析
- [ ] Agent 级别 `planner` 配置覆盖
- [ ] 配置合并逻辑（Agent > 全局 > 默认值）
- [ ] 环境变量覆盖（`YAA_PLANNER_*`）
- [ ] 配置项验证（timeout > 0, max_concurrent > 0 等）

## 错误处理与重试

- [ ] `ErrPlanTimeout` — 规划超时处理
- [ ] `ErrPlanParseFailed` — 解析失败重试
- [ ] `ErrPlanEmpty` — 空计划处理
- [ ] `ErrPlanTooManySteps` — 步骤数上限检查
- [ ] `ErrPlanCircularDep` — 循环依赖检测
- [ ] `ErrStepExecutionFailed` — Step 失败重试（3 次指数退避）
- [ ] `ErrStepTimeout` — Step 超时不重试
- [ ] `ErrPlannerUnavailable` — Provider 故障降级
- [ ] 降级逻辑：Planner 不可用时跳过规划

## 可观测性

- [ ] 规划开始日志（task, agent_id）
- [ ] 规划完成日志（steps_count, duration）
- [ ] 规划失败日志（error, type）
- [ ] Step 执行日志（step_id, action, status, duration）
- [ ] Step 重试日志（step_id, attempt）
- [ ] 指标: `planner_plan_total` (Counter)
- [ ] 指标: `planner_plan_failed_total` (Counter)
- [ ] 指标: `planner_plan_duration` (Histogram)
- [ ] 指标: `planner_step_total` (Counter, 标签: action, status)
- [ ] 指标: `planner_step_retry_total` (Counter)
- [ ] SSE 事件: `planner.plan.started`
- [ ] SSE 事件: `planner.plan.completed`
- [ ] SSE 事件: `planner.plan.failed`
- [ ] SSE 事件: `planner.step.completed`
- [ ] SSE 事件: `planner.step.failed`

## Remote API

- [ ] `GET /api/v1/sessions/:id/plan` — 获取当前 Plan
- [ ] `POST /api/v1/sessions/:id/plan/retry` — 重试失败的 Step
- [ ] `POST /api/v1/sessions/:id/plan/cancel` — 取消 Plan 执行
- [ ] SSE: Plan 状态变化事件推送
