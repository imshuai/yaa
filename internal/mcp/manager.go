package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/tool"
	"golang.org/x/exp/slog"
)

// runUpstream heartbeat 固定参数（docs/mcp/config-ref.md §7.2）。
// 本 commit 不引入 reconnect / reconciliation / listChanged 调度，但 ticker 已实作.
const (
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 10 * time.Second
)
// Manager 是 catalog、稳定 Tool Proxy、heartbeat 和重连的唯一 owner
// （docs/mcp/README.md §2）。Manager 不向调用方暴露可变 *Client，
// 调用方只能拿到 ServerStatus 快照与 Tool 列表深拷贝。
//
// 本系列 commit：实现 stdio auto_start 上游连接 + DiscoverTools + 稳定 Proxy 注册到 ToolManager,
// 暴露 ServerStatus.ProtocolVersion，并落地 runUpstream async goroutine (heartbeat ticker 30s +
// 10s Ping timeout + compare-and-clear generation 失败清理 + Stop 全 goroutine 同步退出).
// lifecycle: Prepare 同步启动 stdio auto_start Client (connect → initialize → discovertools → register),
// 成功后启动 runUpstream goroutine; 失败仅标记 server 的 LastError + Status=Error, 不阻断其他 server.
// SSE / Streamable HTTP transport、heartbeat 失败后的指数退避重连、catalog reconciliation、
// tools/list_changed 通知合并待后续 commit.
type Manager struct {
	cfg    *config.MCPConfig
	logger *slog.Logger
	tm     *tool.Manager

	// entries 是配置中声明的上游 server，列表投影源。每 entry 持有运行时状态。
	entries []serverEntry

	// runCtx/cancel 是 Manager 生命周期；Stop 取消它使 Serve 与 runUpstream 退出。
	runCtx    context.Context
	cancelRun context.CancelFunc

	// done 在 teardown 完成后关闭；Stop 后再次 Stop 读 cacheErr。
	doneOnce sync.Once
	done     chan struct{}
	stopOnce sync.Once
	cacheErr error

	// ready 反映本地 Serve 是否在运行。v1：未配置本地 *Server，恒 true 直到 Stop。
	// 后续实现本地 Server 时，Serve 意外退出置 false 推动 Runtime unhealthy。
	readyMu sync.RWMutex
	ready   bool

	// upstreamWG 跟踪所有 entry 的 runUpstream goroutine；Stop 等其全部退出再 close done
	// （docs/mcp/config-ref.md §7.3 teardown）。
	upstreamWG sync.WaitGroup

	mu sync.RWMutex
}


// serverEntry 是 Manager 内部持有的配置 server 投影源 + 运行时状态。
// name/transport 是配置投影；handle/client/status/tools 是运行时状态；
// cfg 缓存 stdio auto_start 启动所需字段（command/args/env/timeout）。
type serverEntry struct {
	name      string
	transport string
	cfg       config.MCPServerConfig
	handle    *ProxyHandle     // 同一 server 的所有 Proxy 共享
	client    *Client          // 当前代连接；断线置 nil
	status    ServerStatus     // 含 ToolCount/ProtocolVersion/ConnectedAt/LastError
	tools     []tool.ToolInfo // 已发现的 Tool 快照深拷贝
	// generation 是 runUpstream 用于 compare-and-clear 的代际计数；每代 Client 一个 generation，
	// 新代替换旧代时递增（docs/mcp/config-ref.md §7.2）。本期 connectStdioServer 启动时 generation=0。
	generation uint64
	// listChanged 是该代独有的合并通道（cap 1），本期仅声明，下一 commit 接 tools/list_changed
	// notification 投递与 catalog reconciliation；旧代不被新代复用以避免迟到 callback 触发重连。
	listChanged chan struct{}
}

