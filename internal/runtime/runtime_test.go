package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/config"
)

func newTestConfig() *config.Config {
	cfg := config.Default()
	cfg.Runtime.API.HTTP.Addr = "127.0.0.1:0"
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
	if !h.Ready || h.Status != "healthy" {
		t.Fatalf("health: %+v", h)
	}
	if h.Components["api"] != "ready" {
		t.Fatalf("api component missing: %#v", h.Components)
	}
	if rt.UptimeSeconds() < 0 {
		t.Fatalf("uptime negative: %d", rt.UptimeSeconds())
	}
}

func TestRuntimeShutdownClearsReady(t *testing.T) {
	rt, _ := New(newTestConfig(), nil)
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
	rt, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
