// Package context 实现一次 Provider 请求的输入窗口管理。
// 它从 Agent 已组装的 provider.ChatRequest 中压缩或移除旧历史，
// 保证最终请求不超过目标模型的输入预算。
package context

import (
	stdctx "context"
	"fmt"
	"time"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/provider"
)

// Manager 是 Context 窗口管理器；无内部状态，每次 Build 独立。
type Manager struct{}

// NewManager 构造 Manager。
func NewManager() *Manager { return &Manager{} }

// BuildInput 是 Build 的输入。
type BuildInput struct {
	Provider         provider.Provider
	Model            provider.ModelInfo
	Request          provider.ChatRequest
	Config           config.ContextConfig
	CurrentTurnStart int
}

// BuildOutput 是 Build 的输出。
type BuildOutput struct {
	Request         provider.ChatRequest
	InputTokens     int
	InputBudget     int
	EffectiveWindow int
	Metadata        BuildMetadata
}

// BuildMetadata 记录本次构建的元信息。
type BuildMetadata struct {
	Strategy          string        `json:"strategy"`
	OriginalMessages  int           `json:"original_messages"`
	FinalMessages     int           `json:"final_messages"`
	CompressedTurns   int           `json:"compressed_turns"`
	TruncatedUnits    int           `json:"truncated_units"`
	CompressionFailed bool          `json:"compression_failed"`
	BuildDuration     time.Duration `json:"build_duration"`
}

// messageUnit 是内部处理单元。
type messageUnit struct {
	Messages     []provider.Message
	Protected    bool
	Compressible bool
}

// Build 把完整候选请求变成可安全发送的请求。
func (m *Manager) Build(ctx stdctx.Context, in BuildInput) (*BuildOutput, error) {
	start := time.Now()
	if in.Provider == nil {
		return nil, fmt.Errorf("%w: provider is nil", ErrContextBuildFailed)
	}
	if in.Request.Model != in.Model.ID {
		return nil, fmt.Errorf("%w: request model %q != model id %q", ErrContextBuildFailed, in.Request.Model, in.Model.ID)
	}
	if in.Request.MaxTokens == nil || *in.Request.MaxTokens <= 0 {
		return nil, fmt.Errorf("%w: request.max_tokens must be > 0", ErrContextBuildFailed)
	}

	budget, err := ResolveContextBudget(in.Config, in.Model.ContextWindow, in.Model.MaxOutput, *in.Request.MaxTokens)
	if err != nil {
		return nil, err
	}

	stategy := in.Config.Strategy
	if stategy == "" {
		stategy = "hybrid"
	}
	if stategy != "hybrid" && stategy != "truncate" && stategy != "reject" {
		return nil, fmt.Errorf("%w: unknown strategy %q", ErrContextConfigInvalid, stategy)
	}

	// 构造并校验消息单元
	units, err := groupUnits(in.Request.Messages, in.CurrentTurnStart)
	if err != nil {
		return nil, err
	}

	originalCount := len(in.Request.Messages)

	// 估算完整候选请求
	tokens, err := in.Provider.EstimateInputTokens(ctx, &in.Request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenEstimationFailed, err)
	}

	// 不超预算则直接返回
	if tokens <= budget.Input {
		return &BuildOutput{
			Request:         copyRequest(in.Request),
			InputTokens:     tokens,
			InputBudget:     budget.Input,
			EffectiveWindow: budget.EffectiveWindow,
			Metadata: BuildMetadata{
				Strategy:         stategy,
				OriginalMessages: originalCount,
				FinalMessages:    originalCount,
				BuildDuration:    time.Since(start),
			},
		}, nil
	}

	// 受保护请求估算（只保留 protected units 的消息）
	protectedMessages := collectProtectedMessages(units)
	if len(protectedMessages) > 0 {
		protectedReq := copyRequest(in.Request)
		protectedReq.Messages = protectedMessages
		ptokens, perr := in.Provider.EstimateInputTokens(ctx, &protectedReq)
		if perr != nil {
			return nil, fmt.Errorf("%w: protected estimate: %v", ErrTokenEstimationFailed, perr)
		}
		if ptokens > budget.Input {
			return nil, fmt.Errorf("%w: protected input %d > budget %d", ErrContextOverflow, ptokens, budget.Input)
		}
	}

	switch stategy {
	case "reject":
		return nil, fmt.Errorf("%w: reject strategy, tokens %d > budget %d", ErrContextOverflow, tokens, budget.Input)
	case "truncate":
		return m.truncate(ctx, in, units, budget, stategy, originalCount, start)
	case "hybrid":
		// ponytail: v1 摘要需要 Provider Chat 调用，Phase2 暂降级为 truncate。
		// 等摘要逻辑实现后再接 hybrid 完整路径。
		if in.Config.Compression.Enabled {
			// TODO: 同步摘要。当前先直接降级 truncate。
		}
		return m.truncate(ctx, in, units, budget, stategy, originalCount, start)
	}
	return nil, fmt.Errorf("%w: unhandled strategy %q", ErrContextBuildFailed, stategy)
}

