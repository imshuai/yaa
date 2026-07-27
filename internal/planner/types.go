// Package planner 是 Yaa! 单 turn 临时规划层 (docs/planner/README.md).
// v1 只支持 LLM Planner 与 disabled; Plan 在 Session turn FIFO gate 内消费, 不入 Session / Storage / Memory.
// 本文件定义 docs/planner/planner.md §1 与 execution.md §3 中的权威类型, 不含运行状态.
package planner

import (
	"encoding/json"
)

// Action 枚举: tool 调用 ToolManager, llm 做一次无 Tool 推理 (planner.md §2).
const (
	ActionTool string = "tool"
	ActionLLM  string = "llm"
)

// PlanningInput 是 Agent 调 Plan 时传入的上下文 (planner.md §1).
// 必填字段: TurnID / AgentID / Task / Model; MaxSteps 必须等于已解析的当前 Agent Planner 配置且 > 0.
// Capabilities 只能来自 ToolManager.ListForAgent(AgentID) 的 enabled 授权投影; 名称必须唯一, Parameters 是其 JSON Schema.
type PlanningInput struct {
	TurnID       string       `json:"turn_id"`
	AgentID      string       `json:"agent_id"`
	Task         string       `json:"task"`
	Model        string       `json:"model"`
	MaxSteps     int          `json:"max_steps"`
	Capabilities []Capability `json:"capabilities"`
}

// Capability 描述一个对当前 Agent 已授权的 Tool 能力.
type Capability struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Plan 是不可变的规划结果 (planner.md §1). 不包含状态 / 输出 / 时间戳.
// ID 固定为 TurnID + ":plan"; Task 固定复制可信的 PlanningInput.Task.
type Plan struct {
	ID    string `json:"id"`
	Task  string `json:"task"`
	Steps []Step `json:"steps"`
}

// Step 是 DAG 节点. action=tool 时 Target 必填且必须属于 Capabilities; action=llm 时 Target 必空.
// Depends 是直接依赖 ID 列表; 不得重复 / 自依赖 / 引用不存在 Step.
type Step struct {
	ID      string         `json:"id"`
	Action  string         `json:"action"` // tool | llm
	Target  string         `json:"target,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Depends []string       `json:"depends,omitempty"`
}
