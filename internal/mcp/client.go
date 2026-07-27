package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
)

// runtimeVersion 是 InitializeParams.ClientInfo.Version 字段值。
// v1 不从 build 注入；用常量以避免引入 build flag 依赖（Ponytail：单 caller 字面量）。
const runtimeVersion = "0.0.0-dev"

// 客户端 wire 限制常量（docs/mcp/transport.md §2 表）。
const (
	maxPendingRequests      = 1024
	maxListToolsPages       = 128
	maxTotalTools           = 4096
	maxCursorBytes          = 4096
	maxToolNameBytes        = 128
	maxCanonicalNameBytes   = 256
	maxDescriptionBytes     = 4096
	maxInputSchemaBytes     = 256 * 1024 // 256 KiB
	bestEffortCancelTimeout = 100 * time.Millisecond
	controlBufferSize       = 32
)

// clientResponse 是 pendingCall 容量 1 channel 的交付结构。
// recvLoop 摘除 pending entry 后投递一次，request goroutine 接收。
type clientResponse struct {
	msg *Message
	err error
}

// pendingCall 是单次 request 的等待状态；ch 容量 1，
// 由 recvLoop 或 fail 恰好投递一次。
type pendingCall struct {
	ch chan clientResponse
}

// Client 是单代 MCP 连接（docs/mcp/client.md §1）。Client 不拥有稳定 Proxy、catalog、退避或重连：
// Manager 为每次重连创建新的 Client，旧 Client 的 closeOnce 不复用。
//
// v1 这里：尚未接 Manager 与 stdio/sse/streamable_http 真实 transport；
// 独立 runnable + fake transport 测试完整握手 + 工具发现 + 调用 + 关闭 + 失败路径。
// 接 stdio 后 Manager 通过 NewClient(name, runCtx, transport) 构造并 connect bootstrap。
type Client struct {
	name      string
	runCtx    context.Context // Manager 生命周期；非 startup timeout
	transport ClientTransport

	mu     sync.RWMutex
	status ConnectionStatus
	// protocolVersion 是 Initialize 协商后的 Server 选择版本（docs/transport.md §2）。
	// 在 mu 下读写：Initialize 成功后只读。Manager 投影到 ServerStatus.ProtocolVersion。
	protocolVersion string
	cancel          context.CancelFunc
	// onListChanged 由 Manager 设置; recvLoop 检测到 tools/list_changed notification 时
	// 非阻塞调用本回调. 回调必须快速 (投递 cap-1 channel), 不得在 recvLoop 中发起 request
	// (docs/mcp/client.md §recvLoop, docs/mcp/config-ref.md §7.2). nil 时 notification 被容忍丢弃.
	onListChanged  func()

	closeOnce sync.Once
	failOnce  sync.Once
	closing   bool
	closeErr  error

	closedErr error
	pendingMu sync.Mutex
	pending   map[uint64]*pendingCall

	wg       sync.WaitGroup
	control  chan *Message
	recvDone chan struct{} // recvLoop 完成

	nextID          uint64
	issuedHighWater uint64
}

// NewClient 构造未连接的 Client（docs/mcp/client.md §2）。
// runCtx 是 Manager 生命周期；startup timeout 由 Connect 参数控制而非 runCtx。
// 调用方负责之后 Connect → Initialize → DiscoverTools。
func NewClient(name string, runCtx context.Context, transport ClientTransport) *Client {
	return &Client{
		name:      name,
		runCtx:    runCtx,
		transport: transport,
		status:    StatusDisconnected,
		pending:   make(map[uint64]*pendingCall),
		recvDone:  make(chan struct{}),
	}
}

// SetOnListChanged 设置 tools/list_changed notification 投递回调, 必须在 Connect 前调用.
// fn 由 Manager 注册, 必须自身非阻塞并线程安全; recvLoop 在 notification 路径上直接调用本回调.
// docs/mcp/client.md §recvLoop: 回调不得在 recvLoop 中发起 request, 仅可向容量为 1 的合并 channel 非阻塞投递.
func (c *Client) SetOnListChanged(fn func()) {
	c.pendingMu.Lock()
	c.onListChanged = fn
	c.pendingMu.Unlock()
}

// Status 返回当前状态快照（mu 保护）。
func (c *Client) Status() ConnectionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// ProtocolVersion 返回 Initialize 协商后 Server 选择的协议版本（docs/transport.md §2）。
// 未握手成功时返回空串；Manager 据此投影 ServerStatus.ProtocolVersion。
// 读在 mu 下保证一致快照。
func (c *Client) ProtocolVersion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.protocolVersion
}

