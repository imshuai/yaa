package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/imshuai/yaa/internal/config"

	"golang.org/x/exp/slog"
)


// TestSafeEndpoint 验证日志脱敏 (docs/mcp/observability.md §1 末段).
func TestSafeEndpoint(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"http://h:8080/mcp", "http://h:8080/mcp"},
		{"https://user:secret@h/mcp?token=x#frag", "https://h/mcp"},
		{"http://h/p?q=1&token=abc#/x", "http://h/p"},
		{"not-a-url", "not-a-url"}, // 非绝对 URL 原样返回
		{"/relative", "/relative"}, // 非 http(s) 原样返回
	}
	for _, c := range cases {
		if got := safeEndpoint(c.in); got != c.want {
			t.Errorf("safeEndpoint(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// TestEndpointFor 按 transport 选择脱敏 endpoint.
func TestEndpointFor(t *testing.T) {
	stdio := &serverEntry{transport: "stdio", cfg: config.MCPServerConfig{Command: "/usr/bin/python3"}}
	if got := endpointFor(stdio); got != "/usr/bin/python3" {
		t.Errorf("stdio endpoint=%q want /usr/bin/python3", got)
	}
	net := &serverEntry{transport: "sse", cfg: config.MCPServerConfig{URL: "https://user:x@h:9000/mcp?a=1#f"}}
	if got := endpointFor(net); got != "https://h:9000/mcp" {
		t.Errorf("sse endpoint=%q want https://h:9000/mcp", got)
	}
	if got := endpointFor(nil); got != "" {
		t.Errorf("nil endpoint=%q want empty", got)
	}
}


// capturingHandler 把所有 slog.Record 的 msg 收集到切片, 线程安全.
type capturingHandler struct {
	mu     sync.Mutex
	msgs   []string
	attrs  []map[string]string
	level_ slog.Level
}

func (h *capturingHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level_
}
func (h *capturingHandler) Handle(r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, r.Message)
	am := map[string]string{}
	r.Attrs(func(a slog.Attr) {
		am[a.Key] = a.Value.String()
	})
	h.attrs = append(h.attrs, am)
	return nil
}
func (h *capturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler      { return h }

// TestManagerEmitsConnectingAndConnectedEvents 验证 docs/mcp/observability.md §1
// 在 stdio auto_start server 启动时 emit mcp.server.connecting 和 mcp.server.connected 事件.
func TestManagerEmitsConnectingAndConnectedEvents(t *testing.T) {
	th := &capturingHandler{level_: slog.LevelInfo}
	logger := slog.New(th)
	tm := buildToolManager(t)
	cfg := &config.MCPConfig{Servers: []config.MCPServerConfig{fakeStdioServerConfig(t, "fake2", true)}}
	m, err := NewManager(cfg, tm, logger)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer func() {
		_ = m.Stop(context.Background())
		<-m.Done()
	}()
	if err := m.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	st, ok := m.Get("fake2")
	if !ok || st.Status != StatusConnected {
		t.Fatalf("want fake2 Connected, got %v ok=%v", st, ok)
	}
	th.mu.Lock()
	msgs := append([]string(nil), th.msgs...)
	attrs := append([]map[string]string(nil), th.attrs...)
	th.mu.Unlock()
	wantConnecting := false
	wantConnected := false
	for i, msg := range msgs {
		if msg == "mcp.server.connecting" && attrs[i]["server"] == "fake2" {
			wantConnecting = true
			if attrs[i]["transport"] != "stdio" {
				t.Errorf("connecting transport=%q want stdio", attrs[i]["transport"])
			}
			if attrs[i]["endpoint"] != requirePython3(t) {
				t.Errorf("connecting endpoint=%q want %q", attrs[i]["endpoint"], requirePython3(t))
			}
		}
		if msg == "mcp.server.connected" && attrs[i]["server"] == "fake2" {
			wantConnected = true
			if attrs[i]["tool_count"] != "2" {
				t.Errorf("connected tool_count=%q want 2", attrs[i]["tool_count"])
			}
		}
	}
	if !wantConnecting {
		t.Errorf("missing mcp.server.connecting event; msgs=%v", msgs)
	}
	if !wantConnected {
		t.Errorf("missing mcp.server.connected event; msgs=%v", msgs)
	}
}

// TestManagerEmitsErrorEventOnBadStdioCommand 验证 docs/mcp/observability.md §1
// 在 connectAndDiscover transport_build 失败时 emit mcp.server.error (level=error).
func TestManagerEmitsErrorEventOnBadStdioCommand(t *testing.T) {
	th := &capturingHandler{level_: slog.LevelInfo}
	logger := slog.New(th)
	cfg := &config.MCPConfig{Servers: []config.MCPServerConfig{{
		Name:      "broken",
		Transport: "stdio",
		Command:   "/nonexistent/binary",
		AutoStart:  true,
	}}}
	m, err := NewManager(cfg, buildToolManager(t), logger)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer func() {
		_ = m.Stop(context.Background())
		<-m.Done()
	}()
	if err := m.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	st, ok := m.Get("broken")
	if !ok {
		t.Fatalf("Get(broken): not found")
	}
	if st.Status != StatusError {
		t.Fatalf("want broken Error, got %v", st.Status)
	}
	th.mu.Lock()
	msgs := append([]string(nil), th.msgs...)
	attrs := append([]map[string]string(nil), th.attrs...)
	th.mu.Unlock()
	wantErr := false
	for i, msg := range msgs {
		if msg == "mcp.server.error" && attrs[i]["server"] == "broken" {
			wantErr = true
			if attrs[i]["error_type"] == "" {
				t.Errorf("error event error_type empty")
			}
		}
	}
	if !wantErr {
		t.Errorf("missing mcp.server.error event; msgs=%v", msgs)
	}
}