// NewManager 构造 MCP Manager。cfg 不可为 nil（构造前由 config.Validate 保证
// 各 server name/transport/url 等已通过校验，本构造函数不再重复校验）。tm 可为 nil，
// 仅当存在 auto_start=true 的 stdio server 时才真正使用 ToolManager 注册 Proxy。
//
// 构造后 entry.status 全部 Disconnected、tools=nil；Prepare 才启动上游连接。
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
		tm:     tm,
		ready:  true, // v1：无本地 Serve，恒就绪；本地 Server 实现后由 Serve 状态决定
		done:   make(chan struct{}),
	}
	m.entries = make([]serverEntry, 0, len(cfg.Servers))
	for _, s := range cfg.Servers {
		transport := s.Transport
		if transport == "" {
			transport = "stdio" // docs/mcp/config-ref.md §2：transport 缺省 stdio
		}
		m.entries = append(m.entries, serverEntry{
			name:      s.Name,
			transport: transport,
			cfg:       s,
			status: ServerStatus{
				Name:      s.Name,
				Status:    StatusDisconnected,
				Transport: transport,
			},
		})
	}
	m.runCtx, m.cancelRun = context.WithCancel(context.Background())
	return m, nil
}

// Prepare 校验并持有本地 transport，启动 auto_start 上游 Client 并注册稳定 Tool Proxy，
// 但不运行本地 Serve。（docs/mcp/README.md §2、docs/mcp/integration.md §4、docs/mcp/checklist.md §1）
//
// 本 commit：仅 stdio + auto_start=true 的 server 启动真实连接 ——
// StdioClient → Connect(connTimeout) → Initialize(initTimeout) → DiscoverTools →
// 每个 tool 注册 MCPToolProxy 到 ToolManager。其他 transport 暂保持 Disconnected，
// 等 SSE / Streamable HTTP 后续 commit 实现。
// 任一 server 失败仅标 LastError + Status=Error，不影响其他 server 或 Runtime 启动。
// ToolManager.Register 失败（罕见：canonical 重名 / 空 description）也只标 LastError，不停止其他工作。
func (m *Manager) Prepare() error {
	for i := range m.entries {
		e := &m.entries[i]
		if !e.cfg.AutoStart {
			continue
		}
		if e.transport != "stdio" {
			// SSE / Streamable HTTP 待后续 commit。
			m.mu.Lock()
			e.status.LastError = "transport not supported in current build"
			m.mu.Unlock()
			continue
		}
		m.connectStdioServer(e)
	}
	return nil
}

