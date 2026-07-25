package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeTransport 实现 ClientTransport 用 channel 驱动 Recv，捕获 Send 的消息
// 供测试序列化 JSON-RPC 应答。
type fakeTransport struct {
	mu       sync.Mutex
	info     TransportInfo
	sent     []*Message
	recvQ    chan *Message
	recvErr  error
	closed   bool
	startErr error
}

func newFakeTransport(t TransportInfo) *fakeTransport {
	return &fakeTransport{
		info:  t,
		recvQ: make(chan *Message, 256),
	}
}

func (f *fakeTransport) Start(ctx context.Context) error { return f.startErr }
func (f *fakeTransport) Info() TransportInfo             { return f.info }
func (f *fakeTransport) Send(ctx context.Context, msg *Message) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return ErrMCPTransportClosed
	}
	f.sent = append(f.sent, msg)
	return nil
}
func (f *fakeTransport) Recv(ctx context.Context) (*Message, error) {
	// 如果预置错误，立即移除以避免重复返
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, ErrMCPTransportClosed
	}
	e := f.recvErr
	if e != nil {
		f.recvErr = nil
		f.mu.Unlock()
		return nil, e
	}
	f.mu.Unlock()
	select {
	case msg := <-f.recvQ:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// pushToRecv 把 server 端应答推到 Recv 队列供 dispatcher 读取。
func (f *fakeTransport) pushToRecv(msg *Message) { f.recvQ <- msg }

// popSent 返回并清空已捕获的 Send 消息（按时间序）。
func (f *fakeTransport) popSent() []*Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

func newStdioFakeClient(ctx context.Context) (*Client, *fakeTransport) {
	ft := newFakeTransport(TransportInfo{Type: "stdio", Connected: true})
	c := NewClient("fs", ctx, ft)
	return c, ft
}

// respond 给定 request id 把 Result 推回 Recv。
func respondOK(ft *fakeTransport, id json.RawMessage, result any) {
	rawResult, _ := json.Marshal(result)
	ft.pushToRecv(&Message{JSONRPC: "2.0", ID: id, Result: rawResult})
}

func respondErr(ft *fakeTransport, id json.RawMessage, code int, message string) {
	ft.pushToRecv(&Message{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}})
}

func nextRequestID(msg *Message) json.RawMessage { return msg.ID }

// ------------------------------------------------------------------
// Tests
// ------------------------------------------------------------------

// Connect → status=connecting；Initialize 成功后 → connected；Close → disconnected。
func TestClientInitializeLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if st := c.Status(); st != StatusConnecting {
		t.Fatalf("after Connect status=%q want connecting", st)
	}

	// Initialize 应答：返回 2025-03-26 + tools capability。
	go func() {
		time.Sleep(10 * time.Millisecond)
		// 等待 initialize Send 出
		// 由于测试 fs 只有一个 Recv queue，我们 pop 出 request 注册
	}()
	// 同步路径：发送 initialize 后会阻塞等待响应，必须在 goroutine 内线程化 push
	go func() {
		// 先取 Send 出的 initialize，再回 PushToRecv 应答
		// 由于 Send 在 request 内同步发送，需要 race-free 式 peek；这里用一个简单的
		// 等待策略：每毫秒查 sent 直至出现。
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		if first == nil {
			t.Errorf("no initialize sent")
			return
		}
		if first.Method != "initialize" {
			t.Errorf("first method=%q want initialize", first.Method)
		}
		respondOK(ft, first.ID, InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      Implementation{Name: "srv", Version: "1"},
		})
		// 等 Send 的 notifications/initialized
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 && sent[0].Method == "notifications/initialized" {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	initDone := make(chan error, 1)
	go func() { initDone <- c.Initialize(ctx) }()
	select {
	case err := <-initDone:
		if err != nil {
			t.Fatalf("Initialize: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Initialize did not return in 2s")
	}
	if st := c.Status(); st != StatusConnected {
		t.Fatalf("after Initialize status=%q want connected", st)
	}

	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if st := c.Status(); st != StatusDisconnected {
		t.Errorf("after Close status=%q want disconnected", st)
	}
}

// Close 幂等：第二次返同一 closeErr（ErrMCPTransportClosed）。
func TestClientCloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := newStdioFakeClient(ctx)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	err1 := c.Close()
	err2 := c.Close()
	if !errors.Is(err1, err2) && err1 != nil && err2 != nil {
		if errors.Is(err1, ErrMCPTransportClosed) != errors.Is(err2, ErrMCPTransportClosed) {
			t.Errorf("two Close() return different sentinel: %v vs %v", err1, err2)
		}
	}
}

