# Planner 实现检查清单

> 上级: [Planner 系统设计](README.md)

---

## 配置与构造

- [ ] `PlannerConfig` 字段、默认值和范围与 [config-ref.md](config-ref.md) 一致
- [ ] `type=disabled` 时不创建 Planner/Executor
- [ ] Agent override 在启动时合并并完整校验
- [ ] Planner 配置变更报告 `restart_required`

## 生成

- [ ] `PlanningInput` 只含当前 Agent 已授权能力
- [ ] 规划调用继承 turn context，并额外应用规划 timeout
- [ ] 模型只生成 `steps`；Plan ID/Task 来自可信输入
- [ ] 使用结构化 JSON 编解码，不从 Markdown fence 截取 JSON
- [ ] Provider 错误、JSON 错误和校验错误保持可 `errors.Is/As`

## 校验

- [ ] Step 数、ID、Action、Target 和能力校验在执行前完成
- [ ] Depends 允许后向引用，但拒绝未知、自依赖和重复依赖
- [ ] Kahn 算法拒绝所有环
- [ ] `$step` 只引用直接依赖，且 object shape 严格
- [ ] `ValidatePlan(plan, in)` 精确绑定可信 TurnID/Task/MaxSteps/Tool capabilities
- [ ] 非法 Plan 不启动 goroutine、Tool 或 Provider Step

## 执行

- [ ] 同一 Session 的 Planner 位于既有 turn FIFO gate 内
- [ ] ready Step 按 Plan 数组顺序确定性调度
- [ ] 并发数不超过 `max_concurrent`
- [ ] 结果 map 只由调度 goroutine 写入
- [ ] 首次失败停止新调度、取消运行节点并等待全部退出
- [ ] 外部取消返回 `context` cause，无 goroutine/channel 泄漏
- [ ] Executor 不重试、不持久化、不恢复 Plan

## 集成与安全

- [ ] Tool 在执行时使用真实 `agentID`/`sessionID` 再次鉴权
- [ ] Skill 只作为静态 Agent Prompt，不存在 Planner Skill action 或子循环
- [ ] LLM Step 不携带 Tool definitions
- [ ] Step 输出在依赖绑定前验证可 JSON 编码
- [ ] Session snapshot、Remote 路由和 RBAC 均无 Plan 字段/resource
- [ ] 日志与指标不泄露任务、输入、输出、prompt 或 secret

## 最小测试

- [ ] 空 Plan、重复 ID、未知依赖、自依赖和环均失败且零调用
- [ ] 独立节点达到并发上限，依赖节点只在全部成功后启动
- [ ] 输入引用完整输出和直接 key 均正确解析
- [ ] 首个 Step 失败后未启动节点为 skipped，运行节点被取消
- [ ] turn cancel 与 timeout 后所有 worker 退出
- [ ] 同一 Agent 无权使用的 Tool 即使由模型生成也被拒绝