// truncate 按 unit 截断最旧的可删除 unit 直到不超限。
func (m *Manager) truncate(ctx stdctx.Context, in BuildInput, units []messageUnit, budget Budget, strategy string, originalCount int, start time.Time) (*BuildOutput, error) {
	truncated := 0
	for {
		// 找最旧的可删除 unit
		idx := -1
		for i, u := range units {
			if !u.Protected {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("%w: no deletable unit left", ErrContextOverflow)
		}
		units = append(units[:idx], units[idx+1:]...)
		truncated++

		msgs := collectAllMessages(units)
		req := copyRequest(in.Request)
		req.Messages = msgs
		tokens, err := in.Provider.EstimateInputTokens(ctx, &req)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTokenEstimationFailed, err)
		}
		if tokens <= budget.Input {
			return &BuildOutput{
				Request:         req,
				InputTokens:    tokens,
				InputBudget:     budget.Input,
				EffectiveWindow: budget.EffectiveWindow,
				Metadata: BuildMetadata{
					Strategy:          strategy,
					OriginalMessages:  originalCount,
					FinalMessages:    len(msgs),
					TruncatedUnits:   truncated,
					BuildDuration:    time.Since(start),
				},
			}, nil
		}
	}
}

// groupUnits 按消息序列构造单元并校验序列合法性。
func groupUnits(messages []provider.Message, currentTurnStart int) ([]messageUnit, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if currentTurnStart < 0 || currentTurnStart >= len(messages) {
		return nil, fmt.Errorf("%w: current_turn_start %d out of range", ErrInvalidMessageSequence, currentTurnStart)
	}
	if messages[currentTurnStart].Role != "user" {
		return nil, fmt.Errorf("%w: current_turn_start %d is not user", ErrInvalidMessageSequence, currentTurnStart)
	}

	var units []messageUnit
	i := 0
	// 开头的 system 消息各自成 unit
	for i < len(messages) && messages[i].Role == "system" {
		units = append(units, messageUnit{
			Messages:  []provider.Message{messages[i]},
			Protected: true,
		})
		i++
	}
	// 第一个非 system 必须是 user
	if i < len(messages) && messages[i].Role != "user" {
		return nil, fmt.Errorf("%w: first non-system message must be user", ErrInvalidMessageSequence)
	}
	// 按 turn 分组
	for i < len(messages) {
		if messages[i].Role != "user" {
			return nil, fmt.Errorf("%w: turn must start with user at index %d", ErrInvalidMessageSequence, i)
		}
		turnStart := i
		i++
		// 收集到下一个 user 之前
		pendingCalls := make(map[string]string) // call_id -> func_name
		for i < len(messages) && messages[i].Role != "user" {
			msg := messages[i]
			switch msg.Role {
			case "assistant":
				if len(msg.ToolCalls) > 0 {
					for _, tc := range msg.ToolCalls {
						if tc.ID == "" {
							return nil, fmt.Errorf("%w: tool call id empty at %d", ErrInvalidMessageSequence, i)
						}
						if _, exists := pendingCalls[tc.ID]; exists {
							return nil, fmt.Errorf("%w: duplicate tool call id %q", ErrInvalidMessageSequence, tc.ID)
						}
						pendingCalls[tc.ID] = tc.Function.Name
					}
				}
			case "tool":
				if msg.ToolCallID == "" {
					return nil, fmt.Errorf("%w: tool result without tool_call_id at %d", ErrInvalidMessageSequence, i)
				}
				if _, ok := pendingCalls[msg.ToolCallID]; !ok {
					return nil, fmt.Errorf("%w: orphan tool result %q at %d", ErrInvalidMessageSequence, msg.ToolCallID, i)
				}
				delete(pendingCalls, msg.ToolCallID)
			default:
				return nil, fmt.Errorf("%w: unexpected role %q at %d", ErrInvalidMessageSequence, msg.Role, i)
			}
			i++
		}
		if len(pendingCalls) > 0 {
			return nil, fmt.Errorf("%w: incomplete tool chain at turn starting %d", ErrInvalidMessageSequence, turnStart)
		}
		turnEnd := i
		unitMsgs := append([]provider.Message(nil), messages[turnStart:turnEnd]...)
		protected := turnStart >= currentTurnStart
		// 有 Tool calls 的 unit 不可压缩（commpressible=false）
		hasTools := false
		for _, m := range unitMsgs {
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				hasTools = true
				break
			}
			if m.Role == "tool" {
				hasTools = true
				break
			}
		}
		units = append(units, messageUnit{
			Messages:     unitMsgs,
			Protected:    protected,
			Compressible: !protected && !hasTools,
		})
	}
	return units, nil
}

// collectAllMessages 把所有 unit 的消息按顺序拼接。
func collectAllMessages(units []messageUnit) []provider.Message {
	var out []provider.Message
	for _, u := range units {
		out = append(out, u.Messages...)
	}
	return out
}

// collectProtectedMessages 把 protected unit 的消息按顺序拼接。
func collectProtectedMessages(units []messageUnit) []provider.Message {
	var out []provider.Message
	for _, u := range units {
		if u.Protected {
			out = append(out, u.Messages...)
		}
	}
	return out
}

// copyRequest 深拷贝 ChatRequest 的 Messages 字段，使其与原 Request 独立。
func copyRequest(req provider.ChatRequest) provider.ChatRequest {
	c := req
	if req.Messages != nil {
		c.Messages = append([]provider.Message(nil), req.Messages...)
		// 深拷贝 ToolCalls
		for i := range c.Messages {
			if c.Messages[i].ToolCalls != nil {
				c.Messages[i].ToolCalls = append([]provider.ToolCall(nil), c.Messages[i].ToolCalls...)
			}
		}
	}
	return c
}