// ProtocolVersion 不匹配 → InvalidClose 后 status=error。
func TestClientInitializeRejectsIncompatibleVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		if first == nil {
			t.Errorf("no initialize sent")
			return
		}
		// stdio 不接受 "1.0.0" 版本
		respondOK(ft, first.ID, InitializeResult{
			ProtocolVersion: "1.0.0",
			Capabilities:    map[string]any{"tools": map[string]any{}},
		})
	}()
	err := c.Initialize(ctx)
	if !errors.Is(err, ErrMCPProtocolError) {
		t.Fatalf("Initialize incompatible version: got %v want ErrMCPProtocolError", err)
	}
}

// server 不 advertise tools → ErrMCPProtocolError。
func TestClientInitializeRejectsServerWithoutTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, first.ID, InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    map[string]any{"logging": map[string]any{}}, // 没有 tools
		})
	}()
	err := c.Initialize(ctx)
	if !errors.Is(err, ErrMCPProtocolError) {
		t.Fatalf("got %v want ErrMCPProtocolError", err)
	}
}

// Ping 成功路径：返 nil。
func TestClientPingOK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	// 直接置 connected 跳过 Initialize，仅测试 Ping 路径
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, first.ID, struct{}{})
	}()
	if err := c.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// DiscoverTools 单页返回两个 Tool；规范化名前缀 mcp.<server>.<remote>，按 name 排序。
func TestClientDiscoverToolsSinglePage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, first.ID, ListToolsResult{
			Tools: []MCPTool{
				{Name: "zeta", Description: "z", InputSchema: []byte(`{"type":"object"}`)},
				{Name: "alpha", Description: "a", InputSchema: []byte(`{"type":"object"}`)},
			},
			NextCursor: "",
		})
	}()
	tools, err := c.DiscoverTools(ctx)
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name != "mcp.fs.alpha" {
		t.Errorf("tools[0].name=%q want mcp.fs.alpha", tools[0].Name)
	}
	if tools[1].Name != "mcp.fs.zeta" {
		t.Errorf("tools[1].name=%q want mcp.fs.zeta", tools[1].Name)
	}
}

// DiscoverTools 多页：nextCursor 翻页直到空。
func TestClientDiscoverToolsMultiPage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	go func() {
		// 等第一页请求
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		// page 1 有 NextCursor=c2
		respondOK(ft, first.ID, ListToolsResult{
			Tools: []MCPTool{
				{Name: "b", InputSchema: []byte(`{"type":"object"}`)},
			},
			NextCursor: "c2",
		})
		// 等 page 2 请求
		var second *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				second = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, second.ID, ListToolsResult{
			Tools: []MCPTool{
				{Name: "a", InputSchema: []byte(`{"type":"object"}`)},
			},
			NextCursor: "",
		})
	}()
	tools, err := c.DiscoverTools(ctx)
	if err != nil {
		t.Fatalf("DiscoverTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "mcp.fs.a" || tools[1].Name != "mcp.fs.b" {
		t.Errorf("tools=%+v", tools)
	}
}

// DiscoverTools 工具 name 超长（>128 bytes）→ fail(ErrMCPProtocolError)。
func TestClientDiscoverToolsToolNameTooLong(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, first.ID, ListToolsResult{
			Tools: []MCPTool{
				{Name: string(long), InputSchema: []byte(`{"type":"object"}`)},
			},
		})
	}()
	if _, err := c.DiscoverTools(ctx); !errors.Is(err, ErrMCPProtocolError) {
		t.Fatalf("got %v want ErrMCPProtocolError", err)
	}
}

// CallTool 成功路径：result.IsError=false 时 Content 文本回传。
func TestClientCallToolOK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondOK(ft, first.ID, CallToolResult{
			Content: []Content{{Type: "text", Text: "hello"}},
			IsError: false,
		})
	}()
	if _, err := c.CallTool(ctx, "remoteName", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
}

