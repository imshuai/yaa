package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/tool"
)

// HandleTurn 提交 user 消息并执行完整 Agent turn（含 Tool loop，docs/agent.md §4）。
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

// runDirectTurn 执行完整 direct turn（含 Tool loop）：
//
//	AppendUser
//	重复最多 maxToolRounds 轮：
//	  1) Snapshot canonical history → ToToolDefs 冻结 projection
//	  2) 组装 Tools=空 canonical ChatRequest → projection.ProjectRequest 注入 alias
//	  3) Context.Build（看到最终 wire alias）
//	  4) callProvider（direct/stream 二选一）
//	  5) 无 ToolCalls → 单批 Append final assistant → emit assistant_done → 返回
//	     有 ToolCalls → resolveToolCalls 校验+反查 → tool.Manager.ExecuteBatch → 单批 Append(assistant+tool results)
//	     → 继续下一轮
//	已达 maxToolRounds 时若 Provider 仍返回 tool_calls 返回 ErrAgentToolRoundLimit（不提交 partial）。
//
// 关键不变式：
//   - Provider 响应的 wire alias 不进 Session/ExecuteBatch；append 前用 projection 改写为 canonical。
//   - final 与每轮 tool unit 均按 Session 单批 unit 提交（assistant + tool results 一一对应）。
//   - Usage 逐轮累加；ToolCallCount 累计进入 ExecuteBatch 的 calls 数量；canonical 名不外泄 wire alias。
func (m *Manager) runDirectTurn(
	ctx context.Context,
	turn *session.Turn,
	req TurnRequest,
	a *agentBinding,
	p provider.Provider,
) (TurnResult, error) {
	// 1. AppendUser（首个写操作，原子提交当前 user）。
	if _, err := turn.AppendUser(req.Content, req.Metadata); err != nil {
		return TurnResult{}, err
	}

	var totalUsage = provider.Usage{}
	var toolCallTotal = 0

	for rounds := 0; ; rounds++ {
		// 2. Snapshot canonical history（每轮都拿最新，含上一轮已提交的 tool unit）。
		snap, err := turn.Snapshot()
		if err != nil {
			return TurnResult{Usage: totalUsage}, err
		}
		// canonical messages：system prompt + snapshot.Messages.Payload。
		var canonicalMsgs []provider.Message
		if a.sysPrompt != "" {
			canonicalMsgs = append(canonicalMsgs, provider.Message{Role: "system", Content: a.sysPrompt})
		}
		for _, sm := range snap.Messages {
			canonicalMsgs = append(canonicalMsgs, sm.Payload)
		}
		// 找 ModelInfo（每轮重找，惰性稳定）。
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
			return TurnResult{Usage: totalUsage}, fmt.Errorf("agent: model %q not found", a.model)
		}
		// currentTurnStart 指向最后一条 user。
		currentTurnStart := len(canonicalMsgs) - 1
		for i := len(canonicalMsgs) - 1; i >= 0; i-- {
			if canonicalMsgs[i].Role == "user" {
				currentTurnStart = i
				break
			}
		}

		// 3. 冻结 projection（docs/agent.md §4 step 3）。Tools 未注入时退化为无投影。
		var proj *tool.ProviderToolProjection
		if m.deps.Tools != nil {
			proj, err = m.deps.Tools.ToToolDefs(a.id, canonicalMsgs)
			if err != nil {
				return TurnResult{Usage: totalUsage}, err
			}
		}

		// 4. 组装 canonical ChatRequest（Tools 留空，由 projection 注入）。
		maxTokens := a.maxTokens
		ctxReq := provider.ChatRequest{
			Model:     a.model,
			Messages:  canonicalMsgs,
			MaxTokens: &maxTokens,
		}
		// 5. ProjectRequest 注入 alias definitions 与历史 ToolCalls 名投影。
		var providerReq provider.ChatRequest
		if proj != nil {
			pr, perr := proj.ProjectRequest(ctxReq)
			if perr != nil {
				return TurnResult{Usage: totalUsage}, perr
			}
			providerReq = pr
		} else {
			providerReq = ctxReq
		}

		// 6. Context.Build 看到最终 wire alias。
		cfg := m.resolveAgentContextConfig(a)
		built, err := m.deps.Context.Build(ctx, ctxwindow.BuildInput{
			Provider:         p,
			Model:            modelInfo,
			Request:          providerReq,
			Config:           cfg,
			CurrentTurnStart: currentTurnStart,
		})
		if err != nil {
			return TurnResult{Usage: totalUsage}, err
		}

		// 7. callProvider（direct 或 stream）。
		assistantMsg, usage, err := m.callProvider(ctx, &built.Request, p, req)
		totalUsage.PromptTokens += usage.PromptTokens
		totalUsage.CompletionTokens += usage.CompletionTokens
		totalUsage.TotalTokens += usage.TotalTokens
		if err != nil {
			return TurnResult{Usage: totalUsage}, err
		}

		// 8. Tool 判定：仅当本轮定义了 projection 且 Provider 返回 ToolCalls 时进入 tool loop。
		var calls []provider.ToolCall
		if len(assistantMsg.ToolCalls) > 0 && proj != nil {
			resolved, rerr := m.resolveToolCalls(assistantMsg.ToolCalls, proj)
			if rerr != nil {
				return TurnResult{Usage: totalUsage}, rerr
			}
			calls = resolved
		}

		// 9. 无 Tool call：final 路径，单条 Append。
		if len(calls) == 0 {
			appended, aerr := turn.Append([]session.AppendInput{{Message: assistantMsg}})
			if aerr != nil {
				return TurnResult{Usage: totalUsage}, aerr
			}
			if len(appended) == 0 {
				return TurnResult{Usage: totalUsage}, fmt.Errorf("%w: assistant append empty", ErrAgentProviderProtocol)
			}
			if req.Emit != nil {
				tcc := toolCallTotal
				req.Emit(TurnEvent{Kind: "assistant_done", Assistant: &appended[0], Usage: &totalUsage, ToolCallCount: &tcc})
			}
			return TurnResult{
				Message:       appended[0],
				Usage:         totalUsage,
				ToolCallCount: toolCallTotal,
			}, nil
		}

		// 10. 下一轮会进入 tool round；先检查上限（docs/agent.md §4 step 8：最多 maxToolRounds）。
		// 本轮已形成的 calls 必须能被 append 到 Session 才算可继续；上限检查在执行/提交前。
		if rounds+1 >= maxToolRounds {
			return TurnResult{Usage: totalUsage}, ErrAgentToolRoundLimit
		}

		// 11. ExecuteBatch（ExecuteScope with AgentID/SessionID）。
		scope := tool.ExecutionScope{AgentID: a.id, SessionID: req.SessionID}
		results, eerr := m.deps.Tools.ExecuteBatch(ctx, scope, calls)
		if eerr != nil {
			return TurnResult{Usage: totalUsage}, eerr
		}
		if len(results) != len(calls) {
			return TurnResult{Usage: totalUsage}, fmt.Errorf("%w: batch size mismatch", ErrAgentProviderProtocol)
		}

		// 12. 把 canonical 名写回 assistantMsg.ToolCalls 以便 Append（Session 永远只持有 canonical）。
		for i := range calls {
			assistantMsg.ToolCalls[i].Function.Name = calls[i].Function.Name
		}

		// 13. 构造单批 unit: [assistant(tool_calls), tool, tool, ...]，一一对应；Session classify 要求严格匹配。
		batch := make([]session.AppendInput, 0, 1+len(calls))
		batch = append(batch, session.AppendInput{Message: assistantMsg})
		for i, r := range results {
			batch = append(batch, session.AppendInput{
				Message: provider.Message{
					Role:       "tool",
					ToolCallID: calls[i].ID,
					Content:    r.Content,
				},
			})
		}
		if _, aerr := turn.Append(batch); aerr != nil {
			return TurnResult{Usage: totalUsage}, aerr
		}
		toolCallTotal += len(calls)

		// 14. 流式模式下按 docs/agent.md §5+§7 发布 tool_call / tool_result 进度事件。
		if req.Emit != nil {
			for _, c := range calls {
				cc := c
				req.Emit(TurnEvent{Kind: "tool_call", ToolCall: &cc})
			}
			for i, r := range results {
				req.Emit(TurnEvent{
					Kind: "tool_result",
					ToolResult: &ToolResultEvent{
						ToolCallID: calls[i].ID,
						Name:       calls[i].Function.Name,
						Content:    r.Content,
						IsError:    r.IsError,
					},
				})
			}
		}
	}
}