// Done 是该代连接的无损关闭信号（docs/mcp/client.md §1）。
// failOnce 关闭，正常 Manager 关闭不生成重连事件；
// Manager 已在等待同 runCtx / generation 判定是否重连。
func (c *Client) Done() <-chan struct{} { return c.recvDone }

// Err 返回触发该代连接关闭的稳定错误。
// closedErr 在 pendingMu 下读写，返回稳定 sentinel 而非 transport 原始字符串。
func (c *Client) Err() error {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	return c.closedErr
}

// Connect 启动 recv / control goroutine 并完成 transport.Start。
// startupCtx 仅用于约束 transport.Start；握手成功后 startupCtx 取消不得关闭连接。
// 任一步失败调用 Close 并返回错误。
//
// v1：Connect 只 transport.Start + 起 loop；真正握手走 Initialize。
// 状态：disconnected → connecting → connected（仅当 transport.Start 成功后再由
// Initialize 成功标 connected；若 transport.Start 成功但握手未做，状态保持 connecting）。
func (c *Client) Connect(startupCtx context.Context) error {
	c.mu.Lock()
	if c.status != StatusDisconnected {
		c.mu.Unlock()
		return fmt.Errorf("%w: connect from non-disconnected state", ErrMCPProtocolError)
	}
	c.status = StatusConnecting
	// 从 runCtx 派生连接 ctx：transport.Recv/Send / recvLoop 走连接 ctx 生命周期。
	connCtx, cancel := context.WithCancel(c.runCtx)
	c.cancel = cancel
	c.mu.Unlock()

	// 先 transport.Start 拿到底层连接，再启动 dispatcher / control loop：
	// recvLoop 进入 transport.Recv 时需 transport stdout 已就绪（如 StdioClient）；
	// 反序会让 recvLoop 卡在尚未初始化的资源上。Start 失败则复位 status，不触发 failOnce。
	if err := c.transport.Start(connCtx); err != nil {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.status = StatusDisconnected
		c.mu.Unlock()
		return err
	}

	// 启动 dispatcher / control loop（构造时一次性加入 wg）。
	if err := c.startLoops(connCtx); err != nil {
		_ = c.Close()
		return err
	}
	// Start 成功但握手未完成：保持 StatusConnecting；Client 后续 Initialize 成功后切 connected。
	return nil
}

// startLoops 启动 recvLoop + controlLoop，只允许在 Connect 中调用一次。
// wg.Add 在 Connect 阶段一次性入栈，绝不允许多次或 Close 之后 Add（docs）。
func (c *Client) startLoops(connCtx context.Context) error {
	c.control = make(chan *Message, controlBufferSize)
	c.wg.Add(2)
	go c.runRecvLoop(connCtx)
	go c.runControlLoop(connCtx)
	return nil
}

// marshalParams 严格编码 params：nil → omitted（json.RawMessage(nil)）；
// 否则要求 object（map[string]any）或合法 JSON。
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("%w: bad params", ErrMCPInvalidParams)
	}
	// 严格检查必须是 object（首字符 '{'）或 array（兼容 notification/initialized: nil）
	// 单 nil 已上面 short-circuit；其余非 object 视作 bad params。
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return nil, fmt.Errorf("%w: params must be object or array", ErrMCPInvalidParams)
	}
	return raw, nil
}