// connectStdioServer 启动单个 stdio auto_start server：构造 transport，
// 建立连接，握手，发现工具，并向 ToolManager 注册稳定 Proxy。
// 失败仅更新 e.status（LastError + Status=Error）；成功更新 Status=connected + ToolCount + ConnectedAt + ProtocolVersion。
func (m *Manager) connectStdioServer(e *serverEntry) {
	// handler := NewProxyHandle  // 反正 etm 的可空性，每 server 一个独立 handle.
	handle := &ProxyHandle{}

	// 构造 transport。command 缺失属配置错误（已在 config.Validate 校验，此处兜底）。
	if e.cfg.Command == "" {
		m.mu.Lock()
		e.status.Status = StatusError
		e.status.LastError = "stdio server missing command"
		m.mu.Unlock()
		return
	}
	tr := NewStdioClient(e.cfg.Command, e.cfg.Args, e.cfg.Env, m.logger)
	client := NewClient(e.name, m.runCtx, tr)

	// 应用 connect timeout（缺省 10s）。
	connTimeout := e.cfg.Timeout
	if connTimeout <= 0 {
		connTimeout = m.cfg.Timeout.Connect
	}
	if connTimeout <= 0 {
		connTimeout = 10 * time.Second // docs §6 default 表
	}
	connCtx, cancel := context.WithTimeout(m.runCtx, connTimeout)
	defer cancel()
	if err := client.Connect(connCtx); err != nil {
		m.mu.Lock()
		e.status.Status = StatusError
		e.status.LastError = err.Error()
		m.mu.Unlock()
		return
	}

	// initialize timeout（缺省 15s）。
	initTimeout := m.cfg.Timeout.Init
	if initTimeout <= 0 {
		initTimeout = 15 * time.Second
	}
	initCtx, initCancel := context.WithTimeout(m.runCtx, initTimeout)
	defer initCancel()
	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		m.mu.Lock()
		e.status.Status = StatusError
		e.status.LastError = err.Error()
		m.mu.Unlock()
		return
	}

	// DiscoverTools 用 initTimeout 的同口径 hardcap，避免分页长尾卡住 Runtime 启动。
	toolsCtx, toolsCancel := context.WithTimeout(m.runCtx, initTimeout)
	defer toolsCancel()
	tools, err := client.DiscoverTools(toolsCtx)
	if err != nil {
		_ = client.Close()
		m.mu.Lock()
		e.status.Status = StatusError
		e.status.LastError = err.Error()
		m.mu.Unlock()
		return
	}

	// MCP tool hard timeout（0 = 仅使用 caller deadline）。
	toolTimeout := m.cfg.Timeout.Tool
	if e.cfg.Timeout > 0 {
		toolTimeout = e.cfg.Timeout
	}

	// 注册每个 Tool 为稳定 Proxy。ToolManager 缺失或 Register 失败 →
	// 关 client + 记 LastError（重连 / 重试不在本 commit）。
	if m.tm != nil {
		for _, mt := range tools {
			if mt.Description == "" {
				// Ponytail：上游送空 description 在 normalizeTool 不强制拒绝；
				// 但 ToolManager.Register 拒空描述 → Manager 视为协议错并标 LastError 后收敛。
				_ = client.Close()
				m.mu.Lock()
				e.status.Status = StatusError
				e.status.LastError = fmt.Sprintf("tool %q missing description", mt.Name)
				m.mu.Unlock()
				return
			}
			proxy := NewMCPToolProxy(e.name, stripServerPrefix(e.name, mt.Name), mt.Description, mt.InputSchema, toolTimeout, handle)
			if err := m.tm.Register(proxy); err != nil {
				_ = client.Close()
				m.mu.Lock()
				e.status.Status = StatusError
				e.status.LastError = fmt.Sprintf("register tool %q: %v", proxy.Name(), err)
				m.mu.Unlock()
				return
			}
		}
	}

	// 成功：handle 持有当前代 client；entry 持有 client 与 handle 与状态；generation=0
	// 是首代；listChanged 是该代独有合并通道。启动 runUpstream goroutine（heartbeat ticker）。
	handle.Store(client)
	m.mu.Lock()
	e.handle = handle
	e.client = client
	e.generation = 0 // 初代；下一 commit 重连时递增
	if e.listChanged == nil {
		e.listChanged = make(chan struct{}, 1)
	} else {
		// 复用 entry；清空可能的历史投递。
		select {
		case <-e.listChanged:
		default:
		}
	}
	now := time.Now()
	e.status.Status = StatusConnected
	e.status.ConnectedAt = &now
	e.status.ToolCount = len(tools)
	// ProtocolVersion: 由 Client.Initialize 协商后保存的版本派生（legacy 兼容版本如 sse 选 2024-11-05 时正确反映）。
	pv := client.ProtocolVersion()
	e.status.ProtocolVersion = &pv
	// ToolInfo 快照深拷贝 → 支持 Manager.Tools(name) 返回.
	e.tools = make([]tool.ToolInfo, 0, len(tools))
	for _, mt := range tools {
		e.tools = append(e.tools, tool.ToolInfo{
			Name:        canonicalToolName(e.name, stripServerPrefix(e.name, mt.Name)),
			Description: mt.Description,
			Parameters:  append(tool.ToolInfo{}.Parameters, mt.InputSchema...),
			Enabled:     true,
			Source:      "mcp",
		})
	}
	m.mu.Unlock()

	// 启动 runUpstream goroutine：固定 30s heartbeat + 10s timeout。
	// 本期仅 Ping ticker；重连 / catalog reconciliation / listChanged 在下一 commit 接入。
	m.upstreamWG.Add(1)
	go m.runUpstream(e, handle, client, e.generation)
}

