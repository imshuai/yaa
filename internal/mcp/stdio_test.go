package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// fakeMCPStdioServer 是用作子进程的 Python 简易 MCP server，按行读 stdin 写 stdout。
// 行为：首轮 initialize → 返 2025-03-26 + tools；然后 tools/list → 返 2 个 Tools；
// tools/call N hello world → 返 content text; ping → empty result；其他 method → -32601。
const fakeMCPStdioServer = `
import sys, json

def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

SERVER_CAPS = {"tools": {}}
SVR_INFO = {"name": "fake-mcp-server", "version": "0.0.1"}

tools = [
  {"name": "alpha", "description": "a", "inputSchema": {"type":"object"}},
  {"name": "beta", "description": "b", "inputSchema": {"type":"object"}},
]

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except Exception as e:
        emit({"jsonrpc": "2.0", "id": None, "error": {"code": -32700, "message": "parse error"}})
        continue
    mid = msg.get("id")
    method = msg.get("method")
    params = msg.get("params", {})
    if method == "initialize":
        emit({"jsonrpc": "2.0", "id": mid, "result": {
            "protocolVersion": "2025-03-26",
            "capabilities": SERVER_CAPS,
            "serverInfo": SVR_INFO,
        }})
        continue
    if method == "notifications/initialized":
        continue
    if method == "ping":
        emit({"jsonrpc": "2.0", "id": mid, "result": {}})
        continue
    if method == "tools/list":
        emit({"jsonrpc": "2.0", "id": mid, "result": {"tools": tools}})
        continue
    if method == "tools/call":
        name = params.get("name", "")
        emit({"jsonrpc": "2.0", "id": mid, "result": {
            "content": [{"type":"text","text":"hello " + name}],
            "isError": False,
        }})
        continue
    emit({"jsonrpc": "2.0", "id": mid, "error": {"code": -32601, "message": "method not found: " + str(method)}})
`

func requirePython3(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	t.Skip("python3 not available; stdio integration test requires python3 as fake MCP server")
	return ""
}

// stdioClientEndToEnd 启动 fake MCP server + Client → Connect → Initialize → 启用路径。
func stdioClientEndToEnd(t *testing.T) (*Client, *StdioClient) {
	t.Helper()
	py := requirePython3(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ft := NewStdioClient(py, []string{"-c", fakeMCPStdioServer}, nil, nil)
	c := NewClient("fake", ctx, ft)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close()
	})
	return c, ft
}

