package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/imshuai/yaa/internal/tool"
)

// ProxyHandle 是同一上游所有稳定 Proxy 共享的 handle。
// Manager 在重连时原子替换 Load() 返回的 *Client；断线时 Store(nil)。
// docs/mcp/integration.md §1：首次发现成功后 Proxy 注册一次；暂时断线对共享 handle
// 执行 Store(nil)，不从 Tool Manager 注销。
type ProxyHandle struct {
	client atomic.Pointer[Client]
}

// Load 返回当前代 *Client；断线时返回 nil。
func (h *ProxyHandle) Load() *Client {
	if h == nil {
		return nil
	}
	return h.client.Load()
}

// Store 原子替换当前代 *Client；nil 标记断线。
func (h *ProxyHandle) Store(c *Client) {
	if h == nil {
		return
	}
	h.client.Store(c)
}

// MCPToolProxy 是单个 MCP Tool 的稳定 Yaa! Tool 接口适配器。
// 同一上游 server 的所有 Proxy 共享一个 ProxyHandle；远端调用经 handle.Load() 路由到当前代 Client。
// docs/mcp/integration.md §1：Proxy 保存 server、远端 tool name、description、不可变 schema
// 和可选 MCP hard timeout。scope 只用于 Yaa! 权限/审计，不塞进远端 Tool arguments。
type MCPToolProxy struct {
	server      string
	remoteName  string
	description string // MCP 返回的 Tool 描述，非空（ToolManager.Register 拒绝空描述）
	schema      json.RawMessage
	timeout     time.Duration // 0 表示只使用 Tool Manager 的 caller deadline
	handle      *ProxyHandle
}

// NewMCPToolProxy 构造稳定 Proxy。handle 不可为 nil；description 不可为空（由 Manager 校验）。
// schema 深拷贝为不可变快照。timeout=0 时只使用 caller deadline。
func NewMCPToolProxy(server, remoteName, description string, schema json.RawMessage, timeout time.Duration, handle *ProxyHandle) *MCPToolProxy {
	return &MCPToolProxy{
		server:      server,
		remoteName:  remoteName,
		description: description,
		schema:      append(json.RawMessage(nil), schema...),
		timeout:     timeout,
		handle:      handle,
	}
}

// Name 返回 canonical Yaa! Tool name: mcp.<server>.<remote>。
func (p *MCPToolProxy) Name() string { return canonicalToolName(p.server, p.remoteName) }

// Description 返回 MCP 上游 Tool 描述（不可变快照）。
func (p *MCPToolProxy) Description() string { return p.description }

// Parameters 返回不可变 JSON Schema 快照；空 schema 退化为 `{}`，避免被 ToolManager 拒。
func (p *MCPToolProxy) Parameters() json.RawMessage {
	if len(p.schema) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), p.schema...)
}

// Execute 调用上游 Server 端 Tool（docs/mcp/integration.md §1）。
// 断线返 ErrMCPUnavailable；MCP hard timeout 用 WithCancelCause + AfterFunc
// （Go 1.20 没有 WithTimeoutCause）。结果统一转 tool.ToolResult。
func (p *MCPToolProxy) Execute(ctx context.Context, scope tool.ExecutionScope, params map[string]any) (tool.ToolResult, error) {
	client := p.handle.Load()
	if client == nil {
		return tool.ToolResult{}, ErrMCPUnavailable
	}
	callCtx := ctx
	stopTimeout := func() {}
	if p.timeout > 0 {
		var cancel context.CancelCauseFunc
		callCtx, cancel = context.WithCancelCause(ctx)
		timer := time.AfterFunc(p.timeout, func() {
			cancel(ErrMCPToolTimeout)
		})
		stopTimeout = func() {
			timer.Stop()
			cancel(nil)
		}
	}
	defer stopTimeout()
	result, err := client.CallTool(callCtx, p.remoteName, params)
	if ctx.Err() != nil {
		return tool.ToolResult{}, context.Cause(ctx)
	}
	if callCtx.Err() != nil {
		return tool.ToolResult{}, context.Cause(callCtx)
	}
	return toToolResult(result, err)
}

// toToolResult 把 MCP CallToolResult + wire err 映射为 Yaa! ToolResult
// （docs/mcp/integration.md §1）。空 content → 空文本；多个 text block 按 wire 顺序
// 以单个换行连接；isError 原样保留；内部 Meta 不上 wire。非 text type 返 ErrMCPUnsupportedContent。
func toToolResult(result *CallToolResult, err error) (tool.ToolResult, error) {
	if err != nil {
		return tool.ToolResult{}, err
	}
	if result == nil {
		return tool.ToolResult{}, ErrMCPProtocolError
	}
	parts := make([]string, 0, len(result.Content))
	total := 0
	for _, content := range result.Content {
		if content.Type != "text" {
			return tool.ToolResult{}, ErrMCPUnsupportedContent
		}
		total += len(content.Text)
		if total > 4<<20 {
			return tool.ToolResult{}, ErrMCPProtocolError
		}
		parts = append(parts, content.Text)
	}
	return tool.ToolResult{
		Content: strings.Join(parts, "\n"),
		IsError: result.IsError,
	}, nil
}

// toMCPResult 把 Yaa! ToolResult 反向映射为 MCP CallToolResult，
// 供本地 MCP Server 实现（docs/mcp/integration.md §1）。
// 反向映射始终产生一个 text block，即使 Content 为空。
func toMCPResult(result tool.ToolResult) *CallToolResult {
	return &CallToolResult{
		Content: []Content{{Type: "text", Text: result.Content}},
		IsError: result.IsError,
	}
}
