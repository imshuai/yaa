# Planner 可观测性

> 上级: [Planner 系统设计](README.md)

---

## 1. 日志

所有日志使用项目统一的 `slog` 兼容 API；Go 1.20 构建导入固定版本的 `golang.org/x/exp/slog`，不导入 Go 1.21 才提供的标准库 `log/slog`。固定字段如下：

| 事件 | Level | 字段 |
|------|-------|------|
| `planner.plan.started` | debug | `turn_id`, `agent_id`, `model` |
| `planner.plan.completed` | info | `turn_id`, `agent_id`, `plan_id`, `step_count`, `duration_ms` |
| `planner.plan.failed` | warn | 上述关联字段、`error_class`、`duration_ms` |
| `planner.step.started` | debug | `turn_id`, `plan_id`, `step_id`, `action`, `target` |
| `planner.step.completed` | debug | 关联字段、`duration_ms` |
| `planner.step.failed` | warn | 关联字段、`error_class`, `duration_ms` |

禁止记录 `PlanningInput.Task`、prompt、`Step.Input`、输出、API key、Token 或完整下游错误 body。`target` 已是当前 Agent 授权的注册名，仍应经过日志值长度限制。

---

## 2. 指标

若 Runtime 启用既有 metrics sink，Planner 只注册：

```text
yaa_planner_plans_total{agent_id,result}
yaa_planner_plan_duration_seconds{agent_id}
yaa_planner_steps_total{agent_id,action,result}
yaa_planner_step_duration_seconds{agent_id,action}
```

`result` 只允许 `completed|failed|canceled`；`action` 只允许 `tool|llm`。不得以 `plan_id`、`turn_id`、任务文本、Tool 名或错误字符串作为 label。

---

## 3. Remote 可见性

v1 不新增 Plan SSE/WS 事件、快照端点或全局广播。远端只收到现有 conversation 增量与终态；详细执行过程留在结构化日志和低基数指标中。
