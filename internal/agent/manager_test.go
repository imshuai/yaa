package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/storage"
)

// newAgentTestEnv 构造一套完整的测试环境：memory session manager + fake provider + agent manager。
func newAgentTestEnv(t *testing.T) (*Manager, *session.Manager) {
	t.Helper()

	// Provider mock server: 回 OpenAI 格式 chat completion 响应
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "x",
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "Mock answer"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{},
		})
	}))
	t.Cleanup(srv.Close)

	provCfg := config.ProviderConfig{
		ID:            "p1",
		Type:          "openai",
		APIKey:        "k",
		BaseURL:       srv.URL,
		Timeout:       5 * time.Second,
		MaxRetries:    0,
		RetryInterval: 0,
		Models: []config.ModelConfig{{
			ID:            "test-model",
			Name:          "Test Model",
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
		AgentExists:   func(id string) bool { return id == "agent-test" },
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
			ID:        "agent-test",
			Name:      "Test Agent",
			Provider:  "p1",
			Model:     "test-model",
			MaxTokens: 1000,
		}},
		Context: config.ContextConfig{
			MaxTokens: 0, ReservedTokens: 1500, Strategy: "truncate",
		},
		Session: sessCfg,
	}

	ctxMgr := ctxwindow.NewManager()

	agm, err := NewManager(Dependencies{
		Config:    cfg,
		Sessions:  sm,
		Context:   ctxMgr,
		Providers: pm,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agm.Shutdown(context.Background()) })

	return agm, sm
}

func TestAgentManagerGetList(t *testing.T) {
	agm, _ := newAgentTestEnv(t)
	info, err := agm.Get("agent-test")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "agent-test" || info.Status != StatusRunning {
		t.Fatalf("bad info: %+v", info)
	}
	// NotFound
	_, err = agm.Get("nope")
	if !isAgentErr(err, ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
	infos := agm.List(nil)
	if len(infos) != 1 || infos[0].ID != "agent-test" {
		t.Fatalf("expected 1 agent, got %+v", infos)
	}
}

func TestAgentLifecycle(t *testing.T) {
	agm, _ := newAgentTestEnv(t)
	ctx := context.Background()
	if err := agm.Pause(ctx, "agent-test"); err != nil {
		t.Fatal(err)
	}
	info, _ := agm.Get("agent-test")
	if info.Status != StatusPaused {
		t.Fatalf("expected paused, got %s", info.Status)
	}
	if err := agm.Pause(ctx, "agent-test"); err != nil {
		t.Fatalf("idempotent pause failed: %v", err)
	}
	if err := agm.Stop(ctx, "agent-test"); err != nil {
		t.Fatal(err)
	}
	info, _ = agm.Get("agent-test")
	if info.Status != StatusStopped {
		t.Fatalf("expected stopped, got %s", info.Status)
	}
	// Pause stopped = ErrAgentInvalidState
	if err := agm.Pause(ctx, "agent-test"); err == nil {
		t.Fatal("expected error pausing stopped agent")
	}
	// Start restores running
	if err := agm.Start(ctx, "agent-test"); err != nil {
		t.Fatal(err)
	}
	info, _ = agm.Get("agent-test")
	if info.Status != StatusRunning {
		t.Fatalf("expected running, got %s", info.Status)
	}
}

func TestAgentHandleTurnDirect(t *testing.T) {
	agm, sm := newAgentTestEnv(t)
	ctx := context.Background()
	s, err := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agm.HandleTurn(ctx, "agent-test", TurnRequest{
		SessionID: s.ID,
		TurnID:    "turn_t1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleTurn failed: %v (cause=%T)", err, context.Cause(ctx))
	}
	if result.Message.Payload.Role != "assistant" || result.Message.Payload.Content != "Mock answer" {
		t.Fatalf("bad result: %+v", result.Message)
	}
	got, _ := sm.Get(ctx, s.ID)
	if got.State != session.StateActive {
		t.Fatalf("expected active, got %s", got.State)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Payload.Role != "user" || got.Messages[0].Payload.Content != "hello" {
		t.Fatalf("bad user msg: %+v", got.Messages[0].Payload)
	}
	if got.Messages[1].Payload.Role != "assistant" {
		t.Fatalf("bad assistant msg")
	}
}

func TestAgentHandleTurnInvalid(t *testing.T) {
	agm, _ := newAgentTestEnv(t)
	ctx := context.Background()
	_, err := agm.HandleTurn(ctx, "agent-test", TurnRequest{SessionID: "", TurnID: "t", Content: "x"})
	if !isAgentErr(err, ErrAgentInvalidRequest) {
		t.Fatalf("expected ErrAgentInvalidRequest, got %v", err)
	}
	_, err = agm.HandleTurn(ctx, "nope", TurnRequest{SessionID: "x", TurnID: "t", Content: "x"})
	if !isAgentErr(err, ErrAgentNotFound) {
		t.Fatalf("expected ErrAgentNotFound, got %v", err)
	}
}

func isAgentErr(err, target error) bool {
	return errors.Is(err, target)
}