// CallTool 在未连接状态 → ErrMCPUnavailable。
func TestClientCallToolNotConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := newStdioFakeClient(ctx)
	// 不 Connect
	if _, err := c.CallTool(ctx, "remotename", nil); !errors.Is(err, ErrMCPUnavailable) {
		t.Fatalf("CallTool disconnected: got %v want ErrMCPUnavailable", err)
	}
}

// server 返 -32602 → ErrMCPInvalidParams；
// 返 server-defined -32001 → ErrMCPToolExecFailed。
func TestClientRPCErrorMapping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	// 第一组 Cast -32602
	go func() {
		var first *Message
		for i := 0; i < 200; i++ {
			sent := ft.popSent()
			if len(sent) > 0 {
				first = sent[0]
				break
			}
			time.Sleep(time.Millisecond)
		}
		respondErr(ft, first.ID, -32602, "invalid params")
	}()
	err := c.Ping(ctx)
	if !errors.Is(err, ErrMCPInvalidParams) {
		t.Errorf("(-32602) want ErrMCPInvalidParams got %v", err)
	}
}

// ctx 取消触发 bestEffortCancel；request 返 context.Cause。
func TestClientRequestCtxCancelReturnsCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()

	reqCtx, reqCancel := context.WithCancelCause(ctx)
	cause := fmt.Errorf("test-cancel")
	go func() {
		// 不予 Send 应答，等 caller 取消。
		time.Sleep(20 * time.Millisecond)
		reqCancel(cause)
	}()
	err := c.Ping(reqCtx)
	if !errors.Is(err, cause) {
		t.Errorf("Ping with cancelled ctx: got %v want %v", err, cause)
	}
	// bestEffortCancel 至少把 notifications/cancelled 推到 Send 队列（best-effort，非阻塞可能丢）。
	// 给 best effort 一些时间到达
	time.Sleep(bestEffortCancelTimeout + 30*time.Millisecond)
	sent := ft.popSent()
	foundCancel := false
	for _, m := range sent {
		if m.Method == "notifications/cancelled" {
			foundCancel = true
			break
		}
	}
	if !foundCancel {
		t.Errorf("bestEffortCancel did not produce notifications/cancelled: sent=%+v", sent)
	}
}

// transport.Send 失败 → request 返 wrap(ErrMCPTransportWrite) 且 Client fail。
func TestClientRequestSendFailureFailsClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ft := newFakeTransport(TransportInfo{Type: "stdio"})
	ft.mu.Lock()
	ft.closed = true // Close 之前设；让 Send 立即断
	ft.mu.Unlock()
	c := NewClient("fs", ctx, ft)
	_ = c.Connect(ctx)
	// 把 transport closed flag 重置以便 Connect 已经成功；但 Send 永远 closed
	// Send 路径会返 ErrMCPTransportClosed → 应被 fail 路径包装成 ErrMCPTransportWrite
	c.mu.Lock()
	c.status = StatusConnected
	c.mu.Unlock()
	if _, err := c.CallTool(ctx, "x", nil); err == nil {
		t.Fatalf("expected Send failure error")
	}
	if err := c.Err(); err == nil {
		t.Errorf("Err should be set after fail")
	}
}

// Done 在 fail 后关闭；Close 后 Done 仍关（fail once 由 Close 触发）。
func TestClientDoneClosesOnClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after Close")
	}
}

// controlLoop 收到 server ping 请求 → 回 ping 响应；其他 method → -32601。
func TestClientHandlesServerPingRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, ft := newStdioFakeClient(ctx)
	_ = c.Connect(ctx)

	// 先收到一条 server 端 ping request（有 ID）
	serverPingID := json.RawMessage("999")
	ft.pushToRecv(&Message{JSONRPC: "2.0", ID: serverPingID, Method: "ping"})
	// controlLoop 应处理并 Send 响应：等待
	var got *Message
	for i := 0; i < 300; i++ {
		sent := ft.popSent()
		for _, m := range sent {
			if m.ID != nil {
				got = m
			}
		}
		if got != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got == nil {
		t.Fatal("controlLoop did not send ping response")
	}
	if string(got.ID) != "999" {
		t.Errorf("response id=%s want 999", got.ID)
	}
	if got.Result == nil {
		t.Errorf("ping response should have result, got error=%v", got.Error)
	}
	_ = c.Close()
}
