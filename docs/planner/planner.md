# Planner 接口与生成契约

> 上级: [Planner 系统设计](README.md)

---

## 1. 权威类型

```go
package planner

import (
    "context"
    "encoding/json"
)

type Planner interface {
    Plan(ctx context.Context, in PlanningInput) (Plan, provider.Usage, error)
}

type PlanningInput struct {
    TurnID       string       `json:"turn_id"`
    AgentID      string       `json:"agent_id"`
    Task         string       `json:"task"`
    Model        string       `json:"model"`
    MaxSteps     int          `json:"max_steps"`
    Capabilities []Capability `json:"capabilities"`
}

type Capability struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type Plan struct {
    ID    string `json:"id"`
    Task  string `json:"task"`
    Steps []Step `json:"steps"`
}

type Step struct {
    ID      string         `json:"id"`
    Action  string         `json:"action"` // tool | llm
    Target  string         `json:"target,omitempty"`
    Input   map[string]any `json:"input,omitempty"`
    Depends []string       `json:"depends,omitempty"`
}
```

要求：

- `TurnID`、`AgentID`、`Task` 和 `Model` 必填，`MaxSteps` 必须等于已解析的当前 Agent Planner 配置且大于 0。
- `Capabilities` 只能来自 `ToolManager.ListForAgent(AgentID)` 的 enabled 授权投影；名称必须唯一，`Parameters` 是其 JSON Schema。
- `Plan.ID` 固定为 `TurnID + ":plan"`，不依赖第三方 UUID 包。
- `Plan.Task` 固定复制可信的 `PlanningInput.Task`。
- `Plan` 和 `Step` 不包含状态；执行状态见 [execution.md](execution.md)。

模型只生成 `steps`。实现必须拒绝模型输出中的 `id`、`task`、状态、时间戳或其他未知顶层字段；可信的 ID/Task 只由输入构造。

---

## 2. Action 语义

| `Action` | `Target` | 行为 |
|----------|----------|------|
| `tool` | 必填，且必须是已授权 Tool 名 | 将绑定后的 `input` 作为参数调用 Tool Manager |
| `llm` | 必须为空 | `input.instruction` 是指令；使用当前 Agent Provider 做一次无 Tool 推理 |

`Target` 在执行前必须再次鉴权。Planner 输出不是权限凭据。

---

## 3. LLMPlanner

`LLMPlanner` 持有一个已注册的 `provider.Provider` 和解析后的 `config.PlannerConfig`。每个 Agent 在 Runtime 启动时绑定自己的实例；Planner 不从全局注册表动态选择 Provider。

```go
type LLMPlanner struct {
    provider provider.Provider
    cfg      config.PlannerConfig
}

func (p *LLMPlanner) Plan(ctx context.Context, in PlanningInput) (Plan, provider.Usage, error)
```

实现顺序固定为：

1. 校验 `PlanningInput` 必填字段。
2. 用 `context.WithTimeout(ctx, cfg.Timeout)` 派生规划上下文。
3. 将任务和授权能力编码为 JSON 后放入 user message；不要用字符串拼接构造能力 JSON。
4. 调用当前 Provider 的 `Chat`。`cfg.Model` 非空时覆盖 `in.Model`；收到响应后无论后续 JSON 校验是否成功，都把该响应的 `Usage` 原样返回给 Agent accumulator。
5. 请求 JSON object，并用 `json.Decoder.DisallowUnknownFields` 严格解码到仅含 `Steps []Step` 的临时 DTO；拒绝 trailing token。
6. 构造可信的 `Plan{ID: in.TurnID + ":plan", Task: in.Task, Steps: raw.Steps}`。
7. 返回候选 `Plan` 和 planning `Usage`。Agent 作为所有 Planner 实现共享的 trust boundary，随后调用一次 `ValidatePlan(plan, in)`；校验失败不得执行任何 Step。

所有步骤必须一次生成。v1 不支持模型在执行中追加或改写步骤。

---

## 4. Prompt 输出格式

Planner system prompt 必须要求只返回下列 JSON object：

```json
{
  "steps": [
    {
      "id": "s1",
      "action": "tool",
      "target": "http",
      "input": {"url": "https://example.invalid/data"},
      "depends": []
    },
    {
      "id": "s2",
      "action": "llm",
      "input": {
        "instruction": "Summarize the fetched object.",
        "source": {"$step": "s1"}
      },
      "depends": ["s1"]
    }
  ]
}
```

输入引用语法由 [执行流程](execution.md#2-输入绑定) 定义。Prompt 必须同时给出 `max_steps`，并明确 action 只有 `tool|llm`，禁止使用未列入 `Capabilities` 的 Tool。
