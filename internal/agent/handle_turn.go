package agent

import (
	"context"
	"fmt"

	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
)

// HandleTurn 提交 user 消息并执行完整 Agent turn（v1 direct 无 Tool 路径）。
func (m *Manager) HandleTurn(ctx context.Context, agentID string, req TurnRequest) (TurnResult, error) {
	if req.SessionID == "" {
		return TurnResult{}, fmt.Errorf("%w: empty session_id", ErrAgentInvalidRequest)
	}
	if req.TurnID == "" {
		return TurnResult{}, fmt.Errorf("%w: empty turn_id", ErrAgentInvalidRequest)
	}
	if req.Content == "" {
		return TurnResult{}, fmt.Errorf("%w: empty content", ErrAgentInvalidRequest)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TurnResult{}, ErrAgentManagerClosed
	}
	a, ok := m.agents[agentID]
	m.mu.Unlock()
	if !ok {
		return TurnResult{}, fmt.Errorf("%w: %s", ErrAgentNotFound, agentID)
	}
	if a.status == StatusStopped {
		return TurnResult{}, fmt.Errorf("%w: %s", ErrAgentStopped, agentID)
	}
	if a.status == StatusPaused {
		return TurnResult{}, fmt.Errorf("%w: %s", ErrAgentPaused, agentID)
	}

	p, perr := m.deps.Providers.Get(a.provider)
	if perr != nil {
		return TurnResult{}, fmt.Errorf("agent: provider %q gone: %w", a.provider, perr)
	}

	var result TurnResult
	var onQueued func(int)
	if req.Emit != nil {
		onQueued = func(position int) {
			req.Emit(TurnEvent{Kind: "queued", Position: &position})
		}
	}

	err := m.deps.Sessions.RunTurn(ctx, req.SessionID, req.TurnID, onQueued,
		func(turnCtx context.Context, turn *session.Turn) error {
			r, e := m.runDirectTurn(turnCtx, turn, req, a, p)
			result = r
			return e
		})
	if err != nil && m.deps.Logger != nil {
		m.deps.Logger.Error("agent.HandleTurn", err, "agent", agentID, "session", req.SessionID, "turn", req.TurnID)
	}
	return result, err
}

// runDirectTurn 执行无 Tool 的 direct turn：AppendUser → 组装 ChatRequest → Context.Build → Provider.Chat → Append final assistant。
func (m *Manager) runDirectTurn(
	ctx context.Context,
	turn *session.Turn,
	req TurnRequest,
	a *agentBinding,
	p provider.Provider,
) (TurnResult, error) {
	// 1. AppendUser
	if _, err := turn.AppendUser(req.Content, req.Metadata); err != nil {
		return TurnResult{}, err
	}

	// 2. 获取 Session snapshot 组装 canonical ChatRequest
	snap, err := turn.Snapshot()
	if err != nil {
		return TurnResult{}, err
	}

	// 构造 Messages：system prompt（如果有）+ 历史消息
	var msgs []provider.Message
	if a.sysPrompt != "" {
		msgs = append(msgs, provider.Message{Role: "system", Content: a.sysPrompt})
	}
	for _, sm := range snap.Messages {
		msgs = append(msgs, sm.Payload)
	}

	// 找 ModelInfo
	var modelInfo provider.ModelInfo
	found := false
	for _, mi := range p.Models() {
		if mi.ID == a.model {
			modelInfo = mi
			found = true
			break
		}
	}
	if !found {
		return TurnResult{}, fmt.Errorf("agent: model %q not found", a.model)
	}

	// CurrentTurnStart 指向最新 user 消息
	maxTokens := a.maxTokens
	currentTurnStart := len(msgs) - 1
	// 从后往前找最后一个 user
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			currentTurnStart = i
			break
		}
	}

	// 3. Context Build
	ctxReq := provider.ChatRequest{
		Model:     a.model,
		Messages:  msgs,
		MaxTokens: &maxTokens,
	}
	// ponytail: v1 不含 Tool 投影，直接用 canonical 给 Context（无需 alias 变换）。
	// Context Manager 直接用 Request 的 messages 构建。
	// Context 太长 会自动 truncate，使用 Agent 的 context 配置（根+override）。
	// 圈 - 暫跳构建直接送到Provider， 在 v1 direct 不绕 Context（Tool为空 时 无 复杂性，但 仍 验预算）：
	cfg := m.resolveAgentContextConfig(a)
	built, err := m.deps.Context.Build(ctx, ctxwindow.BuildInput{
		Provider:         p,
		Model:            modelInfo,
		Request:          ctxReq,
		Config:           cfg,
		CurrentTurnStart: currentTurnStart,
	})
	if err != nil {
		return TurnResult{}, err
	}
	// 4. Provider.Chat（按 stream 选择流式/非流式路径，流式路径 emit 中间事件）
	assistantMsg, usage, err := m.callProvider(ctx, &built.Request, p, req)
	if err != nil {
		return TurnResult{Usage: usage}, err
	}

	// 6. Append final assistant（无 Tool 路径单条 batch）
	appended, err := turn.Append([]session.AppendInput{{Message: assistantMsg}})
	if err != nil {
		return TurnResult{Usage: usage}, err
	}
	if len(appended) > 0 && req.Emit != nil {
		tcc := 0
		req.Emit(TurnEvent{Kind: "assistant_done", Assistant: &appended[0], Usage: &usage, ToolCallCount: &tcc})
	} else if len(appended) == 0 {
		return TurnResult{Usage: usage}, fmt.Errorf("%w: assistant append empty", ErrAgentProviderProtocol)
	}

	return TurnResult{
		Message:       appended[0],
		Usage:         usage,
		ToolCallCount: 0,
	}, nil
}

