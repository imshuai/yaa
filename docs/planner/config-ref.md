# Planner 配置参考

> 文档路径: `docs/planner/config-ref.md`
> 上级: `docs/planner/README.md`

---

## 1. 全局配置

Planner 配置位于 `config.yaml` 的 `planner` 段：

```yaml
planner:
  # 规划器类型，可选: llm / rule / disabled
  type: llm

  # 规划时的 LLM 参数（可选，覆盖 Agent 的 Provider 默认值）
  model: ""
  temperature: 0.3
  max_tokens: 2000

  # 并发规划数（同时进行规划任务的最大数）
  max_concurrent: 4

  # 单次规划超时
  timeout: 30s

  # 是否在简单任务时跳过规划
  auto_skip: true

  # Plan 摘要注入 Context 的最大 token 数
  context_summary_tokens: 500
```

---

## 2. 配置项详解

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `llm` | 规划器类型 |
| `model` | string | `""` | 指定规划用的模型（空则用 Agent 绑定模型） |
| `temperature` | float | `0.3` | 规划时的采样温度，低温度 → 更确定性 |
| `max_tokens` | int | `2000` | 规划请求的最大输出 token 数 |
| `max_concurrent` | int | `4` | 并发规划上限 |
| `timeout` | duration | `30s` | 单次规划超时 |
| `auto_skip` | bool | `true` | 简单任务自动跳过规划 |
| `context_summary_tokens` | int | `500` | Plan 摘要注入 Context 的 token 上限 |

---

## 3. 规划器类型

| 类型 | 说明 | 适用场景 |
|------|------|---------|
| `llm` | LLM 驱动规划，通过 Prompt 让模型输出结构化计划 | 通用场景（默认） |
| `rule` | 基于预定义规则分解任务 | 固定流程、确定性要求高 |
| `disabled` | 不启用 Planner，所有任务直接进入 Agent Loop | 简单对话、降低延迟 |

---

## 4. Agent 级别覆盖

每个 Agent 可独立配置 Planner 参数，覆盖全局设置：

```yaml
agents:
  - id: research-agent
    name: "Research Agent"
    provider_id: openai
    planner:
      type: llm
      temperature: 0.1          # 更确定性
      max_tokens: 4000          # 更长计划
      timeout: 60s               # 更长超时

  - id: chat-agent
    name: "Chat Agent"
    provider_id: claude
    planner:
      type: disabled             # 纯对话，不需要规划
```

---

## 5. 配置合并优先级

```text
Agent 级 planner 配置 > 全局 planner 配置 > 默认值
```

合并逻辑：

```go
func resolvePlannerConfig(global PlannerConfig, agent PlannerConfig) PlannerConfig {
    result := global // 以全局为基准
    if agent.Type != "" {
        result.Type = agent.Type
    }
    if agent.MaxConcurrent > 0 {
        result.MaxConcurrent = agent.MaxConcurrent
    }
    if agent.Timeout > 0 {
        result.Timeout = agent.Timeout
    }
    // ... 其他字段同理
    return result
}
```

---

## 6. 环境变量覆盖

支持通过环境变量覆盖配置文件值（优先级最高）：

| 环境变量 | 对应字段 |
|---------|---------|
| `YAA_PLANNER_TYPE` | `planner.type` |
| `YAA_PLANNER_TIMEOUT` | `planner.timeout` |
| `YAA_PLANNER_MAX_CONCURRENT` | `planner.max_concurrent` |
| `YAA_PLANNER_MODEL` | `planner.model` |
