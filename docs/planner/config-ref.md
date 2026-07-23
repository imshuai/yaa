# Planner 配置参考

> 上级: [Planner 系统设计](README.md)

---

## 1. 权威类型

```go
type PlannerConfig struct {
    Type          string        `yaml:"type"           json:"type"`
    Model         string        `yaml:"model"          json:"model"`
    Temperature   *float64      `yaml:"temperature"    json:"temperature"`
    MaxTokens     int           `yaml:"max_tokens"     json:"max_tokens"`
    MaxSteps      int           `yaml:"max_steps"      json:"max_steps"`
    MaxConcurrent int           `yaml:"max_concurrent" json:"max_concurrent"`
    Timeout       time.Duration `yaml:"timeout"        json:"timeout"`
}
```

`PlannerConfig` 同时用于根 `planner` 和可选的 `agents[].planner` 覆盖。没有 `SchedulerConfig`、重试配置、持久化配置或 context summary 配置。

---

## 2. 字段

| 字段 | 默认值 | 约束 | 说明 |
|------|--------|------|------|
| `type` | `llm` | `llm` / `disabled` | `disabled` 时 Agent 不创建 Planner/Executor |
| `model` | `""` | 已绑定 Provider 可用模型 | 空值使用 Agent model |
| `temperature` | `0.2` | `0.0..2.0` | pointer 保留显式 `0` |
| `max_tokens` | `2048` | `1..16384` | 规划响应上限 |
| `max_steps` | `16` | `1..64` | 单个 Plan 的 Step 上限 |
| `max_concurrent` | `4` | `1..16` | 单个 Plan 同时执行的 ready Step 上限 |
| `timeout` | `30s` | `1s..5m` | 只限制规划请求；执行使用 turn deadline |

```yaml
planner:
  type: llm
  model: ""
  temperature: 0.2
  max_tokens: 2048
  max_steps: 16
  max_concurrent: 4
  timeout: 30s
```

v1 只有 `llm` 实现。固定工作流应实现为 Skill，不增加 `rule` Planner。

---

## 3. Agent 覆盖

Agent block 只覆盖显式非零字段；`temperature` 用 pointer 区分省略与显式 0。`type: disabled` 是有效覆盖。

```yaml
agents:
  - id: research
    name: Research
    provider: primary
    model: model-a
    planner:
      max_steps: 24
      timeout: 60s

  - id: chat
    name: Chat
    provider: primary
    model: model-a
    planner:
      type: disabled
```

解析顺序为：根默认值 -> 根 `planner` -> `agents[].planner` 非零覆盖 -> 完整校验。合并完成后 Runtime 只向 Planner 传递一个完整值。

---

## 4. 更新边界

根 `planner.*` 和 `agents[].planner.*` 在 v1 都需要重启。配置 watcher 可以报告这些路径为 `restart_required`，但不能替换正在执行的 Planner 或 Executor。

环境变量不是字段级覆盖源；只有 YAML 标量中的 `${VAR}` 展开，规则见 [Config 环境变量](../config/envvar.md)。