// request 公共同步请求：marshal → 注册 pending → Send → 等响应。
// 严格检查 nextID/pending 上限；Send 失败按文档包装 ErrMCPTransportWrite；
// ctx 取消触发 bestEffortCancel 发送 notifications/cancelled（best-effort）。
func (c *Client) request(ctx context.Context, method string, params, out any) error {
	rawParams, err := marshalParams(params)
	if err != nil {
		return err
	}
	call := &pendingCall{ch: make(chan clientResponse, 1)}
	c.pendingMu.Lock()
	if c.closedErr != nil {
		er := c.closedErr
		c.pendingMu.Unlock()
		return er
	}
	if c.nextID == math.MaxUint64 || len(c.pending) >= maxPendingRequests {
		c.pendingMu.Unlock()
		c.fail(ErrMCPProtocolError)
		return ErrMCPProtocolError
	}
	c.nextID++
	id := c.nextID
	c.issuedHighWater = id
	c.pending[id] = call
	c.pendingMu.Unlock()
	defer func() {
		c.retire(id, call)
	}()

	if err := c.transport.Send(ctx, &Message{
		JSONRPC: "2.0",
		ID:      json.RawMessage(strconv.FormatUint(id, 10)),
		Method:  method,
		Params:  rawParams,
	}); err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		writeErr := fmt.Errorf("%w: %v", ErrMCPTransportWrite, err)
		c.fail(writeErr)
		return writeErr
	}
	select {
	case response := <-call.ch:
		if response.err != nil {
			return response.err
		}
		if response.msg.Error != nil {
			return mapRPCError(response.msg.Error)
		}
		if len(response.msg.Result) == 0 {
			return fmt.Errorf("%w: empty result", ErrMCPProtocolError)
		}
		if err := json.Unmarshal(response.msg.Result, out); err != nil {
			c.fail(ErrMCPProtocolError)
			return ErrMCPProtocolError
		}
		return nil
	case <-ctx.Done():
		c.bestEffortCancel(id, context.Cause(ctx))
		return context.Cause(ctx)
	}
}

// mapRPCError 将上游 JSON-RPC error code 映射到稳定的 Yaa! sentinel
// （docs/mcp/errors.md §2）。
// -32602 → ErrMCPInvalidParams；
// server-defined -32000..-32099 与其他未知 code → ErrMCPToolExecFailed；
// 不假设 -32001 固定表示过载。
func mapRPCError(err *RPCError) error {
	if err == nil {
		return nil
	}
	switch err.Code {
	case -32602:
		return ErrMCPInvalidParams
	case -32601:
		return ErrMCPToolNotFound // Method not found 对 tools/* 仅本地 catalog 查找用
	default:
		return fmt.Errorf("%w: code=%d", ErrMCPToolExecFailed, err.Code)
	}
}

// fail 是该代.Client 的"一次性失败"路径（docs/mcp/client.md §1）。
// failOnce 保证恰好一次：记 closedErr、摘所有 pending 投递错误、置 status=Error、
// 取消 conn ctx、关闭 transport；若 closing 标记则记录 transport close error。
// 关闭 done channel 通知 Manager。
func (c *Client) fail(err error) {
	c.failOnce.Do(func() {
		c.pendingMu.Lock()
		c.closedErr = err
		calls := c.pending
		c.pending = make(map[uint64]*pendingCall)
		c.pendingMu.Unlock()

		for _, call := range calls {
			// 容量 1：非阻塞投递不会因消费者已退出而阻塞；
			// 消费者退出路径都会从 pending map 摘除自己后等待 ch，
			// 但若 fail 摘除后 call 已 retire，ch 投递的接收方 select
			// 已读出 retire 已收。安全投递。
			call.ch <- clientResponse{err: err}
		}

		c.mu.Lock()
		c.status = StatusError
		closing := c.closing
		if c.cancel != nil {
			c.cancel()
		}
		c.mu.Unlock()

		transportErr := c.transport.Close()
		if closing {
			c.closeErr = transportErr
		}
		close(c.recvDone)
	})
}

// retire 把 call 从 pending 中摘除（若它仍是同一指针）。
// docs: 只有当 map 中仍指向同一 pendingCall 时才删除，避免 fail 摘除后误删新 entry。
func (c *Client) retire(id uint64, call *pendingCall) {
	c.pendingMu.Lock()
	if current := c.pending[id]; current == call {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}

// bestEffortCancel 发送一次 notifications/cancelled 通知（best effor），
// 最多 100ms 超时；失败不覆盖 caller cause；不进入 pending / 不重试 / 不回放。
// docs/mcp/client.md §1。
func (c *Client) bestEffortCancel(id uint64, cause error) {
	ctx, cancel := context.WithTimeout(c.runCtx, bestEffortCancelTimeout)
	defer cancel()
	// 固定 reason 文本，不注入 caller error 细节（避免把 caller cause 序列化进 wire）。
	reason := "client cancelled"
	if cause != nil {
		reason = "client cancelled: " + cause.Error()
		if len(reason) > 128 { // 上限保护，避免巨型 cause 进 wire
			reason = reason[:128]
		}
	}
	_ = c.transport.Send(ctx, &Message{
		JSONRPC: "2.0",
		Method:  "notifications/cancelled",
		Params: mustMarshalParams(map[string]any{
			"requestId": id,
			"reason":    reason,
		}),
	})
}

// mustMarshalParams 内部用：bestEffortCancel 的 params 永不会失败；
// 若 marshal 失败则只发不带 params 的 notification（best-effort 兜底）。
func mustMarshalParams(params any) json.RawMessage {
	raw, err := marshalParams(params)
	if err != nil {
		return nil
	}
	return raw
}

// Close 关闭连接：幂等。先 closing=true 防止新调用 fail 路径误判，
// 再调 fail(ErrMCPTransportClosed) 触发 teardown，等待 wg 后置 status=Dis 连接。
// docs/mcp/client.md §1 + §4。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		c.mu.Unlock()
		c.fail(ErrMCPTransportClosed)
		c.wg.Wait()
		c.mu.Lock()
		c.status = StatusDisconnected
		c.mu.Unlock()
	})
	return c.closeErr
}

