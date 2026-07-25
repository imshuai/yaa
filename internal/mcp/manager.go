package mcp

import (
	"context"
	"sync"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/tool"
	"golang.org/x/exp/slog"
)

// Manager 是 catalog、稳定 Tool Proxy、heartbeat 和重连的唯一 owner
// （docs/mcp/README.md §2）。Manager 不向调用方暴露可变 *Client，
// 调用方只能拿到 ServerStatus 快照与 Tool 列表深拷贝。
//
// v1 起点（本 commit）：仅落地 Manager 结构骨架与 List/Get 真实实现，
// 让 Remote API mcp/servers 端点能返空列表联调。Prepare/Activate/Stop/Done/
// Ready/Tools 满足基本契约但暂不实际连接上游 —— lifecycle / transport /
// heartbeat / 重连见 docs/mcp/checklist.md，后续 multi-commit 渐进补全。
type Manager struct {
	cfg    *config.MCPConfig
	logger *slog.Logger

	// entries 是配置中声明的上游 server 名字与 transport，列表投影源。
	// v1：不持有 Client，不管理连接生命周期。
	entries []serverEntry

	// runCtx/cancel 是 Manager 生命周期；Stop 取消它使 Serve 与 runUpstream 退出。
	runCtx    context.Context
	cancelRun context.CancelFunc

	// done 在 teardown 完成后关闭；Stop 后再次 Stop 读 cacheErr。
	doneOnce sync.Once
	done     chan struct{}
	stopOnce sync.Once
	cacheErr error

	// ready 反映本地 Serve 是否在运行。v1：未配置本地 *Server，恒 true。
	// 后续实现本地 Server 时，Serve 意外退出置 false 推动 Runtime unhealthy。
	readyMu sync.RWMutex
	ready   bool

	mu sync.RWMutex
}

// serverEntry 是 Manager 内部持有的配置 server 投影源。v1 只缓存非敏感字段。
type serverEntry struct {
	name      string
	transport string
}

// NewManager 构造 MCP Manager。cfg 不可为 nil（构造前由 config.Validate 保证
// 各 server name/transport/url 等已通过校验，本构造函数不再重复校验）。
//
// v1 起：仅缓存配置以供 List/Get 投影，不启动任何上游 Client、不进入 Prepare/Activate。
func NewManager(cfg *config.MCPConfig, tm *tool.Manager, logger *slog.Logger) (*Manager, error) {
	if cfg == nil {
		return nil, ErrMCPConfig
	}
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		cfg:    cfg,
		logger: logger,
		ready:  true, // v1：无本地 Serve，恒就绪；本地 Server 实现后由 Serve 状态决定
		done:   make(chan struct{}),
	}
	// 缓存配置 server 投影源（不持有敏感字段：command/args/env/headers/tls 不进入 entries）。
	m.entries = make([]serverEntry, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		transport := s.Transport
		if transport == "" {
			transport = "stdio" // docs/mcp/config-ref.md §2：transport 缺省 stdio
		}
		m.entries = append(m.entries, serverEntry{name: s.Name, transport: transport})
	}
	// v1：runCtx 不限制 Manager 生命周期（Stop 取消即 teardown）；本地 Server 实现后
	// 由 Serve 持有此 ctx。
	m.runCtx, m.cancelRun = context.WithCancel(context.Background())
	// 没有任何 lifecycle，相当于 Manager 立即处于 teardown-done 状态，方便 Done 可读。
	// 真实 lifecycle 落地后移除这一关闭；当前 v1 让 Prepare/Activate/Stop 保持幂等无副作用。
	return m, nil
}

// Prepare 校验并持有本地 transport，启动 auto_start 上游 Client 并注册稳定 Tool Proxy，
// 但不运行本地 Serve。（docs/mcp/README.md §2、docs/mcp/checklist.md §1）
//
// v1 起点：尚未实现 transport/Client/Proxy，立即返回 nil 表示"无本地 transport 需要 prepare"。
// 后续 commit 接入 stdio/sse/streamable_http transport 时实现真实 Prepare。
func (m *Manager) Prepare() error {
	return nil
}

