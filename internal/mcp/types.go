// Package mcp 实现 Yaa! 的 MCP（Model Context Protocol）支持：
// 既作为 Client 连接外部 MCP Server，又作为 Server 对外暴露本地 Tool。
//
// MVP 范围与协议版本见 docs/mcp/README.md §1：仅 Tool capability；
// 首选 2025-03-26，legacy SSE 接受 2024-11-05。Resource/Prompt 留待后续版本。
//
// 本文件定义跨 Manager / Client / Server 共享的状态类型与错误 sentinel。
// 错误.Assertion 边界见 docs/mcp/errors.md。
package mcp

import (
	"errors"
	"time"
)

// ConnectionStatus 是单个上游 MCP 连接的状态机取值。
// 状态转换图见 docs/mcp/errors.md §3：disconnected → connecting → connected → error（→ connecting 重连）。
type ConnectionStatus string

const (
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
	StatusError        ConnectionStatus = "error"
)

// ServerStatus 是 Manager、健康快照与 Remote 投影共用的唯一上游状态类型
// （docs/mcp/README.md §2）。敏感连接配置（command/args/env/headers/tls）
// 不进入该类型，避免通过 Remote API / 健康端点泄露。
type ServerStatus struct {
	Name            string           `json:"name"`
	Status          ConnectionStatus `json:"status"`
	Transport       string           `json:"transport"`
	ProtocolVersion *string          `json:"protocol_version"`
	ToolCount       int              `json:"tool_count"`
	ConnectedAt     *time.Time       `json:"connected_at"`
	LastError       string           `json:"last_error,omitempty"`
}

// 错误 sentinel（docs/mcp/errors.md §1）。所有 sentinel 仅用于 typed 判别；
// 具体字段路径错误由 ValidationError 在配置校验阶段携带，不再扩展零散 sentinel。
var (
	ErrMCPConfig             = errors.New("invalid mcp config")
	ErrMCPConnRefused        = errors.New("mcp connection refused")
	ErrMCPConnTimeout        = errors.New("mcp connection timeout")
	ErrMCPAuthFailed         = errors.New("mcp upstream authentication failed")
	ErrMCPTransportClosed    = errors.New("mcp transport closed")
	ErrMCPTransportWrite     = errors.New("mcp transport write failed")
	ErrMCPProtocolError      = errors.New("mcp protocol error")
	ErrMCPInvalidParams      = errors.New("invalid mcp parameters")
	ErrMCPToolNotFound       = errors.New("mcp tool not found")
	ErrMCPToolExecFailed     = errors.New("mcp tool execution failed")
	ErrMCPToolTimeout        = errors.New("mcp tool timeout")
	ErrMCPUnsupportedContent = errors.New("unsupported mcp content")
	ErrMCPUnavailable        = errors.New("mcp server unavailable")
)
