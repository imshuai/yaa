package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/storage"
	"github.com/imshuai/yaa/internal/tool"
)

// localEchoTool 是 agent 包内的 Tool stub，与 tool 包 echoTool 行为类似：回 params["msg"]。
type localEchoTool struct{}

func (localEchoTool) Name() string        { return "echo" }
func (localEchoTool) Description() string { return "echoes back the msg argument" }
func (localEchoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`)
}

func (localEchoTool) Execute(ctx context.Context, scope tool.ExecutionScope, params map[string]any) (tool.ToolResult, error) {
	msg, _ := params["msg"].(string)
	return tool.ToolResult{Content: "echo: " + msg}, nil
}

// newToolLoopEnv 构造一套完整链路：tool.Manager + 注册 echo + Provider mock + Agent Manager（agent-tools allowlist=[echo]）。
// serverCount 在 mock server 内全局自增，便于按计数切换响应。
func newToolLoopEnv(t *testing.T, handler func(callIdx int) map[string]any, totalServiceCalls *int32) (*Manager, *session.Manager) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(totalServiceCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(handler(int(idx)))
	}))
	t.Cleanup(srv.Close)

	provCfg := config.ProviderConfig{
		ID:      "p1",
		Type:    "openai",
		APIKey:  "k",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
		Models: []config.ModelConfig{{
			ID:            "m",
			Name:          "M",
			ContextWindow: 4096,
			MaxOutput:     2048,
		}},
	}
	pm, err := provider.NewManager([]config.ProviderConfig{provCfg})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pm.Close() })

	store, _ := storage.NewMemory(nil)
	sessCfg := config.SessionConfig{
		MaxMessages: 100, MaxMessageBytes: 1024 * 1024, TTL: 24 * time.Hour,
		MaxLifetime: 720 * time.Hour, Persist: true, MaxSessionsPerAgent: 5, CleanupInterval: time.Minute,
	}
	sm := session.NewManager(sessCfg, store, nil, session.ManagerOptions{
		AgentExists:   func(id string) bool { return id == "agent-tools" },
		AgentOverride: func(id string) *config.SessionOverride { return nil },
	})
	if err := sm.Restore(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := sm.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sm.Shutdown(context.Background()) })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{provCfg},
		Agents: []config.AgentConfig{{
			ID:        "agent-tools",
			Name:      "Tools Agent",
			Provider:  "p1",
			Model:     "m",
			MaxTokens: 1000,
			Tools:     []string{"echo"},
		}},
		Context: config.ContextConfig{MaxTokens: 0, ReservedTokens: 1500, Strategy: "truncate"},
		Session: sessCfg,
	}

	tm, err := tool.NewManager(tool.Dependencies{Config: cfg, Providers: pm})
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.Register(localEchoTool{}); err != nil {
		t.Fatal(err)
	}

	agm, err := NewManager(Dependencies{
		Config:    cfg,
		Sessions:  sm,
		Context:   ctxwindow.NewManager(),
		Providers: pm,
		Tools:     tm,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agm.Shutdown(context.Background()) })

	return agm, sm
}

// openAIToolCallsResp 构造 OpenAI 兼容 chat completion 响应。
// calls 为 nil → final assistant(content)。finishReason 必须为 "tool_calls" 或 "stop"。
func openAIToolCallsResp(calls []map[string]any, content string, finishReason string) map[string]any {
	msg := map[string]any{"role": "assistant"}
	if content != "" {
		msg["content"] = content
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	return map[string]any{
		"id":    "resp1",
		"model": "m",
		"choices": []map[string]any{{
			"message":       msg,
			"finish_reason": finishReason,
		}},
		"usage": map[string]any{},
	}
}

func toolCallWire(id, name, args string) map[string]any {
	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
}

func TestAgentHandleTurnWithToolLoop(t *testing.T) {
	var total int32
	agm, sm := newToolLoopEnv(t, func(idx int) map[string]any {
		if idx == 1 {
			// round 1：assistant 含 tool_calls={echo,"hi"}，finish_reason=tool_calls。
			return openAIToolCallsResp(
				[]map[string]any{toolCallWire("call_1", "echo", `{"msg":"hi"}`)},
				"", "tool_calls")
		}
		// round 2：final assistant，无 tool_calls。
		return openAIToolCallsResp(nil, "done", "stop")
	}, &total)

	ctx := context.Background()
	s, err := sm.Create(ctx, session.CreateRequest{AgentID: "agent-tools"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agm.HandleTurn(ctx, "agent-tools", TurnRequest{
		SessionID: s.ID,
		TurnID:    "turn_loop1",
		Content:   "please echo",
	})
	if err != nil {
		t.Fatalf("HandleTurn failed: %v", err)
	}
	if result.ToolCallCount != 1 {
		t.Fatalf("ToolCallCount = %d, want 1", result.ToolCallCount)
	}
	if result.Message.Payload.Role != "assistant" || result.Message.Payload.Content != "done" {
		t.Fatalf("final message = %+v, want content=done", result.Message.Payload)
	}
	if atomic.LoadInt32(&total) != 2 {
		t.Fatalf("provider called %d times, want 2 (round1 tool + round2 final)", total)
	}

	// Session 序列：user, assistant(tool_calls), tool, assistant(final) = 4。
	got, _ := sm.Get(ctx, s.ID)
	if len(got.Messages) != 4 {
		t.Fatalf("session messages = %d, want 4", len(got.Messages))
	}
	if got.Messages[1].Payload.Role != "assistant" || len(got.Messages[1].Payload.ToolCalls) != 1 {
		t.Fatalf("msg[1] not assistant tool_call: %+v", got.Messages[1].Payload)
	}
	if got.Messages[2].Payload.Role != "tool" || got.Messages[2].Payload.Content != "echo: hi" {
		t.Fatalf("msg[2] not tool result: %+v", got.Messages[2].Payload)
	}
	if got.Messages[3].Payload.Role != "assistant" || got.Messages[3].Payload.Content != "done" {
		t.Fatalf("msg[3] not final assistant: %+v", got.Messages[3].Payload)
	}
	// canonical 名持久化（不是 wire alias）。
	if got.Messages[1].Payload.ToolCalls[0].Function.Name != "echo" {
		t.Fatalf("tool_call function name = %q, want canonical echo", got.Messages[1].Payload.ToolCalls[0].Function.Name)
	}
}

func TestAgentHandleTurnUnknownToolAlias(t *testing.T) {
	var total int32
	agm, sm := newToolLoopEnv(t, func(idx int) map[string]any {
		// round 1：返回 alias "not_registered"，proj.ResolveExecutable 必然失败。
		return openAIToolCallsResp(
			[]map[string]any{toolCallWire("call_x", "not_registered", `{"msg":""}`)},
			"", "tool_calls")
	}, &total)

	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-tools"})
	_, err := agm.HandleTurn(ctx, "agent-tools", TurnRequest{
		SessionID: s.ID,
		TurnID:    "turn_unknown",
		Content:   "x",
	})
	if err == nil || !errors.Is(err, ErrAgentProviderProtocol) {
		t.Fatalf("expected ErrAgentProviderProtocol, got %v", err)
	}
	// Session 不应有 partial assistant/tool，只有 user。
	got, _ := sm.Get(ctx, s.ID)
	if len(got.Messages) != 1 || got.Messages[0].Payload.Role != "user" {
		t.Fatalf("session should only have user, got %+v", got.Messages)
	}
}

func TestAgentHandleTurnRoundLimit(t *testing.T) {
	var total int32
	agm, sm := newToolLoopEnv(t, func(idx int) map[string]any {
		// 每轮恒返回 tool_calls，必然触发 maxToolRounds=8 上限。
		return openAIToolCallsResp(
			[]map[string]any{toolCallWire("call_"+itoa(idx), "echo", `{"msg":"x"}`)},
			"", "tool_calls")
	}, &total)

	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-tools"})
	_, err := agm.HandleTurn(ctx, "agent-tools", TurnRequest{
		SessionID: s.ID,
		TurnID:    "turn_limit",
		Content:   "x",
	})
	if err == nil || !errors.Is(err, ErrAgentToolRoundLimit) {
		t.Fatalf("expected ErrAgentToolRoundLimit, got %v", err)
	}
}

func TestAgentHandleTurnInvalidArgsObject(t *testing.T) {
	var total int32
	agm, sm := newToolLoopEnv(t, func(idx int) map[string]any {
		// arguments 是数组而非 object，应被 ErrAgentProviderProtocol 拒绝。
		return openAIToolCallsResp(
			[]map[string]any{toolCallWire("call_a", "echo", `[1,2,3]`)},
			"", "tool_calls")
	}, &total)

	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-tools"})
	_, err := agm.HandleTurn(ctx, "agent-tools", TurnRequest{
		SessionID: s.ID,
		TurnID:    "turn_invalid_args",
		Content:   "x",
	})
	if err == nil || !errors.Is(err, ErrAgentProviderProtocol) {
		t.Fatalf("expected ErrAgentProviderProtocol for non-object args, got %v", err)
	}
}

// itoa 轻量整转串，避免 import strconv 仅此一处。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
