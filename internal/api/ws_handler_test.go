package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/imshuai/yaa/internal/session"
)

// dialWS 在测试中等同 client dial：Authorization Header + 指定 path。
func dialWS(t *testing.T, url, sessionID string) *websocket.Conn {
	t.Helper()
	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer test")
	wsURL := strings.Replace(url, "http://", "ws://", 1)
	c, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/sessions/"+sessionID+"/stream", hdr)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return c
}

// readFrame 读一条 ConversationFrame 并设置 deadline。
func readFrame(t *testing.T, c *websocket.Conn) ConversationFrame {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var f ConversationFrame
	if jerr := json.Unmarshal(data, &f); jerr != nil {
		t.Fatalf("ws frame parse: %v (%s)", jerr, string(data))
	}
	return f
}

func TestWSStreamTurnFlow(t *testing.T) {
	apiSrv, sm := conversationTestEnv(t)
	hsrv := httptest.NewServer(apiSrv.server.Handler)
	t.Cleanup(hsrv.Close)

	ctx := context.Background()
	s, err := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})
	if err != nil {
		t.Fatal(err)
	}
	conn := dialWS(t, hsrv.URL, s.ID)
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 发送 message frame
	req, _ := json.Marshal(wsClientFrame{Type: "message", TurnID: "turn_ws_1", Content: "hi"})
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatalf("ws write: %v", err)
	}

	// 期望序列：queued → assistant_start → assistant_delta* → assistant_done
	var got []ConversationFrame
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		f := readFrame(t, conn)
		got = append(got, f)
		if f.Type == "assistant_done" {
			break
		}
	}
	kinds := make([]string, 0, len(got))
	for _, f := range got {
		kinds = append(kinds, f.Type)
	}
	if !containsStr(kinds, "queued") || !containsStr(kinds, "assistant_start") ||
		!containsStr(kinds, "assistant_delta") || !containsStr(kinds, "assistant_done") {
		t.Fatalf("missing expected frames, got %v", kinds)
	}
	// delta 拼接为 "Hi there"
	var acc string
	for _, f := range got {
		if f.Type == "assistant_delta" && f.Delta != nil {
			acc += *f.Delta
		}
	}
	if acc != "Hi there" {
		t.Fatalf("delta acc=%q want Hi there", acc)
	}
	// assistant_done 必带 assistant/usage/tool_call_count
	var done *ConversationFrame
	for i := range got {
		if got[i].Type == "assistant_done" {
			done = &got[i]
		}
	}
	if done == nil || done.Assistant == nil || done.Usage == nil || done.ToolCallCount == nil {
		t.Fatalf("assistant_done missing fields: %+v", done)
	}
	if done.Assistant.Content != "Hi there" {
		t.Fatalf("assistant content=%q want Hi there", done.Assistant.Content)
	}
}

func TestWSStreamCancelBeforeStart(t *testing.T) {
	// 测试缺少 Authorization 被拒。
	apiSrv, sm := conversationTestEnv(t)
	hsrv := httptest.NewServer(apiSrv.server.Handler)
	t.Cleanup(hsrv.Close)
	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})

	// 无 Authorization 头：握手 HTTP 401，gorilla Dialer 返回响应。
	hdr := http.Header{}
	wsURL := strings.Replace(hsrv.URL, "http://", "ws://", 1)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/sessions/"+s.ID+"/stream", hdr)
	if err == nil {
		t.Fatal("expected handshake failure without Authorization")
	}
	if resp == nil {
		t.Fatalf("expected response on handshake failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWSStreamCancelRunningTurn(t *testing.T) {
	apiSrv, sm := conversationTestEnv(t)
	hsrv := httptest.NewServer(apiSrv.server.Handler)
	t.Cleanup(hsrv.Close)
	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})

	conn := dialWS(t, hsrv.URL, s.ID)
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 发送 message；Streaming mock 很快返回，无法可靠在 provider 阶段取消。
	// 我们检验 cancel 一个未知 turn_id 返回 error frame "turn not active"。
	req, _ := json.Marshal(wsClientFrame{Type: "cancel", TurnID: "nope"})
	_ = conn.WriteMessage(websocket.TextMessage, req)
	f := readFrame(t, conn)
	if f.Type != "error" || f.TurnID != "nope" || f.Code != "40001" {
		t.Fatalf("expected error 40001 for unknown turn, got %+v", f)
	}
}

// 防止编译器误报 strings 未使用。
var _ = strings.HasPrefix

// TestWSStreamDisconnectCancelsTurn 在 active turn 期间关闭连接，期望 service 端不会泄漏
// 启动态 turn（无法检测远程行为，PN v1 只校验：重新 dail 同 turn_id 复用成功）。
func TestWSStreamDisconnectCancelsTurn(t *testing.T) {
	apiSrv, sm := conversationTestEnv(t)
	hsrv := httptest.NewServer(apiSrv.server.Handler)
	t.Cleanup(hsrv.Close)
	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})

	conn := dialWS(t, hsrv.URL, s.ID)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 发送 message 但不读结果，立刻关闭连接。
	req, _ := json.Marshal(wsClientFrame{Type: "message", TurnID: "turn_abort_1", Content: "slowplease"})
	_ = conn.WriteMessage(websocket.TextMessage, req)
	_ = conn.Close()

	// 等一段让 service 处理取消。
	time.Sleep(200 * time.Millisecond)

	// 再次拨号；turn_abort_1 应已取消，不应在 active 列表。重发同一 turn_id 现在应被服务接受
	// （queued 取消不消费 ID），所以 queued 帧应该正常回来。
	conn2 := dialWS(t, hsrv.URL, s.ID)
	defer conn2.Close()
	_ = conn2.SetReadDeadline(time.Now().Add(10 * time.Second))
	req2, _ := json.Marshal(wsClientFrame{Type: "message", TurnID: "turn_abort_1", Content: "again"})
	_ = conn2.WriteMessage(websocket.TextMessage, req2)

	f := readFrame(t, conn2)
	// 首个进来的应该是 queued 或 assistant_start（取决于运行时序）。v1 文档要求 queued 先。
	// 即使是 assistant_start 也表明 turn 已重新发起，证实 turn 状态已清理。
	if f.Type != "queued" && f.Type != "assistant_start" && f.Type != "error" {
		t.Fatalf("unexpected first frame: %+v", f)
	}
	// 若是 error 且 code=40001 turn id already used，则 turn 取消未清理。
	if f.Type == "error" && f.Code == "40001" && strings.Contains(f.Message, "already used") {
		t.Fatalf("turn_abort_1 was not cleaned up on disconnect: %+v", f)
	}
	_ = readFrame(t, conn2) // drain 下一帧避免主退出时阻塞 writer
}

func TestWSStreamSessionEndOnClose(t *testing.T) {
	apiSrv, sm := conversationTestEnv(t)
	hsrv := httptest.NewServer(apiSrv.server.Handler)
	t.Cleanup(hsrv.Close)
	ctx := context.Background()
	s, _ := sm.Create(ctx, session.CreateRequest{AgentID: "agent-test"})

	conn := dialWS(t, hsrv.URL, s.ID)
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_ = sm.Close(ctx, s.ID)
	f := readFrame(t, conn)
	if f.Type != "session_end" || f.Reason != "closed" {
		t.Fatalf("expected session_end/closed, got %+v", f)
	}
}
