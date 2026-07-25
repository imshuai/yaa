package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/config"
)

func newTestConfig() *config.Config {
	cfg := config.Default()
	cfg.Runtime.API.HTTP.Addr = "127.0.0.1:0"
	cfg.Runtime.Storage.Type = "memory" // 测试用内存后端，避免落地真实 SQLite 文件
	return cfg
}

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil, nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestRuntimeStartMarksReadyAndHealth(t *testing.T) {
	rt, err := New(newTestConfig(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt.cfg.Skills.Dir = t.TempDir()
	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutdownCtx)
	})
	if !rt.Ready() {
		t.Fatal("not ready after start")
	}
	h := rt.Health()
	if !h.Ready || h.Status != "degraded" {
		t.Fatalf("health: %+v (memory backend => degraded)", h)
	}
	if h.Components["api"] != "ready" {
		t.Fatalf("api component missing: %#v", h.Components)
	}
	if h.Components["storage"] != "degraded" {
		t.Fatalf("storage component should be degraded for memory backend: %#v", h.Components)
	}
	if h.Components["provider"] != "ready" {
		t.Fatalf("provider component should be ready: %#v", h.Components)
	}
	if rt.UptimeSeconds() < 0 {
		t.Fatalf("uptime negative: %d", rt.UptimeSeconds())
	}
}

func TestRuntimeShutdownClearsReady(t *testing.T) {
	rt, _ := New(newTestConfig(), nil)
	rt.cfg.Skills.Dir = t.TempDir()
	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if rt.Ready() {
		t.Fatal("still ready after shutdown")
	}
	h := rt.Health()
	if h.Ready || h.Status != "not_ready" {
		t.Fatalf("health after shutdown: %+v", h)
	}
	if _, ok := h.Components["api"]; ok {
		t.Fatalf("api component still present after shutdown: %#v", h.Components)
	}
}

func TestRuntimeHealthNotReadyBeforeStart(t *testing.T) {
	rt, _ := New(newTestConfig(), nil)
	h := rt.Health()
	if h.Ready {
		t.Fatal("should not be ready before start")
	}
}

func TestRuntimeE2EHealthHTTP(t *testing.T) {
	cfg := config.Default()
	cfg.Runtime.API.HTTP.Addr = "127.0.0.1:0"
	cfg.Runtime.Storage.Type = "sqlite"
	cfg.Runtime.Storage.Path = filepath.Join(t.TempDir(), "yaa.db")
	rt, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt.cfg.Skills.Dir = t.TempDir()
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.Shutdown(ctx)
	})

	addr := rt.APIAddr()
	resp, err := http.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("http get health: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d body=%s", resp.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"ready":true`)) {
		t.Fatalf("health body missing ready=true: %s", body)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID header")
	}
}

// TestRuntimeMemorySQLiteBackendStart verifies Memory backend selection by cfg
// produces "ready" memory component and rt.memory is non-nil.
func TestRuntimeMemorySQLiteBackendStart(t *testing.T) {
	cfg := newTestConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Storage.Type = "sqlite"
	cfg.Memory.Storage.Path = filepath.Join(t.TempDir(), "yaa-mem.db")
	// 关向量（v1 Embedder 未注入），关闭持久 storage 为 memory（root）保证 health 不被 storage 拖累。
	cfg.Memory.Vector.Enabled = false

	rt, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt.cfg.Skills.Dir = t.TempDir()
	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutdownCtx)
	})

	h := rt.Health()
	if comp := h.Components["memory"]; comp != "ready" {
		t.Fatalf("memory component = %q, want ready; full health: %#v", comp, h.Components)
	}
	if rt.memory == nil {
		t.Fatal("rt.memory should be non-nil for sqlite backend")
	}
	// 文件应被创建（sqlite 启动创建）。
	if _, statErr := os.Stat(cfg.Memory.Storage.Path); statErr != nil {
		t.Fatalf("sqlite file not created at %s: %v", cfg.Memory.Storage.Path, statErr)
	}
}

// TestRuntimeMemorySQLiteBackendStartFailsOnBadPath 证实 SQLite migration 失败阻断 Start
// （docs/memory/storage.md §2：无法创建则启动失败）。
func TestRuntimeMemorySQLiteBackendStartFailsOnBadPath(t *testing.T) {
	cfg := newTestConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Storage.Type = "sqlite"
	// 不可能创建的目录路径（已存在为文件而非目录）。
	badDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(badDir, []byte("file not dir"), 0o644); err != nil {
		t.Fatalf("seed not-a-dir file: %v", err)
	}
	cfg.Memory.Storage.Path = filepath.Join(badDir, "yaa-mem.db")

	rt, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt.cfg.Skills.Dir = t.TempDir()
	if err := rt.Start(context.Background()); err == nil {
		t.Fatal("Start should fail when sqlite backend cannot create dir")
		_ = rt.Shutdown(context.Background())
	}
}

// TestRuntimeMemoryVectorStartupReindex 启动期 vector enabled + mock embedder server，
// 验证 Runtime Reindex 在每个 vector-enabled Agent 上跑通且 memory component 保持 ready。
func TestRuntimeMemoryVectorStartupReindex(t *testing.T) {
	// mock OpenAI-compatible embeddings server: 对所有 inputs 都返固定 dim=2 向量 [0.1, 0.2]。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		out := struct {
			Data []map[string]any `json:"data"`
		}{}
		for range req.Input {
			out.Data = append(out.Data, map[string]any{"embedding": []float64{0.1, 0.2}})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	cfg := newTestConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Storage.Type = "memory"
	cfg.Memory.Vector.Enabled = true
	cfg.Memory.Vector.SimilarityThreshold = 0.5
	cfg.Memory.Vector.TopK = 5
	cfg.Memory.Embedding.Provider = "openai-compatible"
	cfg.Memory.Embedding.Model = "any"
	cfg.Memory.Embedding.BaseURL = srv.URL
	cfg.Memory.Embedding.Dimension = 2
	cfg.Memory.Embedding.Timeout = 2 * time.Second

	rt, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt.cfg.Skills.Dir = t.TempDir()
	ctx := context.Background()
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.Shutdown(shutdownCtx)
	})

	if comp := rt.Health().Components["memory"]; comp != "ready" {
		t.Fatalf("memory component = %q, want ready", comp)
	}
	if rt.memory == nil {
		t.Fatal("rt.memory is nil")
	}
}