// runRecvLoop 是 dispatch goroutine：唯一调用 transport.Recv。
// 用 validateEnvelope 分类；response 摘 pending 投递；request 入 control channel；
// notification 仅容忍 tools/list_changed（无声 callback），其他通知 errored 关连接。
// 退出条件：transport.Recv 返错误 / control queue 满 / 关闭被 fail 已置 closedErr。
// 注意：fail 关闭 transport 后 Recv 会返错误，loop 退出时 wg.Done。
func (c *Client) runRecvLoop(ctx context.Context) {
	defer c.wg.Done()
	defer close(c.control) // recvLoop 退出后关闭 control，controlLoop 自然退出
	for {
		msg, err := c.transport.Recv(ctx)
		if err != nil {
			// ctx.err 或 transport close：判断 failOnce 是否已触发；若未触发则 fail transport closed
			select {
			case <-c.recvDone:
				// fail 已被另一路径触发；本 loop 友善退出。
				return
			default:
			}
			c.fail(fmt.Errorf("%w: %v", ErrMCPTransportClosed, err))
			return
		}
		kind, verr := validateEnvelope(msg)
		if verr != nil {
			c.fail(verr)
			return
		}
		switch kind {
		case kindResponse:
			c.dispatchResponse(msg)
		case kindRequest:
			select {
			case c.control <- msg:
			default:
				// control queue 满（docs：control queue 满 → fail）
				c.fail(fmt.Errorf("%w: control queue full", ErrMCPProtocolError))
				return
			}
		case kindNotification:
			// 仅容忍 notifications/tools/list_changed; 其他通知按协议错误 fail.
			// 收到 notifications/tools/list_changed 时非阻塞调用 onListChanged (docs/mcp/client.md §recvLoop):
			//   - 回调已设: 由 Manager 投递到该代独有的 cap-1 合并 channel (旧代不复用, 避免迟到 callback).
			//   - 回调未设 (nil): 客忍无声 (manager 未接入 listChanged 路径的 client).
			if msg.Method != "notifications/tools/list_changed" {
				c.fail(fmt.Errorf("%w: unsupported notification %s", ErrMCPProtocolError, msg.Method))
				return
			}
			if c.onListChanged != nil {
				c.onListChanged()
			}
		}
	}
}

// dispatchResponse 处理 wire response：解析正 uint64 ID，摘 pending 投递。
// docs recvLoop 规则：
//   - id 合法且匹配 pending：投递 consumer
//   - 未匹配但 id <= issuedHighWater：late/duplicate，丢弃 + 计数，不毒化连接
//   - id==0 / 格式非法 / id > issuedHighWater：协议错误 fail
func (c *Client) dispatchResponse(msg *Message) {
	id, ok := parseID(msg.ID)
	if !ok || id == 0 {
		c.fail(fmt.Errorf("%w: response id invalid", ErrMCPProtocolError))
		return
	}
	issued := func() uint64 {
		c.pendingMu.Lock()
		defer c.pendingMu.Unlock()
		return c.issuedHighWater
	}()
	if id > issued {
		// id 越过签发水位：协议错误
		c.fail(fmt.Errorf("%w: response id %d > issuedHighWater %d", ErrMCPProtocolError, id, issued))
		return
	}
	c.pendingMu.Lock()
	call, found := c.pending[id]
	c.pendingMu.Unlock()
	if !found {
		// late / duplicate：丢弃；不毒化连接（docs recvLoop）。
		return
	}
	call.ch <- clientResponse{msg: msg}
}

