# Planner 实现检查清单

> 上级: [Planner 系统设计](README.md)

---

## 配置与构造

- [x] `PlannerConfig` 字段、默认值和范围与 [config-ref.md](config-ref.md) 一致 (config 包早已实现 + validate test 覆盖)
- [x] `type=disabled` 时不创建 Planner/Executor (agentBinding 仅 Type=="llm" 才构造 LLMPlanner; Type="" 兜底也按 disabled)
- [x] Agent override 在启动时合并并完整校验 (config.ResolvePlannerConfig 在 NewManager 末尾统一解析; Runtime 走 config.Validate 兜底完整性校验)
- [x] Planner 配置变更报告 `restart_required` (config-ref §4 已规约; yaa v1 没有 runtime hot reload watcher, restart_required 由 startup validation 路径表达)

## 生成

- [x] `PlanningInput` 只含当前 Agent 已授权能力 (LLMPlanner 不投影能力, 接受 Runtime 传入)
- [x] 规划调用继承 turn context，并额外应用规划 timeout (llm_planner.go: context.WithTimeout)
- [x] 模型只生成 `steps`；Plan ID/Task 来自可信输入 (DisallowUnknownFields + Plan.ID/Task 固定)
- [x] 使用结构化 JSON 编解码，不从 Markdown fence 截取 JSON (json.Decoder + Marshal user payload)
- [x] Provider 错误、JSON 错误和校验错误保持可 `errors.Is/As` (双 `%w` wrap; TestPlan* 验证)

## 校验

- [x] Step 数、ID、Action、Target 和能力校验在执行前完成 (internal/planner/validate.go 规则 3/4/5; capability 名唯一 + Action 工具目标 + LLM 无目标)
- [x] Depends 允许后向引用，但拒绝未知、自依赖和重复依赖 (规则 6; TestValidatePlanRejectsRule6DependsOrphans / TestValidatePlanDependsNeedNotPrecedeArrayOrder)
- [x] Kahn 算法拒绝所有环 (规则 7; TestValidatePlanRejectsRule7Cycle 覆盖 2 步 + 3 步环)
- [x] `$step` 只引用直接依赖，且 object shape 严格 (规则 8; TestValidatePlanRejectsRule8DollarStepReference / TestValidatePlanAcceptsRule8DollarStepKeyReference)
- [x] `ValidatePlan(plan, in)` 精确绑定可信 TurnID/Task/MaxSteps/Tool capabilities (规则 1/2; TestValidatePlanRejectsRule1InputEmpty / Rule2PlanIDAndTaskAndStepCount)
- [x] 非法 Plan 不启动 goroutine、Tool 或 Provider Step (ValidatePlan 是纯函数零副作用; 失败立刻返 *ValidationError 含 ErrPlanInvalid)

## 执行

- [x] 同一 Session 的 Planner 位于既有 turn FIFO gate 内 (runPlannedTurn 在 m.deps.Sessions.RunTurn callback 内运行, Session FIFO gate 已继承)
- [x] ready Step 按 Plan 数组顺序确定性调度 (executor.go §Execute ready slice 按 Plan.Steps 数组顺序弹; TestExecuteParallelIndependentHitsMaxConcurrent)
- [x] 并发数不超过 `max_concurrent` (Executor.maxConcurrent 控制启动节奏; TestExecuteParallelIndependentHitsMaxConcurrent peak ≤ maxConcurrent)
- [x] 结果 map 只由调度 goroutine 写入 (worker 用单独 stepResultMsg chan; results map 由 main schedule goroutine 写; docs §4 "所有结果 map 写入都发生在调度 goroutine 中")
- [x] 首次失败停止新调度、取消运行节点并等待全部退出 (firstErr + cancel(res.runErr) + running 等所有结果; TestExecuteFailsFirstStepSkipsRest + TestExecuteStepFailedCancelRunningSiblings)
- [x] 外部取消返回 `context` cause，无 goroutine/channel 泄漏 (planCtx.WithCancelCause; cancel 后 worker <-ctx.Done() 退出; resultCh cap=steps; TestExecuteCallerCancelCancelsRunning 2s 内返 + status=canceled)
- [x] Executor 不重试、不持久化、不恢复 Plan (executor.go 无 retry 持久化; PL-007 PL-001 一致)

## 集成与安全

- [x] Tool 在执行时使用真实 `agentID`/`sessionID` 再次鉴权 (AggregateStepRunner.runToolStep 用 tool.ExecutionScope{AgentID,SessionID}; ToolManager.Execute 内 CheckPermission)
- [x] Skill 只作为静态 Agent Prompt，不存在 Planner Skill action 或子循环 (planningInput.capabilities 只来自 ToolManager.ListForAgent, Skill 由 agent.Manager renderSkillSystemMessage 作 system message 注入, 不进 Capabilities)
- [x] LLM Step 不携带 Tool definitions (runLLMStep ChatRequest Tools=nil 显式; TestPlannedTurnEndToEnd 验证)
- [x] Step 输出在依赖绑定前验证可 JSON 编码 (executor.go scheduler 成功 path 之前 json.Marshal(Output) 不可编码 → step failed + ExecutionError carry marshalling err; TestExecuteFailsOnUnmarshalableOutput + TestExecuteMarshalCheckIsolated)
- [x] Session snapshot、Remote 路由和 RBAC 均无 Plan 字段/resource (planner 不入 Session 不写 Tool unit; api 沿用现有 routing 无 plan resource)
- [ ] 日志与指标不泄露任务、输入、输出、prompt 或 secret

## 最小测试

- [x] 空 Plan、重复 ID、未知依赖、自依赖和环均失败且零调用 (validate_test.go 覆盖规则 6/7 + steps_empty)
- [x] 独立节点达到并发上限，依赖节点只在全部成功后启动 (TestExecuteParallelIndependentHitsMaxConcurrent + TestExecuteFullyLinearCompleted)
- [x] 输入引用完整输出和直接 key 均正确解析 (TestValidatePlanAcceptsRule8DollarStepKeyReference + TestExecuteInputBindingChainedOutputs)
- [x] 首个 Step 失败后未启动节点为 skipped，运行节点被取消 (TestExecuteFailsFirstStepSkipsRest + TestExecuteStepFailedCancelRunningSiblings)
- [x] turn cancel 与 timeout 后所有 worker 退出 (TestExecuteCallerCancelCancelsRunning ctx cancel 2s 内 worker 退出, status=canceled)
- [x] 同一 Agent 无权使用的 Tool 即使由模型生成也被拒绝 (TestValidatePlanRejectsRule4And5ActionTarget/tool_unknown_target)
