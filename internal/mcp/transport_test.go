package mcp

import (
	"encoding/json"
	"testing"
)

func TestValidateEnvelopeRequest(t *testing.T) {
	msg := &Message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "ping"}
	k, err := validateEnvelope(msg)
	if err != nil || k != kindRequest {
		t.Fatalf("got (%s, %v), want request", k, err)
	}
}

func TestValidateEnvelopeNotification(t *testing.T) {
	msg := &Message{JSONRPC: "2.0", Method: "notifications/initialized"}
	k, err := validateEnvelope(msg)
	if err != nil || k != kindNotification {
		t.Fatalf("got (%s, %v), want notification", k, err)
	}
	// notification 加 ID 后语义上变成 request（wire 言 ID+method 即 request）
	msg.ID = json.RawMessage("1")
	k2, err2 := validateEnvelope(msg)
	if err2 != nil || k2 != kindRequest {
		t.Fatalf("notification+id: got (%s, %v), want request", k2, err2)
	}
}

func TestValidateEnvelopeResponse(t *testing.T) {
	msg := &Message{JSONRPC: "2.0", ID: json.RawMessage("7"), Result: json.RawMessage(`{}`)}
	k, err := validateEnvelope(msg)
	if err != nil || k != kindResponse {
		t.Fatalf("got (%s, %v), want response", k, err)
	}
	msg.Method = "ping"
	if _, err := validateEnvelope(msg); err == nil {
		t.Fatalf("response with method: expected error")
	}
}

func TestValidateEnvelopeInvalidVersion(t *testing.T) {
	msg := &Message{JSONRPC: "1.0", ID: json.RawMessage("1"), Method: "ping"}
	if _, err := validateEnvelope(msg); err == nil {
		t.Fatalf("bad jsonrpc version accepted")
	}
}

func TestValidateEnvelopeResultErrorMutuallyExclusive(t *testing.T) {
	msg := &Message{JSONRPC: "2.0", ID: json.RawMessage("1"), Result: json.RawMessage(`{}`), Error: &RPCError{Code: -1}}
	if _, err := validateEnvelope(msg); err == nil {
		t.Fatalf("result+error accepted")
	}
}

func TestValidateEnvelopeEmpty(t *testing.T) {
	msg := &Message{JSONRPC: "2.0"}
	if _, err := validateEnvelope(msg); err == nil {
		t.Fatalf("empty accepted")
	}
}

func TestValidateEnvelopeNil(t *testing.T) {
	if _, err := validateEnvelope(nil); err == nil {
		t.Fatalf("nil accepted")
	}
}

func TestPreferredAndAccepts(t *testing.T) {
	if v := preferredVersion("stdio"); v != ProtocolVersion {
		t.Errorf("stdio preferred=%s want %s", v, ProtocolVersion)
	}
	if v := preferredVersion("streamable_http"); v != ProtocolVersion {
		t.Errorf("http preferred=%s want %s", v, ProtocolVersion)
	}
	if v := preferredVersion("sse"); v != LegacyProtocolVersion {
		t.Errorf("sse preferred=%s want %s", v, LegacyProtocolVersion)
	}
	if !acceptsVersion("stdio", ProtocolVersion) || !acceptsVersion("stdio", LegacyProtocolVersion) {
		t.Errorf("stdio should accept both versions")
	}
	if acceptsVersion("streamable_http", LegacyProtocolVersion) {
		t.Errorf("http should reject legacy")
	}
	if acceptsVersion("sse", ProtocolVersion) {
		t.Errorf("sse should reject 2025-03-26")
	}
}

func TestParseIDNumber(t *testing.T) {
	id, ok := parseID(json.RawMessage("42"))
	if !ok || id != 42 {
		t.Fatalf("parseID(42): (%d, %v)", id, ok)
	}
	// 0 视作无效：Client 不签发 0 ID
	if _, ok := parseID(json.RawMessage("0")); ok {
		t.Errorf("parseID(0) accepted; should not - Client IDs are positive")
	}
	// string ID 视作无效：MVP Client 不签发 / 不接受 string response ID
	if _, ok := parseID(json.RawMessage(`"42"`)); ok {
		t.Errorf("parseID(\"42\") accepted; MVP only numbers")
	}
	// 空视作无效
	if _, ok := parseID(json.RawMessage("")); ok {
		t.Errorf("parseID(empty) accepted")
	}
}
