// Package tool 提供 Tool 系统统一接口、Manager（注册/发现/鉴权/执行）与内置 Tool。
// 实现对应 docs/tool 文档树。
package tool

import (
	"context"
	"encoding/json"
)

// ExecutionScope 是 Agent 在 Manager.Execute / ExecuteBatch 提供的调用身份。
// AgentID 永远非空；SessionID 在 Agent turn 中真实传入，MCP 等非 Session 调用可空。
type ExecutionScope struct {
	AgentID   string
	SessionID string
}

// Tool 是所有工具的统一接口。
type Tool interface {
	// Name 返回 canonical Tool name（1..256 UTF-8，无控制字符）。
	Name() string
	// Description 返回传递给 LLM 的人类可读描述。
	Description() string
	// Parameters 返回参数的 JSON Schema 字节（RawMessage）。
	Parameters() json.RawMessage
	// Execute 执行工具调用。params 已通过 JSON Schema 校验。
	// IsError 表示已处理的业务失败；error 表示 Manager 需分类的硬错误。
	Execute(ctx context.Context, scope ExecutionScope, params map[string]any) (ToolResult, error)
}

// ToolResult 是 Tool 执行返回值。
type ToolResult struct {
	Content string         // 返回给 LLM 的文本
	IsError bool           // 业务失败（非硬错误）
	Meta    map[string]any // 可选元数据，不传给 LLM
}

// ToolInfo 是只读公开视图。
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Enabled     bool            `json:"enabled"`
	Source      string          `json:"source"` // builtin | plugin | mcp
}