// resolveToolCalls 反查 Provider 返回的 wire alias 到 canonical name。
// 校验：每个 Arguments 是单个合法 JSON object 且无 trailing token；alias 通过
// projection.ResolveExecutable 精确反查；任一失败返回 ErrAgentProviderProtocol，
// 整批 not executed、不提交 partial（docs/agent.md §5）。
func (m *Manager) resolveToolCalls(aliasCalls []provider.ToolCall, proj *tool.ProviderToolProjection) ([]provider.ToolCall, error) {
	out := make([]provider.ToolCall, len(aliasCalls))
	for i, c := range aliasCalls {
		if !isValidArgsObject(c.Function.Arguments) {
			return nil, fmt.Errorf("%w: invalid tool call arguments", ErrAgentProviderProtocol)
		}
		canonical, ok := proj.ResolveExecutable(c.Function.Name)
		if !ok {
			return nil, fmt.Errorf("%w: unknown tool alias %q", ErrAgentProviderProtocol, c.Function.Name)
		}
		out[i] = c
		out[i].Function.Name = canonical
	}
	return out, nil
}

// isValidArgsObject 校验 s 是单个 JSON object 且无 trailing token（docs/agent.md §5 step 5）。
// 空串、数组、标量、object 后接字符均非法。
func isValidArgsObject(s string) bool {
	if s == "" {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	var v any
	if err := dec.Decode(&v); err != nil {
		return false
	}
	if _, ok := v.(map[string]any); !ok {
		return false
	}
	if dec.More() {
		return false
	}
	return true
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
// ToolCalls 增量按 chunk 累积到最终 message；request 与反查 Tool 名由 Agent 主 loop 外层处理。
func (m *Manager) callStream(
	ctx context.Context,
	req *provider.ChatRequest,
	p provider.Provider,
	turnReq TurnRequest,
) (provider.Message, provider.Usage, error) {
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
		out.Role = pickNonEmpty(out.Role, d.Role)
		out.Content += d.Content
		out.ReasoningContent += d.ReasoningContent
		out.Refusal += d.Refusal
		if len(d.ToolCalls) > 0 {
			out.ToolCalls = append(out.ToolCalls, d.ToolCalls...)
		}
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
