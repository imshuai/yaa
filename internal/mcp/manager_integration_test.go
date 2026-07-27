package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/tool"
)

// buildToolManager 构造带 allow-all agent 的 Tool Manager，供 Manager 集成测试注册 MCP Proxy 用。
// a1 不声明 Tools 列表 → 全 Tool allow（包括 mcp.<server>.*）；
// 默认 timeout 设大避免短测误触发 timeout。
func buildToolManager(t *testing.T) *tool.Manager {
	t.Helper()
	// 用最小 provider manager。
	provCfg := config.ProviderConfig{ID: "p1", Type: "openai", APIKey: "k", BaseURL: "http://0",
		Models: []config.ModelConfig{{ID: "m"}}}
	pm, pmErr := provider.NewManager([]config.ProviderConfig{provCfg})
	if pmErr != nil {
		t.Fatalf("provider manager: %v", pmErr)
	}
	t.Cleanup(func() { _ = pm.Close() })
	cfg := &config.Config{
		Agents: []config.AgentConfig{{ID: "a1"}}, // 空 Tools → AllowAll
		Tools: config.ToolsConfig{
			DefaultTimeout: 2 * time.Second, MaxTimeout: 5 * time.Second, MaxConcurrent: 2,
			Builtin: map[string]config.ToolConfig{},
		},
	}
	m, err := tool.NewManager(tool.Dependencies{Config: cfg, Providers: pm})
	if err != nil {
		t.Fatalf("tool.NewManager: %v", err)
	}
	return m
}

// fakeStdioServerConfig 返回指向 stdio_test.go 中 fakeMCPStdioServer 的 MCPServerConfig。
// server 名 "fake"，transport stdio，command python3 -c <script>，auto_start=true。
func fakeStdioServerConfig(t *testing.T, name string, autoStart bool) config.MCPServerConfig {
	t.Helper()
	return config.MCPServerConfig{
		Name:      name,
		Transport: "stdio",
		Command:   requirePython3(t),
		Args:      []string{"-c", fakeMCPStdioServer},
		AutoStart: autoStart,
	}
}

// stdio fake MCP server 经 Manager.Prepare 启动 → connected → ToolCount=2 →
// Tools() 返回 alpha/beta 两个 ToolInfo（canonical 名前缀 mcp.fake）。
func TestManagerPrepareAutoStartStdioRegistersTools(t *testing.T) {
	py := requirePython3(t)
	_ = py
	tm := buildToolManager(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{fakeStdioServerConfig(t, "fake", true)},
	}
	m, err := NewManager(cfg, tm, nil)
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

	// 给后台 transport 一点缓冲（实际 Prepare 同步等到 register 完成，应立即可见）.
	st, ok := m.Get("fake")
	if !ok {
		t.Fatalf("Get(fake): not found")
	}
	if st.Status != StatusConnected {
		t.Fatalf("status=%q want %q (last_error=%q)", st.Status, StatusConnected, st.LastError)
	}
	if st.ToolCount != 2 {
		t.Errorf("tool_count=%d want 2", st.ToolCount)
	}
	if st.ConnectedAt == nil {
		t.Errorf("connected_at=nil want non-nil")
	}
	if st.LastError != "" {
		t.Errorf("last_error=%q want empty", st.LastError)
	}
	// ServerStatus.ProtocolVersion 应来自 Client 协商结果（fake MCP server 返 2025-03-26）.
	if st.ProtocolVersion == nil {
		t.Errorf("protocol_version=nil want non-nil")
	} else if *st.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol_version=%q want %q", *st.ProtocolVersion, ProtocolVersion)
	}
	tools, ok := m.Tools("fake")
	if !ok {
		t.Fatalf("Tools(fake): not found")
	}
	if len(tools) != 2 {
		t.Fatalf("Tools len=%d want 2", len(tools))
	}
	want := map[string]bool{"mcp.fake.alpha": false, "mcp.fake.beta": false}
	for _, ti := range tools {
		if _, isWant := want[ti.Name]; isWant {
			want[ti.Name] = true
		}
		if ti.Source != "mcp" {
			t.Errorf("tool %q source=%q want mcp", ti.Name, ti.Source)
		}
		if !ti.Enabled {
			t.Errorf("tool %q enabled=false want true", ti.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("missing tool %q in Tools result", name)
		}
	}
}

