# Planner 设计决策

> 上级: [Planner 系统设计](README.md)

---

## 已冻结决策

### PL-001: Planner 是单 turn 临时层

Plan 由 Agent 在 Session turn gate 内创建和消费。它不进入 Session、Storage 或 Memory，也不跨重启恢复。

### PL-002: v1 只有 LLM Planner

`type` 只支持 `llm|disabled`。固定流程由 Skill 表达，不为尚无实现的策略增加接口和配置。

### PL-003: Plan 与执行状态分离

`Plan` 只描述 DAG；状态与输出属于一次 `PlanResult`。执行器不原地修改模型生成的数据。

### PL-004: Step 直接组成 DAG

不增加 Task、Queue、Worker、Scheduler 或第二套状态机。Executor 使用调用期内存节点和有界 goroutine。

### PL-005: 先完整校验再执行

能力、ID、依赖、环和输入引用在任何外部调用前验证。模型输出永远不是权限凭据。

### PL-006: 失败即停止调度

首个失败取消已经运行的兄弟节点，并跳过尚未启动节点。完成的外部副作用不回滚、不自动重放。

### PL-007: 不叠加重试

Planner 与 Executor 都不重试。Provider、Tool 只按各自权威契约处理可重试错误。

### PL-008: 没有 Planner Remote surface

不增加路由、RBAC resource、专用事件或 snapshot。Remote 只投影现有 conversation 结果。

### PL-009: 最小输出绑定

只支持完整 Step 输出或一个直接 object key 的引用；复杂数据变换交给 Tool 或 LLM Step，v1 不引入表达式语言。

### PL-010: Planner capability 只有 Tool

Skill 在 turn 开始时解析为静态 Prompt，不建立第二个 Agent 子循环。Plan action 只有 `tool|llm`；Tool capability 只来自当前 Agent 的 `ListForAgent` 投影。
