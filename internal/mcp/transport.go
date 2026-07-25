package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// ClientTransport 是 MCP Client 端 transport 抽象。
// 实现 stdio / sse / streamable_http；v1 Client 通过该接口完成所有 I/O。
// （docs/mcp/transport.md §3）
//
// 线程模型：每个 ClientTransport 实例只允许一个 goroutine 在 Recv 上阻塞
// （Client 的 dispatcher goroutine）；Send 可以并发。Transport 必须把所有
// response / notification 原样送入 Recv，不自行匹配 request ID。
// 已经发送的请求不得由 transport 重试。
type ClientTransport interface {
	// Start 建立底层连接；ctx 用于约束拨号超时。
	Start(ctx context.Context) error
	// Send 发送一条 JSON-RPC 消息；可被并发调用。
	Send(ctx context.Context, msg *Message) error
	// Recv 阻塞读取下一条消息；每个实例仅一个 dispatcher goroutine 调用。
	Recv(ctx context.Context) (*Message, error)
	// Close 关闭底层连接；幂等。
	Close() error
	// Info 返回 transport 元数据（类型、端点、是否连接）。
	Info() TransportInfo
}

// messageKind 是 validateEnvelope 分类结果。
type messageKind int

const (
	kindInvalid      messageKind = iota
	kindRequest                  // 有 ID + Method，无 Result/Error
	kindNotification             // 有 Method，无 ID/Result/Error
	kindResponse                 // 有 ID + Result 或 Error，无 Method/Params
)

// String 仅诊断使用，未入正式日志（避免 sibling surface 不一致）。
func (k messageKind) String() string {
	switch k {
	case kindRequest:
		return "request"
	case kindNotification:
		return "notification"
	case kindResponse:
		return "response"
	default:
		return "invalid"
	}
}

// validateEnvelope 严格分类 JSON-RPC 消息（docs/mcp/transport.md §2）。
// 任一规则违反返 (kindInvalid, ErrMCPProtocolError)。
// 调用方（recvLoop / Server handler）必须在分发前调用同一函数，不得自行猜测 kind。
func validateEnvelope(msg *Message) (messageKind, error) {
	if msg == nil {
		return kindInvalid, fmt.Errorf("%w: nil message", ErrMCPProtocolError)
	}
	if msg.JSONRPC != "2.0" {
		return kindInvalid, fmt.Errorf("%w: jsonrpc must be \"2.0\"", ErrMCPProtocolError)
	}
	hasID := msg.ID != nil && len(msg.ID) > 0 && !isNullJSON(msg.ID)
	hasMethod := msg.Method != ""
	hasParams := len(msg.Params) > 0
	hasResult := len(msg.Result) > 0
	hasError := msg.Error != nil

	if hasMethod && hasResult || hasMethod && hasError {
		// request/notification 不能携带 result/error
		return kindInvalid, fmt.Errorf("%w: method and result/error mutually exclusive", ErrMCPProtocolError)
	}
	if hasResult && hasError {
		// response 只允许单一结果或错误
		return kindInvalid, fmt.Errorf("%w: result and error mutually exclusive", ErrMCPProtocolError)
	}
	if !hasID && (hasResult || hasError) {
		// response 必须有 ID
		return kindInvalid, fmt.Errorf("%w: response without id", ErrMCPProtocolError)
	}
	if !hasMethod && !hasResult && !hasError {
		// 既无 method 无 result/error 既不是 request/notification/response
		return kindInvalid, fmt.Errorf("%w: empty envelope", ErrMCPProtocolError)
	}

	switch {
	case hasID && hasMethod:
		// 请求：有 ID + Method；不得 result/error（上面已校验）
		return kindRequest, nil
	case !hasID && hasMethod:
		// 通知：无 ID + Method；不得 result/error（上面已校验）；params 允许
		return kindNotification, nil
	case hasID && (hasResult || hasError) && !hasMethod:
		// 响应：有 ID + 单一 result/error；不得 method/params
		if hasParams {
			return kindInvalid, fmt.Errorf("%w: response must not carry params", ErrMCPProtocolError)
		}
		return kindResponse, nil
	default:
		return kindInvalid, fmt.Errorf("%w: ambiguous envelope", ErrMCPProtocolError)
	}
}

// isNullJSON 判断 ID 字节流是否为 JSON null 字面量（"null"）。
// ID 字段在 wire 上要么省略（无 ID = notification），要么是 string 或正 number；
// 显式 null ID 视作无效。
func isNullJSON(b json.RawMessage) bool {
	if len(b) < 4 {
		return false
	}
	s := string(b)
	return s == "null" || s == "\"null\""
}

// preferredVersion 按 transport 类型决定 Initialize 请求发送的协议版本
// （docs/mcp/client.md §2 矩阵）。
func preferredVersion(transportType string) string {
	switch transportType {
	case "streamable_http":
		return ProtocolVersion
	case "sse":
		return LegacyProtocolVersion
	case "stdio":
		return ProtocolVersion
	default:
		// 未知 transport → 默认首选，由 caller / transport.Info() 决定（不应发生）。
		return ProtocolVersion
	}
}

// acceptsVersion 判断 Server 返回的版本是否在该 transport 允许的接受集合内。
// 不在则 Client 关连接返 ErrMCPProtocolError（docs/mcp/client.md §2 矩阵）。
func acceptsVersion(transportType, serverVersion string) bool {
	switch transportType {
	case "streamable_http":
		return serverVersion == ProtocolVersion
	case "sse":
		return serverVersion == LegacyProtocolVersion
	case "stdio":
		return serverVersion == ProtocolVersion || serverVersion == LegacyProtocolVersion
	default:
		return false
	}
}

// parseID 将 wire 上的 string 或 number ID 解析为 uint64。
// 返回 (id, ok)；ok=false 表示 ID 既不是合法 number 也不是 string 数值，
// 或超出 uint64 范围。Notification ID 为空时返 (0, false)。
// docs: Client recvLoop 解析 response ID 为正 uint64；只接受 Client 自己签发的正整数 ID。
func parseID(id json.RawMessage) (uint64, bool) {
	if len(id) == 0 {
		return 0, false
	}
	// number：直接 ParseUint；Client 用 strconv.FormatUint 生成的 ID 是十进制数字字面量
	if id[0] >= '0' && id[0] <= '9' {
		v, err := strconv.ParseUint(string(id), 10, 64)
		if err != nil || v == 0 {
			return 0, false
		}
		return v, true
	}
	// string：MVP Client 不签发 string ID；返回 false 视作协议错误或 late response
	return 0, false
}