// runControlLoop 回应 server 端 request：ping 返 ping 响应，
// 其他返 -32601 method not found。
// docs: Server request 入 control channel 容量 32；
// 不分发至业务 request pending map（不混入 Client 业务）。
func (c *Client) runControlLoop(ctx context.Context) {
	defer c.wg.Done()
	for msg := range c.control {
		kind, verr := validateEnvelope(msg)
		if verr != nil || kind != kindRequest {
			continue // dispatch 已分类过；忽略二次验证异常
		}
		resp := c.handleServerRequest(ctx, msg)
		if resp == nil {
			continue
		}
		_ = c.transport.Send(ctx, resp)
	}
}

// handleServerRequest 生成 server request 的响应。docs: Client 只处理 ping，其他返 -32601。
func (c *Client) handleServerRequest(ctx context.Context, msg *Message) *Message {
	if msg.Method != "ping" {
		return &Message{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &RPCError{Code: -32601, Message: "method not found"},
		}
	}
	// ping 响应：空 result（docs Initialize/handshake 不要求 server·info payload）
	return &Message{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Result:  json.RawMessage(`{}`),
	}
}

// notify 发送 notification（无 ID，没有 response）。
// docs Initialize: 握手完成发 notifications/initialized；v1 仅 Initialize 路径用。
func (c *Client) notify(ctx context.Context, method string, params any) error {
	rawParams, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.transport.Send(ctx, &Message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	})
}

// Initialize 完成 MCP 握手（docs/mcp/client.md §2）。
// 请求 initialize → 校验 ProtocolVersion → 校验 capabilities 含 tools → 发 initialized 通知。
// 任一步失败返错误；调用方收到错误应 Close 该 Client。
func (c *Client) Initialize(ctx context.Context) error {
	var result InitializeResult
	err := c.request(ctx, "initialize", InitializeParams{
		ProtocolVersion: preferredVersion(c.transport.Info().Type),
		Capabilities:    map[string]any{},
		ClientInfo:      Implementation{Name: "yaa", Version: runtimeVersion},
	}, &result)
	if err != nil {
		return err
	}
	if !acceptsVersion(c.transport.Info().Type, result.ProtocolVersion) {
		return fmt.Errorf("%w: server selected %s", ErrMCPProtocolError, result.ProtocolVersion)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		return fmt.Errorf("%w: server does not advertise tools", ErrMCPProtocolError)
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}
		return err
	}
	c.mu.Lock()
	c.protocolVersion = result.ProtocolVersion // 协商后定格；transport.info 决定客户端候选
	c.status = StatusConnected
	c.mu.Unlock()
	return nil
}

// Ping 心跳检测（docs/mcp/client.md §2）。只验证当前代连接可用性，
// 不启动重连；Manager 使用独立 heartbeat deadline 调用并决定是否换 Client。
func (c *Client) Ping(ctx context.Context) error {
	var result struct{}
	return c.request(ctx, "ping", nil, &result)
}

