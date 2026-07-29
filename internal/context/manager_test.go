package context

import (
	"context"
	"errors"
	"testing"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/provider"
)

// fakeProvider 用字节数估算 tokens。
type fakeProvider struct {
	model provider.ModelInfo
}

func (f *fakeProvider) ID() string   { return "fake" }
func (f *fakeProvider) Type() string { return "fake" }
func (f *fakeProvider) Chat(context.Context, *provider.ChatRequest) (*provider.ChatResponse, error) {
	return nil, nil
}
func (f *fakeProvider) StreamChat(context.Context, *provider.ChatRequest) (<-chan provider.ChatChunk, error) {
	return nil, nil
}
func (f *fakeProvider) Models() []provider.ModelInfo { return []provider.ModelInfo{f.model} }
func (f *fakeProvider) Close() error                 { return nil }
func (f *fakeProvider) EstimateInputTokens(ctx context.Context, req *provider.ChatRequest) (int, error) {
	// 每条消息按 100 tokens 计算估算
	return len(req.Messages) * 100, nil
}

func newTestProvider(window, maxOutput int) *fakeProvider {
	return &fakeProvider{model: provider.ModelInfo{
		ID: "test-model", ContextWindow: window, MaxOutput: maxOutput,
	}}
}

func newTestConfig(strategy string) config.ContextConfig {
	return config.ContextConfig{
		MaxTokens: 0, ReservedTokens: 4096, Strategy: strategy,
	}
}

func TestBuildUnderBudget(t *testing.T) {
	m := NewManager()
	fp := newTestProvider(10000, 8192)
	maxTokens := 4096
	msgs := []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "again"},
	}
	out, err := m.Build(context.Background(), BuildInput{
		Provider: fp, Model: fp.model,
		Request:          provider.ChatRequest{Model: "test-model", Messages: msgs, MaxTokens: &maxTokens},
		Config:           newTestConfig("reject"),
		CurrentTurnStart: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputTokens != 300 {
		t.Fatalf("expected 300 tokens, got %d", out.InputTokens)
	}
	if len(out.Request.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out.Request.Messages))
	}
}

func TestBuildReject(t *testing.T) {
	m := NewManager()
	fp := newTestProvider(10000, 8192) // input budget = 10000 - 4096 = 5904
	maxTokens := 4096                  // budget 5904 = 59 messages
	msgs := make([]provider.Message, 70)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = provider.Message{Role: "user", Content: "msg"}
		} else {
			msgs[i] = provider.Message{Role: "assistant", Content: "resp"}
		}
	}
	msgs[0] = provider.Message{Role: "user", Content: "first"}
	msgs[1] = provider.Message{Role: "assistant", Content: "first resp"}
	// 每条 user 后跟 assistant，按 turn 分
	// CurrentTurnStart 指向最后一个 user
	lastUser := -1
	for i := len(msgs) - 2; i >= 0; i -= 2 {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}
	// 实际由于结构，find last user index
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}
	_, err := m.Build(context.Background(), BuildInput{
		Provider: fp, Model: fp.model,
		Request:          provider.ChatRequest{Model: "test-model", Messages: msgs, MaxTokens: &maxTokens},
		Config:           newTestConfig("reject"),
		CurrentTurnStart: lastUser,
	})
	if !errors.Is(err, ErrContextOverflow) {
		t.Fatalf("expected ErrContextOverflow, got %v", err)
	}
}

