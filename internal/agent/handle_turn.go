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
	// 4. Provider.Chat
	resp, err := p.Chat(ctx, &built.Request)
	if err != nil {
		return TurnResult{Usage: resp.Usage}, err
	}

	// 5. 组装 final assistant Message
	assistantMsg := provider.Message{
		Role:             "assistant",
		Content:          resp.Content,
		ReasoningContent: resp.ReasoningContent,
		Refusal:          resp.Refusal,
		ToolCalls:        resp.ToolCalls,
	}

	// 6. Append final assistant（无 Tool 路径单条 batch）
	appended, err := turn.Append([]session.AppendInput{{Message: assistantMsg}})
	if err != nil {
		return TurnResult{Usage: resp.Usage}, err
	}
	if len(appended) > 0 && req.Emit != nil {
		req.Emit(TurnEvent{Kind: "assistant_done"})
	} else if len(appended) == 0 {
		return TurnResult{Usage: resp.Usage}, fmt.Errorf("%w: assistant append empty", ErrAgentProviderProtocol)
	}

	return TurnResult{
		Message:       appended[0],
		Usage:         resp.Usage,
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
