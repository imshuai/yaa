package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/agent"
	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/storage"
)

// conversationTestEnv 构造完整环境的 API Server + Agent Manager + Session Manager + Provider。
func conversationTestEnv(t *testing.T) (*Server, *session.Manager) {
	t.Helper()

	// Provider mock
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只回 Chat completion
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "x",
			"model": "test-model",
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "Hi there"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{},
		})
	}))
	t.Cleanup(srv.Close)

	provCfg := config.ProviderConfig{
		ID:      "p1",
		Type:    "openai",
		APIKey:  "k",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
		Models: []config.ModelConfig{{
			ID:            "test-model",
			Name:          "Test",
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
	_ = sm.Restore(context.Background(), time.Now().UTC())
	_ = sm.Start(context.Background())
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
	}

	cm := ctxwindow.NewManager()
	am, err := agent.NewManager(agent.Dependencies{
		Config: cfg, Sessions: sm, Context: cm,
		Providers: pm,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = am.Shutdown(context.Background()) })

	apiSrv := NewServer("127.0.0.1:0", nil, nil)
	apiSrv.SetSessionProvider(sm, &fakeAgentProvider{
		agents: map[string]bool{"agent-test": true},
	})
	apiSrv.SetAgentProvider(am)
	return apiSrv, sm
}

func TestAPIPostMessageDirectTurn(t *testing.T) {
	srv, sm := conversationTestEnv(t)
	// 获取 Agent Context manager 怎么 实现？给 Manager.Context: 重建 - 见上 还需在 am构造 时提Comment Context。这里添加：
	// 用 injection trick - 用 reflections 无法 set private field.
	// 简单地：手动用 conversationTestEnv 中 am 提供访问 Context？ 最 简办法：让 am Context 替换 via Public field. 加入 新公开 方法：
	ctx := context.Background()
	s, err := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"turn_id": "turn_post_1",
		"content": "hello",
	}
	var buf []byte
	buf, _ = json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+s.ID+"/messages", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, rr.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != 0 {
		t.Fatalf("expected code 0, got %d msg=%s", env.Code, env.Message)
	}
	data, _ := env.Data.(map[string]any)
	if data["turn_id"] != "turn_post_1" {
		t.Fatalf("expected turn_id turn_post_1, got %v", data["turn_id"])
	}
	msg, _ := data["message"].(map[string]any)
	if msg["role"] != "assistant" || msg["content"] != "Hi there" {
		t.Fatalf("bad assistant message: %+v", msg)
	}
}

func TestAPIPostMessageInvalidTurnID(t *testing.T) {
	srv, sm := conversationTestEnv(t)
	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})

	body := map[string]any{"turn_id": "", "content": "hi"}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/sessions/"+s.ID+"/messages", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, rr.Body.String())
	}
	var env Envelope
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Code != 40001 {
		t.Fatalf("expected 40001, got %d", env.Code)
	}
}