// DiscoverTools 从空 cursor 完整分页获取并规范化 Tool catalog
// （docs/mcp/client.md §3）。
// 上限保护：128 pages / 4096 tools / 4KiB cursor；重复 cursor / 重复 name / wire shape 非法 → fail(ErrMCPProtocolError)。
// 返回 normalized Tool list 按 name 升序排序。
func (c *Client) DiscoverTools(ctx context.Context) ([]MCPTool, error) {
	var all []MCPTool
	cursor := ""
	seenCursors := map[string]struct{}{"": {}}
	names := make(map[string]struct{})
	for pageNo := 0; pageNo < maxListToolsPages; pageNo++ {
		page, err := c.listTools(ctx, cursor)
		if err != nil {
			return nil, err
		}
		if len(page.Tools) > maxTotalTools-len(all) {
			c.fail(ErrMCPProtocolError)
			return nil, ErrMCPProtocolError
		}
		for _, candidate := range page.Tools {
			normalized, err := normalizeTool(c.name, candidate)
			if err != nil {
				c.fail(ErrMCPProtocolError)
				return nil, ErrMCPProtocolError
			}
			if _, duplicate := names[normalized.Name]; duplicate {
				c.fail(ErrMCPProtocolError)
				return nil, ErrMCPProtocolError
			}
			names[normalized.Name] = struct{}{}
			all = append(all, normalized)
		}
		if page.NextCursor == "" {
			sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
			return all, nil
		}
		if len(page.NextCursor) > maxCursorBytes {
			c.fail(ErrMCPProtocolError)
			return nil, ErrMCPProtocolError
		}
		if _, repeated := seenCursors[page.NextCursor]; repeated {
			c.fail(ErrMCPProtocolError)
			return nil, ErrMCPProtocolError
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
	c.fail(ErrMCPProtocolError)
	return nil, ErrMCPProtocolError
}

// listTools 单次 tools/list：strict 解码 wire DTO。
// docs: result 必须含非 null tools array；nextCursor 只能省略或 string，
// null 或其他类型拒绝；只允许这两字段 + EOF 检查。
func (c *Client) listTools(ctx context.Context, cursor string) (*ListToolsResult, error) {
	params := ListToolsParams{Cursor: cursor}
	var result ListToolsResult
	if err := c.request(ctx, "tools/list", params, &result); err != nil {
		return nil, err
	}
	// 严格校验：options v1 用 sentinel 做注释 reference；后续 commit 可加原始 wire strictness
	//   - result.Marshal 后 UseNumber + EOF 已经在 request Unmarshal 做
	//   - tools 字段在 v1 视为 required: 实际 wire 已通过 json.Unmarshal 校验类型，
	//     若 server 返 tools:null 则 Tools 为 nil；这里视 nil 为协议错误（docs: 必须 array）
	if result.Tools == nil {
		c.fail(ErrMCPProtocolError)
		return nil, ErrMCPProtocolError
	}
	return &result, nil
}

// normalizeTool 规范化上游 Tool，校验大小并构造 canonical 名 mcp.<server>.<remote>。
// docs/mcp/client.md §3：远端 name 1..128 UTF-8 不含控制字符；description ≤ 4 KiB；
// inputSchema ≤ 256 KiB JSON；canonical 名 ≤ 256 bytes、无控制字符。
// 失败返 ErrMCPProtocolError 包装错误，调用方负责 fail 关连接。
func normalizeTool(serverName string, raw MCPTool) (MCPTool, error) {
	if len(raw.Name) == 0 || len(raw.Name) > maxToolNameBytes {
		return MCPTool{}, fmt.Errorf("%w: tool name length", ErrMCPProtocolError)
	}
	if !isValidName(raw.Name) {
		return MCPTool{}, fmt.Errorf("%w: tool name contains control chars", ErrMCPProtocolError)
	}
	if len(raw.Description) > maxDescriptionBytes {
		return MCPTool{}, fmt.Errorf("%w: description too long", ErrMCPProtocolError)
	}
	if len(raw.InputSchema) > maxInputSchemaBytes {
		return MCPTool{}, fmt.Errorf("%w: input schema too long", ErrMCPProtocolError)
	}
	canonical := canonicalToolName(serverName, raw.Name)
	if len(canonical) > maxCanonicalNameBytes || !isValidName(canonical) {
		return MCPTool{}, fmt.Errorf("%w: canonical name invalid", ErrMCPProtocolError)
	}
	return MCPTool{
		Name:        canonical,
		Description: raw.Description,
		InputSchema: append(json.RawMessage(nil), raw.InputSchema...),
	}, nil
}

// canonicalToolName 生成 mcp.<server>.<remote> 形式的工具名，供 Yaa! ToolManager 注册。
func canonicalToolName(serverName, remoteName string) string {
	return "mcp." + serverName + "." + remoteName
}

// isValidName 校验字符串 1..N bytes、合法 UTF-8、不含控制字符。
// 用于 serverName + remoteName 规范化。
func isValidName(s string) bool {
	if s == "" {
		return false
	}
	// 任何 byte < 0x20 或 0x7f 视作控制字符（ASCII 片段）
	for _, b := range []byte(s) {
		if b < 0x20 || b == 0x7f {
			return false
		}
	}
	return true
}

// CallTool 调用上游 Server 端 Tool（docs/mcp/client.md §4）。
// 状态校验：未 Connected 返 ErrMCPUnavailable。
// 业务失败（result.isError=true）由 caller 处理；error 仅在 wire 失败时返。
// ctx 取消返 context.Cause(ctx)，不重映射 DeadlineExceeded 为 ErrMCPToolTimeout（hard cap 由 Proxy 设置）。
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*CallToolResult, error) {
	if c.Status() != StatusConnected {
		return nil, ErrMCPUnavailable
	}
	var result CallToolResult
	err := c.request(ctx, "tools/call", CallToolParams{
		Name:      name,
		Arguments: arguments,
	}, &result)
	if err != nil {
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		return nil, err
	}
	return &result, nil
}