// stripServerPrefix 从 normalized canonical tool name 去掉 mcp.<server>. 前缀，
// 还原远端原始 name，供 MCPToolProxy 透传 CallTool 远端名。
// normalized mt.Name 已是 mcp.<server>.<remote>；前缀长度 = len("mcp.") + len(server) + len(".").
func stripServerPrefix(serverName, canonical string) string {
	prefix := "mcp." + serverName + "."
	if len(canonical) > len(prefix) && canonical[:len(prefix)] == prefix {
		return canonical[len(prefix):]
	}
	// 异常输入回退返回原串，避免 panic；正常路径不会走到这里.
	return canonical
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

// runUpstream 是每个 entry 唯一的重连 / heartbeat owner（docs/mcp/config-ref.md §7.2）。
// 当前 commit：仅实现 heartbeat ticker + client.Done() 失败信号；
// 重连、指数退避、catalog reconciliation、tools/list_changed 通知合并待后续 commit 接入。
//
// 入参：handle/client/entryCreationGen 都是 launch 时刻的快照（不动 entry.mu 之外的 e.client）：
//   - client: 当前代 Client，作为 ticker Ping 目标；
//   - gen: 启动时刻的代际；如果 Ping 失败、或 entry 的 e.generation 已升高（重连发生），
//     检测 stale 后退出本轮 goroutine，避免把旧代状态写到新 entry。
// 设计上 e.client 在多 goroutine（旧代仍然可能还在 ticker）下读 access 时，使用者需读 m.mu 下
// snapshot，避免使用 bare comparison（Ponytail：本 commit 单代 1 次，重连原子后真正加 generation 比对）.
func (m *Manager) runUpstream(e *serverEntry, handle *ProxyHandle, client *Client, gen uint64) {
	defer m.upstreamWG.Done()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.runCtx.Done():
			// Manager 关闭；Stop 路径会关闭 client，本 goroutine 退出.
			return
		case <-client.Done():
			// 当前代 Client 已 fail/Close（外部取消或 transport 断开）.
			// compare-and-clear handle 并标 error；本 commit 不重连.
			m.markGenerationFailed(e, handle, client, gen, "client done", client.Err())
			return
		case <-ticker.C:
			// heartbeat Ping: heartbeatTimeout 限制; 失败时标 error 并退出该代 goroutine.
			pingCtx, cancel := context.WithTimeout(m.runCtx, heartbeatTimeout)
			err := client.Ping(pingCtx)
			cancel()
			if err == nil {
				continue
			}
			// ctx 已取消（Manager 关闭）则不视为 heartbeat 失败.
			if m.runCtx.Err() != nil {
				return
			}
			m.markGenerationFailed(e, handle, client, gen, "heartbeat failed", err)
			return
		}
	}
}

// markGenerationFailed 在 heartbeat / Done() 触发失败时统一处理：
// （1）若 entry 已进入新区代（gen != e.generation）本次视为 stale，忽略；
// （2）否则在 m.mu 下比对 e.client==client 仍是当前代（避免与 Stop race），
//     handle.Store(nil)，关闭 client，置 status=Error + LastError.
// 本 commit 重连未接入：成功标志 error + unavailable 后停止 ticker goroutine.
// docs/mcp/config-ref.md §7.2:
//   "Ping/Recv/disconnect 的暂时错误先用 entry 当前 generation 做 compare-and-clear,
//    将 handle 置 nil、状态置 error，再在锁外关闭旧 Client".
func (m *Manager) markGenerationFailed(e *serverEntry, handle *ProxyHandle, client *Client, gen uint64, reason string, cause error) {
	m.mu.Lock()
	if e.generation != gen {
		// 已进入新区代；本代失败已无关.
		m.mu.Unlock()
		return
	}
	if e.client != client {
		// Stop 已将 e.client 置 nil 或替换，本边路视为已处理.
		m.mu.Unlock()
		return
	}
	if e.handle != nil {
		e.handle.Store(nil)
	}
	e.client = nil
	e.status.Status = StatusError
	if cause != nil {
		e.status.LastError = fmt.Sprintf("%s: %v", reason, cause)
	} else {
		e.status.LastError = reason
	}
	m.mu.Unlock()
	// 锁外关闭旧 client（Close 幂等，Stop 路径再次调用也无副作用）.
	_ = client.Close()
}

