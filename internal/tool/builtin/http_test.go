package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/tool"
)

func TestHTTPExecuteBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Hi", "1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("the body"))
	}))
	defer srv.Close()
	h, _ := NewHTTP(newShellCfg(nil))
	r, err := h.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"url":    srv.URL + "/x",
		"method": "GET",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r.IsError {
		t.Fatalf("unexpected IsError: %s", r.Content)
	}
	var out struct {
		StatusCode int               `json:"status_code"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
	}
	if jerr := json.Unmarshal([]byte(r.Content), &out); jerr != nil {
		t.Fatalf("parse result: %v (%s)", jerr, r.Content)
	}
	if out.StatusCode != 200 || out.Body != "the body" {
		t.Fatalf("out=%+v", out)
	}
	if out.Headers["X-Hi"] != "1" {
		t.Fatalf("headers=%+v", out.Headers)
	}
}

func TestHTTPExecuteBlockedHost(t *testing.T) {
	h, _ := NewHTTP(config.ToolConfig{Enabled: true, Options: map[string]any{
		"blocked_hosts": []any{"example.com"},
	}})
	r, _ := h.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"url": "https://example.com",
	})
	if !r.IsError || r.Content != "host blocked" {
		t.Fatalf("got=%+v", r)
	}
}

func TestHTTPExecuteNotAllowed(t *testing.T) {
	h, _ := NewHTTP(config.ToolConfig{Enabled: true, Options: map[string]any{
		"allowed_hosts": []any{"foo.com"},
	}})
	r, _ := h.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"url": "https://bar.com",
	})
	if !r.IsError || r.Content != "host not allowed" {
		t.Fatalf("got=%+v", r)
	}
}

func TestHTTPExecutePostBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		_, _ = w.Write([]byte("got:" + strings.TrimSpace(string(buf[:n]))))
	}))
	defer srv.Close()
	h, _ := NewHTTP(newShellCfg(nil))
	r, _ := h.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"body":   "payload",
	})
	var out struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal([]byte(r.Content), &out)
	if !strings.HasPrefix(out.Body, "got:payload") {
		t.Fatalf("body=%q", out.Body)
	}
}