// resolveAgentContextConfig 从 root + Agent override 解析 effective Context config。
func (m *Manager) resolveAgentContextConfig(a *agentBinding) config.ContextConfig {
	for _, ag := range m.deps.Config.Agents {
		if ag.ID == a.id {
			return config.ResolveContextConfig(m.deps.Config.Context, ag.Context)
		}
	}
	return m.deps.Config.Context
}

var _ = fmt.Sprintf

// callProvider 按 req.Stream 选择流式/非流式执行 Path；二者返回累积后的 assistant message + usage + 错误。
// 流式路径在首个 chunk 前出现错误时由 retryingProvider 负责重试，首个可见 chunk 后不再重试。
func (m *Manager) callProvider(
	ctx context.Context,
	req *provider.ChatRequest,
	p provider.Provider,
	turnReq TurnRequest,
) (provider.Message, provider.Usage, error) {
	if !turnReq.Stream {
		return m.callChat(ctx, req, p)
	}
	return m.callStream(ctx, req, p, turnReq)
}

// callChat 是非流式 fallback：直接用 Provider.Chat。
func (m *Manager) callChat(ctx context.Context, req *provider.ChatRequest, p provider.Provider) (provider.Message, provider.Usage, error) {
	resp, err := p.Chat(ctx, req)
	if err != nil {
		return provider.Message{}, resp.Usage, err
	}
	return provider.Message{
		Role:             "assistant",
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		Refusal:          resp.Refusal,
		ToolCalls:        resp.ToolCalls,
	}, resp.Usage, nil
}

// callStream 用 Provider.StreamChat，累积 delta 并通过 Emit 回调发布。
// v1 无 Tool 路径，ToolCalls 增量会累积到最终 message；Tool 描述的反查与执行在 Phase 3 补全。
func (m *Manager) callStream(
	ctx context.Context,
	req *provider.ChatRequest,
	p provider.Provider,
	turnReq TurnRequest,
) (provider.Message, provider.Usage, error) {
	// 标记 stream 请求；某些 adapter 靠此 flag 在 request body 中加 stream:true。
	req.Stream = true
	ch, err := p.StreamChat(ctx, req)
	if err != nil {
		return provider.Message{}, provider.Usage{}, err
	}
	var out provider.Message
	var usage provider.Usage
	started := false
	emit := turnReq.Emit
	for chunk := range ch {
		if chunk.Error != nil {
			return out, usage, chunk.Error
		}
		d := chunk.Delta
		// 首个 chunk 带 role 时宣告 assistant_start。
		if !started && d.Role == "assistant" {
			started = true
			if emit != nil {
				emit(TurnEvent{Kind: "assistant_start"})
			}
		} else if !started && (d.Content != "" || d.ReasoningContent != "" || d.Refusal != "") {
			started = true
			if emit != nil {
				emit(TurnEvent{Kind: "assistant_start"})
			}
		}
		// 累积各字段。
		out.Role = pickNonEmpty(out.Role, d.Role)
		out.Content += d.Content
		out.ReasoningContent += d.ReasoningContent
		out.Refusal += d.Refusal
		if len(d.ToolCalls) > 0 {
			out.ToolCalls = append(out.ToolCalls, d.ToolCalls...)
		}
		// emit delta：reasoning 与 正常 content 分开帧；二者可同时为空但均发帧（文档允许空 delta）。
		if emit != nil {
			if d.ReasoningContent != "" {
				emit(TurnEvent{Kind: "reasoning_delta", Delta: d.ReasoningContent})
			}
			if d.Content != "" {
				emit(TurnEvent{Kind: "assistant_delta", Delta: d.Content})
			}
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
	}
	if out.Role == "" {
		out.Role = "assistant"
	}
	return out, usage, nil
}

// pickNonEmpty 若 b 非空则返回 b，否则返回 a。用于累积 role。
func pickNonEmpty(a, b string) string {
	if b != "" {
		return b
	}
	return a
}