// Manager.Prepare 成功后，经 ToolManager.Execute 调用 mcp.fake.alpha 应返回
// "hello alpha"（来自 fake MCP server 的 tools/call 响应）。
func TestManagerToolProxyCallViaToolManager(t *testing.T) {
	requirePython3(t)
	tm := buildToolManager(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{fakeStdioServerConfig(t, "fake", true)},
	}
	m, _ := NewManager(cfg, tm, nil)
	defer func() {
		_ = m.Stop(context.Background())
		<-m.Done()
	}()
	_ = m.Prepare()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	scope := tool.ExecutionScope{AgentID: "a1"}
	result, err := tm.Execute(ctx, scope, "mcp.fake.alpha", map[string]any{})
	if err != nil {
		t.Fatalf("ToolManager.Execute mcp.fake.alpha: %v", err)
	}
	if result.Content != "hello alpha" {
		t.Errorf("content=%q want %q (iser=%v)", result.Content, "hello alpha", result.IsError)
	}
	if result.IsError {
		t.Errorf("is_error=true want false")
	}
}

// 构造一个 command 不存在的 stdio auto_start server → Prepare 不返错，
// Get(server) Status=error + LastError 非空 + ToolCount=0。
func TestManagerPrepareStdioAutoStartFailureMarksError(t *testing.T) {
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{
			{Name: "bogus", Transport: "stdio", Command: "/no/such/command", AutoStart: true},
		},
	}
	m, _ := NewManager(cfg, buildToolManager(t), nil)
	defer func() {
		_ = m.Stop(context.Background())
		<-m.Done()
	}()
	if err := m.Prepare(); err != nil {
		t.Fatalf("Prepare should not return error on per-server failure: %v", err)
	}
	st, ok := m.Get("bogus")
	if !ok {
		t.Fatalf("Get(bogus): not found")
	}
	if st.Status != StatusError {
		t.Errorf("status=%q want %q", st.Status, StatusError)
	}
	if st.LastError == "" {
		t.Errorf("last_error empty want non-empty")
	}
	if st.ToolCount != 0 {
		t.Errorf("tool_count=%d want 0", st.ToolCount)
	}
	tools, ok := m.Tools("bogus")
	if ok || tools != nil {
		t.Errorf("Tools(bogus): got (%v, %v) want (nil, false)", tools, ok)
	}
}

// auto_start=false 的 server 即使 auto_start=false 也不应被 Prepare 启动；
// Get(server) Status=disconnected、ToolCount=0；List 仍然列出该 server。
func TestManagerPrepareAutoStartFalseLeavesDisconnected(t *testing.T) {
	requirePython3(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{
			{Name: "lazy", Transport: "stdio", Command: requirePython3(t),
				Args: []string{"-c", fakeMCPStdioServer}, AutoStart: false},
		},
	}
	m, _ := NewManager(cfg, buildToolManager(t), nil)
	defer func() {
		_ = m.Stop(context.Background())
		<-m.Done()
	}()
	if err := m.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	st, ok := m.Get("lazy")
	if !ok {
		t.Fatalf("Get(lazy): not found")
	}
	if st.Status != StatusDisconnected {
		t.Errorf("status=%q want disconnected", st.Status)
	}
	if st.ToolCount != 0 {
		t.Errorf("tool_count=%d want 0", st.ToolCount)
	}
}