// 完整 lifecycle：Connect → Initialize → Ping OK → DiscoverTools → Close
func TestStdioClientEndToEndLifecycle(t *testing.T) {
	c, _ := stdioClientEndToEnd(t)
	if st := c.Status(); st != StatusConnected {
		t.Fatalf("after Initialize status=%q want connected", st)
	}
	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
	tools, err := c.DiscoverTools(ctx)
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name != "mcp.fake.alpha" || tools[1].Name != "mcp.fake.beta" {
		t.Errorf("tools=%+v", tools)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if st := c.Status(); st != StatusDisconnected {
		t.Errorf("after Close status=%q want disconnected", st)
	}
}

// CallTool 端到端：name="alpha" → content hello alpha。
func TestStdioClientCallToolEndToEnd(t *testing.T) {
	c, _ := stdioClientEndToEnd(t)
	result, err := c.CallTool(context.Background(), "remoteFoo", map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected IsError=true")
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" ||
		result.Content[0].Text != "hello remoteFoo" {
		t.Errorf("content=%+v", result.Content)
	}
}

// Send 行长度超过 4 MiB 上限 → ErrMCPProtocolError。
func TestStdioClientSendBodyTooLarge(t *testing.T) {
	py := requirePython3(t)
	c := NewStdioClient(py, []string{"-c", fakeMCPStdioServer}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()
	huge := make([]byte, stdioMessageMaxBytes+10)
	for i := range huge {
		huge[i] = ' '
	}
	msg := &Message{
		JSONRPC: "2.0", ID: json.RawMessage("1"),
		Method: "ping", Params: json.RawMessage(huge),
	}
	err := c.Send(ctx, msg)
	if !errors.Is(err, ErrMCPProtocolError) {
		t.Errorf("Send oversized: got %v want ErrMCPProtocolError", err)
	}
}

// Start 失败（command 不存在） → ErrMCPConnRefused。
func TestStdioClientStartCommandNotFound(t *testing.T) {
	if _, err := exec.LookPath("definitely-not-existing-cmd-xyz"); err == nil {
		t.Skip("unexpected: nonexistent command found")
	}
	c := NewStdioClient("definitely-not-existing-cmd-xyz", nil, nil, nil)
	err := c.Start(context.Background())
	if !errors.Is(err, ErrMCPConnRefused) {
		t.Errorf("Start nonexistent: got %v want ErrMCPConnRefused", err)
	}
}

// Close 幂等；多次调用不 panic。
func TestStdioClientCloseIdempotent(t *testing.T) {
	c, _ := stdioClientEndToEnd(t)
	if err := c.Close(); err != nil {
		t.Errorf("Close #1: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close #2: %v", err)
	}
}

// Info 反映 Start 前 / Start 后 / Close 后的连接状态。
func TestStdioClientInfoConnected(t *testing.T) {
	py := requirePython3(t)
	c := NewStdioClient(py, []string{"-c", fakeMCPStdioServer}, nil, nil)
	if info := c.Info(); info.Connected {
		t.Errorf("before Start Connected=true want false")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if info := c.Info(); !info.Connected || info.Type != "stdio" {
		t.Errorf("after Start info=%+v", info)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if info := c.Info(); info.Connected {
		t.Errorf("after Close Connected=true want false")
	}
}

// 子进程先退出（kill）→ Recv 返 ErrMCPTransportClosed。
func TestStdioClientRecvOnSubprocessExit(t *testing.T) {
	py := requirePython3(t)
	c := NewStdioClient(py, []string{"-c", fakeMCPStdioServer}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Kill 子进程并触发 stdout 关闭
	c.mu.Lock()
	proc := c.cmd.Process
	c.mu.Unlock()
	_ = proc.Kill()

	deadline := time.After(3 * time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.Recv(ctx)
	}()
	select {
	case <-done:
		// Recv 返错误即符合预期
	case <-deadline:
		t.Fatal("Recv did not return within 3s after subprocess kill")
	}
	_ = c.Close()
}

// env 注入策略：用户 env 应让子进程能看到。fake server 收到 initialize 后把
// FAKE_MCP_TEST_ENV 拼进 serverInfo.name；Client 通过 c.request 取回 InitializeResult 校验。
func TestStdioClientEnvInjection(t *testing.T) {
	py := requirePython3(t)
	const envServer = `
import sys, json, os
def emit(o):
  sys.stdout.write(json.dumps(o) + "\n"); sys.stdout.flush()
for line in sys.stdin:
  line = line.strip()
  if not line: continue
  try: msg = json.loads(line)
  except Exception: continue
  if msg.get("method") == "initialize":
    val = os.environ.get("FAKE_MCP_TEST_ENV", "MISSING")
    emit({"jsonrpc":"2.0","id":msg.get("id"),"result":{
      "protocolVersion":"2025-03-26","capabilities":{"tools":{}},
      "serverInfo":{"name":"env:" + val, "version":"1"},
    }})
    break
`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ft := NewStdioClient(py, []string{"-c", envServer}, map[string]string{"FAKE_MCP_TEST_ENV": "injected"}, nil)
	c := NewClient("fake", ctx, ft)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	var result InitializeResult
	params := InitializeParams{
		ProtocolVersion: preferredVersion(ft.Info().Type),
		Capabilities:    map[string]any{},
		ClientInfo:      Implementation{Name: "yaa", Version: "test"},
	}
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		t.Fatalf("request initialize: %v", err)
	}
	if result.ServerInfo.Name != "env:injected" {
		t.Errorf("serverInfo.name=%q want env:injected", result.ServerInfo.Name)
	}
}