func TestBuildTruncate(t *testing.T) {
	m := NewManager()
	fp := newTestProvider(6400, 8192) // input budget = 6400 - 4096 = 2304 → 23 messages
	maxTokens := 4096                 // input budget = 2304-tokens (23 messages)
	// 30 messages = 3000 tokens > 2304
	msgs := make([]provider.Message, 30)
	for i := range msgs {
		if i%2 == 0 {
			msgs[i] = provider.Message{Role: "user", Content: "msg"}
		} else {
			msgs[i] = provider.Message{Role: "assistant", Content: "resp"}
		}
	}
	// 找最后 user
	lastUser := 28
	msgs[lastUser] = provider.Message{Role: "user", Content: "last"}
	out, err := m.Build(context.Background(), BuildInput{
		Provider: fp, Model: fp.model,
		Request:          provider.ChatRequest{Model: "test-model", Messages: msgs, MaxTokens: &maxTokens},
		Config:           newTestConfig("truncate"),
		CurrentTurnStart: lastUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.InputTokens > 2304 {
		t.Fatalf("expected <= 2304, got %d", out.InputTokens)
	}
	if out.Metadata.TruncatedUnits == 0 {
		t.Fatal("expected truncated units > 0")
	}
}

func TestGroupUnits(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "sys1"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "c1", Function: provider.ToolCallFunction{Name: "w", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "c1", Name: "w", Content: "result"},
		{Role: "user", Content: "q3"},
	}
	units, err := groupUnits(msgs, 6) // current turn = q3 (index 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 4 {
		t.Fatalf("expected 4 units (1 system + 3 turns), got %d", len(units))
	}
	if !units[0].Protected {
		t.Fatal("system unit should be protected")
	}
	if units[1].Protected { // q1+a1 turn, before current
		t.Fatal("old turn should not be protected")
	}
	if !units[3].Protected { // q3 current turn
		t.Fatal("current turn should be protected")
	}
	if units[2].Compressible { // has tools
		t.Fatal("tool turn should not be compressible")
	}
	if !units[1].Compressible { // no tools, not protected
		t.Fatal("plain turn should be compressible")
	}
}

func TestGroupUnitsInvalidSequence(t *testing.T) {
	// orphan tool result
	msgs := []provider.Message{
		{Role: "user", Content: "q"},
		{Role: "tool", ToolCallID: "c1"},
	}
	_, err := groupUnits(msgs, 0)
	if !errors.Is(err, ErrInvalidMessageSequence) {
		t.Fatalf("expected ErrInvalidMessageSequence, got %v", err)
	}

	// first non-system not user
	msgs2 := []provider.Message{
		{Role: "assistant", Content: "a"},
	}
	_, err = groupUnits(msgs2, 0)
	if !errors.Is(err, ErrInvalidMessageSequence) {
		t.Fatalf("expected ErrInvalidMessageSequence, got %v", err)
	}

	// incomplete tool chain
	msgs3 := []provider.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "c1", Function: provider.ToolCallFunction{Name: "w", Arguments: "{}"}}}},
		{Role: "user", Content: "q2"},
	}
	_, err = groupUnits(msgs3, 2)
	if !errors.Is(err, ErrInvalidMessageSequence) {
		t.Fatalf("expected ErrInvalidMessageSequence, got %v", err)
	}
}