// Activate 仅在 Config.Activate(binding) 成功后由 Runtime 调用，启动本地 MCP Server。
// （docs/mcp/README.md §2、docs/mcp/checklist.md §1）
//
// v1 起点：未配置 / 未实现本地 Server。若配置了 mcp.server.enabled=true 而本地 Server 实现
// 仍未交付，返回 ErrMCPConfig 让 Runtime 启动失败而非静默启用 —— 避免 Remote 反过来被
// 空 Server 接受请求产生语义错乱。
func (m *Manager) Activate() error {
	if m.cfg.Server.Enabled {
		return ErrMCPConfig
	}
	return nil
}

// Stop 同步清空 handles，后台 teardown 用 errors.Join 完成；Done 后再次 Stop 返回缓存的最终错误。
// （docs/mcp/README.md §2、docs/mcp/checklist.md §1）
// v1 起：无 handles、无 Serve，立即关闭 done 并返回 nil；再次调用读到 cacheErr。
func (m *Manager) Stop(ctx context.Context) error {
	m.stopOnce.Do(func() {
		if m.cancelRun != nil {
			m.cancelRun()
		}
		m.doneOnce.Do(func() { close(m.done) })
		m.readyMu.Lock()
		m.ready = false
		m.readyMu.Unlock()
	})
	return m.cacheErr
}

// Done 在 teardown 完成后关闭。Runtime 调 Stop 后必须等 Done，再用 fresh ctx 调 Stop
// 取得最终错误，之后才能关闭 Tool Manager 等依赖。（docs/mcp/README.md §2）
// v1 起：构造即 teardown 已完成（无 lifecycle），Done 立即可读。
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

// Ready 反映本地 Serve 是否在运行。本地 Serve 意外退出后返回 false，使 Runtime
// 进入 unhealthy/Not Ready。（docs/mcp/README.md §2、docs/mcp/checklist.md §1）
// v1 起：未配置本地 Server 或本地 Server 已稳定退出，恒 true。
func (m *Manager) Ready() bool {
	m.readyMu.RLock()
	defer m.readyMu.RUnlock()
	return m.ready
}

// List 列出所有配置的上游连接状态。（docs/mcp/README.md §2、docs/mcp/checklist.md §1）
// v1 起：所有 server 都处于 StatusDisconnected（无连接 lifecycle）。
// 返回的是 ServerStatus 副本，调用方修改不影响 Manager 内部状态。
func (m *Manager) List() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerStatus, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, ServerStatus{
			Name:      e.name,
			Status:    StatusDisconnected,
			Transport: e.transport,
			// ToolCount=0、ProtocolVersion nil、ConnectedAt nil、LastError 空：v1 未连接。
			ToolCount: 0,
		})
	}
	return out
}

// Tools 返回指定 server 当前已发现的 Tool 列表深拷贝，不暴露可变 Client。
// （docs/mcp/README.md §2、docs/mcp/checklist.md §1 §7）
// v1 起：无连接，无 Tool；返回 (nil, false) 表示 server 存在但当前未连接。
func (m *Manager) Tools(name string) ([]tool.ToolInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e.name == name {
			return nil, false // server 已配置但 v1 未连接：Tools 暂为 empty / unavailable
		}
	}
	return nil, false
}

// Get 返回指定 server 的当前状态快照。返回 ServerStatus 副本，修改不影响 Manager 内部状态。
// 找不到 name 返 (zero, false)。
func (m *Manager) Get(name string) (ServerStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e.name == name {
			return ServerStatus{
				Name:      e.name,
				Status:    StatusDisconnected,
				Transport: e.transport,
				ToolCount: 0,
			}, true
		}
	}
	return ServerStatus{}, false
}