// Stop 后所有 server 回 disconnected；Tools 返回 (nil, false)。
func TestManagerStopDisconnectsClients(t *testing.T) {
	requirePython3(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{fakeStdioServerConfig(t, "fake", true)},
	}
	tm := buildToolManager(t)
	m, _ := NewManager(cfg, tm, nil)
	_ = m.Prepare()
	if st, _ := m.Get("fake"); st.Status != StatusConnected {
		t.Fatalf("prereq: status=%q want %q", st.Status, StatusConnected)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	<-m.Done()
	st, ok := m.Get("fake")
	if !ok {
		t.Fatalf("Get(fake): not found")
	}
	if st.Status != StatusDisconnected {
		t.Errorf("after Stop status=%q want disconnected", st.Status)
	}
	tools, ok := m.Tools("fake")
	if ok || tools != nil {
		t.Errorf("after Stop Tools(fake): got (%v, %v) want (nil, false)", tools, ok)
	}
}

// ErrMCPUnavailable 应在 cond Proxy handle nil 时由 Proxy.Execute 返回.
// 通过手动构造干等 handle 验证 toToolResult / Proxy 简单路径，不需要 subprocess.
func TestMCPToolProxyUnavailableWhenHandleNil(t *testing.T) {
	handle := &ProxyHandle{} // store nil (default)
	proxy := NewMCPToolProxy("srv", "remote", "desc", []byte(`{"type":"object"}`), 0, handle)
	if proxy.Name() != "mcp.srv.remote" {
		t.Errorf("Name=%q want mcp.srv.remote", proxy.Name())
	}
	if proxy.Description() != "desc" {
		t.Errorf("Description=%q want desc", proxy.Description())
	}
	_, err := proxy.Execute(context.Background(), tool.ExecutionScope{AgentID: "a1"}, map[string]any{})
	if !errors.Is(err, ErrMCPUnavailable) {
		t.Errorf("Execute err=%v want ErrMCPUnavailable", err)
	}
}

// toToolResult 把 CallToolResult 多 text block 按顺序 \n 连接；保留 isError.
func TestToToolResultJoinsText(t *testing.T) {
	r := &CallToolResult{
		Content: []Content{{Type: "text", Text: "a"}, {Type: "text", Text: "b"}},
		IsError: true,
	}
	out, err := toToolResult(r, nil)
	if err != nil {
		t.Fatalf("toToolResult: %v", err)
	}
	if out.Content != "a\nb" {
		t.Errorf("content=%q want a\\nb", out.Content)
	}
	if !out.IsError {
		t.Errorf("is_error=false want true")
	}
}

// toToolResult 非 text content 返 ErrMCPUnsupportedContent.
func TestToToolResultRejectsNonText(t *testing.T) {
	r := &CallToolResult{Content: []Content{{Type: "image", Text: "..."}}}
	_, err := toToolResult(r, nil)
	if !errors.Is(err, ErrMCPUnsupportedContent) {
		t.Errorf("err=%v want ErrMCPUnsupportedContent", err)
	}
}

// toToolResult nil result 返 ErrMCPProtocolError.
func TestToToolResultNilResult(t *testing.T) {
	_, err := toToolResult(nil, nil)
	if !errors.Is(err, ErrMCPProtocolError) {
		t.Errorf("err=%v want ErrMCPProtocolError", err)
	}
}

// toToolResult wire err 透传.
func TestToToolResultWrapsErr(t *testing.T) {
	in := ErrMCPUnavailable
	_, err := toToolResult(nil, in)
	if !errors.Is(err, in) {
		t.Errorf("err=%v want wrap %v", err, in)
	}
}

// fakeMCPExitServer 是 fakeMCPStdioServer 变体：tools/call 收到 name=="_stop_" 时
// sys.exit(2) 让 subprocess 主动退出模拟上游 transport 断开，验证 runUpstream client.Done() 路径.
const fakeMCPExitServer = `
import sys, json

def emit(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

SERVER_CAPS = {"tools": {}}
SVR_INFO = {"name": "fake-mcp-exit-server", "version": "0.0.1"}

tools = [
  {"name": "alpha", "description": "a", "inputSchema": {"type":"object"}},
  {"name": "stop", "description": "triggers subprocess exit", "inputSchema": {"type":"object"}},
]

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except Exception:
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
        if name == "stop":
            sys.stdout.flush()
            sys.exit(2)
        emit({"jsonrpc": "2.0", "id": mid, "result": {
            "content": [{"type":"text","text":"hello " + name}],
            "isError": False,
        }})
        continue
    emit({"jsonrpc": "2.0", "id": mid, "error": {"code": -32601, "message": "method not found"}})
`

// runUpstream 监听 client.Done()：上游 subprocess 异常退出（exit 2）触发 transport close,
// Manager markGenerationFailed 转为 StatusError + handle.Store(nil)；
// 后续 ToolManager.Execute(mcp.<server>.alpha) 应返 ErrMCPUnavailable.
// 无需等 30s ticker：client.Done() 路径在断开后即时触发.
func TestManagerRunUpstreamRecoversTransportClose(t *testing.T) {
	requirePython3(t)
	tm := buildToolManager(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{{
			Name:      "exit",
			Transport: "stdio",
			Command:   requirePython3(t),
			Args:      []string{"-c", fakeMCPExitServer},
			AutoStart: true,
		}},
		// 连接 / init timeout 给点冗余避免慢机器误判.
		Timeout: config.MCPTimeoutConfig{Connect: 5 * time.Second, Init: 5 * time.Second},
	}
	m, err := NewManager(cfg, tm, nil)
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
	st, ok := m.Get("exit")
	if !ok || st.Status != StatusConnected {
		t.Fatalf("prereq: status=%q last_error=%q", st.Status, st.LastError)
	}
	if st.ToolCount != 2 {
		t.Fatalf("prereq: tool_count=%d want 2", st.ToolCount)
	}

	// 触发上游 subprocess 主动退出: 触发 alpha -> stop (触发 sys.exit(2)) 让 python 退出.
	// 走 mcp.exit.alpha 一次确保 transport 仍正常, 然后调 mcp.exit.stop 触发 sys.exit.
	scope := tool.ExecutionScope{AgentID: "a1"}
	// 先 normal alpha call 验证 Proxy 走通.
	if _, err := tm.Execute(context.Background(), scope, "mcp.exit.alpha", map[string]any{}); err != nil {
		t.Fatalf("Startup normal mcp.exit.alpha Execute: %v", err)
	}
	// 触发 subprocess 退出.
	// 工具调用的 tools/call 在 server 端 emit 后 raise; client 端 Send 之后 recvLoop 在 stdout 被 close 后返 ErrMCPTransportClosed.
	_, _ = tm.Execute(context.Background(), scope, "mcp.exit.stop", map[string]any{})

	// 等 runUpstream 检测到 client.Done() 并 markGenerationFailed 置 Error.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := m.Get("exit"); s.Status == StatusError {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	final, _ := m.Get("exit")
	if final.Status != StatusError {
		t.Fatalf("status after subprocess exit=%q want error (last_error=%q)", final.Status, final.LastError)
	}
	if final.LastError == "" {
		t.Errorf("last_error empty; expected reason recorded by markGenerationFailed")
	}

	// 后续 Proxy 调用应返 ErrMCPUnavailable (handle.Store(nil)).
	_, err = tm.Execute(context.Background(), scope, "mcp.exit.alpha", map[string]any{})
	if !errors.Is(err, ErrMCPUnavailable) {
		t.Errorf("after transport closed, Execute mcp.exit.alpha err=%v want ErrMCPUnavailable", err)
	}
}

// Stop 在 runUpstream goroutine 运行中应能干净 Join 并 close Done 在合理时间内。
// 避免引入"Stop 等到 ticker 30s"或者"Stop 死锁"的退化.
func TestManagerStopJoinsUpstreamGoroutines(t *testing.T) {
	requirePython3(t)
	tm := buildToolManager(t)
	cfg := &config.MCPConfig{
		Servers: []config.MCPServerConfig{fakeStdioServerConfig(t, "fake", true)},
	}
	m, _ := NewManager(cfg, tm, nil)
	if err := m.Prepare(); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, ok := m.Get("fake"); !ok {
		t.Fatalf("prereq: server not found")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.Stop(context.Background()) }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop returned err=%v want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5s while runUpstream was active (ticker join deadlock)")
	}
	select {
	case <-m.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed within 2s after Stop returned")
	}
}