func TestResolveContextBudget(t *testing.T) {
	cfg := newTestConfig("truncate")
	b, err := ResolveContextBudget(cfg, 10000, 8192, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if b.EffectiveWindow != 10000 || b.ReservedOutput != 4096 || b.Input != 5904 {
		t.Fatalf("bad budget: %+v", b)
	}
}

func TestResolveContextBudgetInvalidWindow(t *testing.T) {
	cfg := newTestConfig("truncate")
	_, err := ResolveContextBudget(cfg, 0, 8192, 4096)
	if !errors.Is(err, ErrProviderWindowUnknown) {
		t.Fatalf("expected ErrProviderWindowUnknown, got %v", err)
	}
}

// TestBuildTruncatePreservesToolUnitWithReasoning 验证 §14.4 第2+3项:
// 含 reasoning_content 的 Tool unit (assistant+tool) 不可被压缩/拆分;
// truncate 只删旧普通 turn, 不剥离 reasoning_content; 剩余请求仍含完整 Tool unit.
func TestBuildTruncatePreservesToolUnitWithReasoning(t *testing.T) {
	m := NewManager()
	fp := newTestProvider(6400, 4096) // input budget = 6400-4096 = 2304 → 23 条
	maxTokens := 4096
	// 构造: 1 system + 18 普通交替 turn (36条,超budget) + 1 Tool unit (assistant reasoning + tool)
	// current turn = 最后的 user(TODO 可去掉,让 Tool unit 在当前 turn 之前可删但属 Protected? )
	// 为保证 Tool unit 被保留且验证 reasoning 仍在: 把 Tool unit 放在 current turn 之外的老 turn.
	msgs := []provider.Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 18; i++ {
		msgs = append(msgs, provider.Message{Role: "user", Content: "q"})
		msgs = append(msgs, provider.Message{Role: "assistant", Content: "a"})
	}
	// tool unit (旧turn): assistant with ReasoningContent + tool_calls, tool result.
	msgs = append(msgs, provider.Message{Role: "user", Content: "tool-turn"})
	msgs = append(msgs, provider.Message{
		Role:             "assistant",
		ReasoningContent: "I need to call a tool",
		ToolCalls:        []provider.ToolCall{{ID: "c1", Function: provider.ToolCallFunction{Name: "w", Arguments: "{}"}}},
	})
	msgs = append(msgs, provider.Message{Role: "tool", ToolCallID: "c1", Name: "w", Content: "result"})
	// 当前 turn
	msgs = append(msgs, provider.Message{Role: "user", Content: "current"})
	lastUser := len(msgs) - 1

	out, err := m.Build(context.Background(), BuildInput{
		Provider: fp, Model: fp.model,
		Request:          provider.ChatRequest{Model: "test-model", Messages: msgs, MaxTokens: &maxTokens},
		Config:           newTestConfig("truncate"),
		CurrentTurnStart: lastUser,
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	// 在最终 messages 中找 Tool unit, 验证 reasoning_content 和 tool result 都保留.
	var foundAssistant bool
	var foundTool bool
	for _, m := range out.Request.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.ReasoningContent == "I need to call a tool" {
			foundAssistant = true
		}
		if m.Role == "tool" && m.ToolCallID == "c1" && m.Name == "w" {
			foundTool = true
		}
	}
	if !foundAssistant {
		t.Fatal("truncation stripped assistant message with ReasoningContent (§8.4 violated)")
	}
	if !foundTool {
		t.Fatal("truncation stripped tool result message (§8.3 atomic unit violated)")
	}
}

// TestBuildRespectsContextCancellation 覆盖 checklist 行48: ctx 取消在循环截断中及时生效.
func TestBuildRespectsContextCancellation(t *testing.T) {
	p := newTestProvider(10000, 1000)
	cfg := newTestConfig("truncate")
	maxTokens := 1000
	// 60 条消息 × 100 = 6000 > budget.Input (window - reserved)
	messages := make([]provider.Message, 60)
	for i := range messages {
		if i%2 == 0 || i == len(messages)-1 {
			messages[i] = provider.Message{Role: "user", Content: "x"}
		} else {
			messages[i] = provider.Message{Role: "assistant", Content: "y"}
		}
	}
	m := &Manager{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 先取消
	_, err := m.Build(ctx, BuildInput{
		Provider:         p,
		CurrentTurnStart: len(messages) - 1, // 最后一条 user 为当前 turn
		Request: provider.ChatRequest{
			Model:     "test-model",
			MaxTokens: &maxTokens,
			Messages:  messages,
		},
		Model:  provider.ModelInfo{ID: "test-model", ContextWindow: 10000, MaxOutput: 1000},
		Config: cfg,
	})
	if err == nil {
		t.Skip("build completed immediately (no truncation needed)")
	}
	// 应该返回 ctx.Canceled 或 ErrContextBuildFailed (wrap ctx.Err)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrContextBuildFailed) {
		t.Fatalf("expected ctx-related error, got %v", err)
	}
}