// Stop 同步清空 handles，后台 teardown 用 errors.Join 完成；Done 后再次 Stop 返回缓存的最终错误。
// （docs/mcp/README.md §2、docs/mcp/integration.md §4、docs/mcp/checklist.md §1）
// 本 commit: cancelRun (触发各 runUpstream goroutine 退出 via m.runCtx.Done() branch) + 关闭
// 每个已建立的 client (Close 幂等) + close done + ready=false; upstreamWG.Wait 确保 ticker 退出.
func (m *Manager) Stop(ctx context.Context) error {
	m.stopOnce.Do(func() {
		if m.cancelRun != nil {
			m.cancelRun()
		}
		// 关闭每条已建立的 client（包括已断线但 client handle 仍持有的代）。
		m.mu.Lock()
		for i := range m.entries {
			e := &m.entries[i]
			if e.client != nil {
				_ = e.client.Close()
				// 断线后置 handle nil，拒绝后续 Proxy 调用（docs/integration.md §1）.
				if e.handle != nil {
					e.handle.Store(nil)
				}
				e.client = nil
				e.status.Status = StatusDisconnected
			}
		}
		m.mu.Unlock()
		// 等所有 runUpstream goroutine 退出再 close done.
		m.upstreamWG.Wait()
		m.doneOnce.Do(func() { close(m.done) })
		m.readyMu.Lock()
		m.ready = false
		m.readyMu.Unlock()
	})
	return m.cacheErr
}

// Done 在 teardown 完成后关闭。Runtime 调 Stop 后必须等 Done，再用 fresh ctx 调 Stop
// 取得最终错误，之后才能关闭 Tool Manager 等依赖。（docs/mcp/README.md §2）
func (m *Manager) Done() <-chan struct{} {
	return m.done
}

// Ready 反映本地 Serve 是否在运行。本地 Serve 意外退出后返回 false，使 Runtime
// 进入 unhealthy/Not Ready。（docs/mcp/README.md §2、docs/mcp/checklist.md §1）
// v1 起：未配置本地 Server 或本地 Server 已稳定退出，恒 true 到 Stop。
func (m *Manager) Ready() bool {
	m.readyMu.RLock()
	defer m.readyMu.RUnlock()
	return m.ready
}

// List 列出所有配置的上游连接状态。（docs/mcp/README.md §2、docs/mcp/checklist.md §1）
// 返回 ServerStatus 副本，调用方修改不影响 Manager 内部状态。
func (m *Manager) List() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerStatus, 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, cloneServerStatus(e.status))
	}
	return out
}

// Get 返回指定 server 的当前状态快照。找不到 name 返 (zero, false)。
func (m *Manager) Get(name string) (ServerStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e.name == name {
			return cloneServerStatus(e.status), true
		}
	}
	return ServerStatus{}, false
}

// Tools 返回指定 server 当前已发现的 Tool 列表深拷贝，不暴露可变 Client。
// （docs/mcp/README.md §2、docs/mcp/checklist.md §1 §7）
// server 已配置但未连接或未注册成功 Tools → (nil, false)；不存在 → (nil, false)。
func (m *Manager) Tools(name string) ([]tool.ToolInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.entries {
		if e.name == name {
			if e.status.Status != StatusConnected || len(e.tools) == 0 {
				return nil, false
			}
			out := make([]tool.ToolInfo, len(e.tools))
			copy(out, e.tools)
			return out, true
		}
	}
	return nil, false
}

// cloneServerStatus 深拷贝 ServerStatus（ConnectedAt 指针单独复制）。
func cloneServerStatus(s ServerStatus) ServerStatus {
	out := s
	if s.ConnectedAt != nil {
		t := *s.ConnectedAt
		out.ConnectedAt = &t
	}
	return out
}
