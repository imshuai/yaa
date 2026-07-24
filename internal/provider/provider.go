package provider

import (
	"context"
	"encoding/json"
	"time"
)

// Provider 是对 LLM 的唯一访问边界。Agent 只依赖该接口，不依赖厂商类型。
type Provider interface {
	ID() string
	Type() string
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
	EstimateInputTokens(ctx context.Context, req *ChatRequest) (int, error)
	Models() []ModelInfo
	Close() error
}

// ChatRequest 是一次对话请求的统一 DTO。
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	Tools          []ToolDef       `json:"tools,omitempty"`
	ToolChoice     *ToolChoice     `json:"tool_choice,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	Thinking       *ThinkingConfig `json:"thinking,omitempty"`
	Extra          map[string]any  `json:"extra,omitempty"`
}

// ThinkingConfig 是请求级 reasoning 配置。
type ThinkingConfig struct {
	Enabled bool   `json:"enabled"`
	Effort  string `json:"effort,omitempty"` // low | medium | high | max
	Budget  *int   `json:"budget,omitempty"`
}

// ResponseFormat 描述期望的输出格式。
type ResponseFormat struct {
	Type       string          `json:"type"` // text | json_object | json_schema
	Name       string          `json:"name,omitempty"`
	JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}

// Message 是统一消息类型，tags 与 Session canonical 形式一致。
type Message struct {
	Role             string     `json:"role"` // system | user | assistant | tool
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Name             string     `json:"name,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Refusal          string     `json:"refusal,omitempty"`
}

// ToolDef 描述一个可调用 Tool（类型固定 function）。
type ToolDef struct {
	Type     string       `json:"type"` // function
	Function ToolFunction `json:"function"`
}

// ToolFunction 是 Tool 的函数定义。
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
}

// ToolCall 是模型生成的一次 Tool 调用。
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // function
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 是 ToolCall 的函数载荷。
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON object encoded as string
}

// ToolChoice 描述 Tool 调用策略。
type ToolChoice struct {
	Mode string `json:"mode"`           // auto | none | required | specific
	Tool string `json:"tool,omitempty"` // Mode=specific 时必填
}

// ChatResponse 是非流式对话的完整响应。
type ChatResponse struct {
	ID               string     `json:"id"`
	Model            string     `json:"model"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Refusal          string     `json:"refusal,omitempty"`
	Role             string     `json:"role"` // assistant
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	FinishReason     string     `json:"finish_reason"`
	Usage            Usage      `json:"usage"`
}

// Usage 是 token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChunk 是流式响应的一个增量。
type ChatChunk struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
	Usage        *Usage `json:"usage,omitempty"`
	Error        error  `json:"-"`
}

// Delta 是单个 chunk 的增量内容。
type Delta struct {
	Role             string     `json:"role,omitempty"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Refusal          string     `json:"refusal,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// ModelInfo 描述一个已知模型的能力。
type ModelInfo struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	ContextWindow     int      `json:"context_window"`
	MaxOutput         int      `json:"max_output"`
	SupportsTools     bool     `json:"supports_tools"`
	SupportsVision    bool     `json:"supports_vision"`
	SupportsStreaming bool     `json:"supports_streaming"`
	SupportsThinking  bool     `json:"supports_thinking"`
	ThinkingEfforts   []string `json:"thinking_efforts"`
	MinThinkingBudget int      `json:"min_thinking_budget"`
}

// ProviderInfo 是对外只读视图。
type ProviderInfo struct {
	ID     string      `json:"id"`
	Type   string      `json:"type"`
	Models []ModelInfo `json:"models"`
}

// retryingTimeoutCap 限制单次逻辑调用退避上界，与文档 §6.1 的 30s 一致。
const retryBackoffCap = 30 * time.Second
