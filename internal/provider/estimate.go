// Package provider estimate: 完整请求 token 估算的共享启发.
// docs/context/checklist.md 行22/23: 估算包含 Tool schema、response format、framing 和 Provider extra.
package provider

import "encoding/json"

// estimateRequestChars 返回完整请求 wire 的近似字符总数.
// 含 messages、tools schema、tool_calls、response_format, 和 provider extra.
// ponytail: char/4 启发, 无 tokenizer. 实际 wire 不 marshal, 只累加关键字段长度.
func estimateRequestChars(req *ChatRequest) int {
	if req == nil {
		return 0
	}
	total := 0
	// 1. messages: content + reasoning + tool calls
	for _, m := range req.Messages {
		total += len(m.Content) + len(m.ReasoningContent)
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	// 2. tools schema: function name + description + parameters JSON Schema
	for _, t := range req.Tools {
		total += len(t.Function.Name) + len(t.Function.Description)
		total += len(t.Function.Parameters) // json.RawMessage length'
	}
	// 3. response_format: type + json_schema
	if req.ResponseFormat != nil {
		total += len(req.ResponseFormat.Type) + len(req.ResponseFormat.Name) + len(req.ResponseFormat.JSONSchema)
	}
	// 4. provider extra: marshal pre-existing fields 已在 wire 中为 framing
	for k, v := range req.Extra {
		total += len(k)
		if raw, err := json.Marshal(v); err == nil {
			total += len(raw)
		}
	}
	// 5. tool_choice framing (system/required/tool)
	if req.ToolChoice != nil {
		if raw, err := json.Marshal(req.ToolChoice); err == nil {
			total += len(raw)
		}
	}
	// 6. thinking config framing
	if req.Thinking != nil {
		total += len(req.Thinking.Effort)
		if req.Thinking.Budget != nil {
			total += 8 // int 表示
		}
	}
	return total
}

// estimateTokensFromChars 用 char/4 启发返回 token 估算.
func estimateTokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}
