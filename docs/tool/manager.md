# Tool Manager

> 上级: [Tool 系统设计](README.md)

---

## 1. 职责

Tool Manager 是 Tool 的唯一注册、发现、鉴权和执行入口。它负责：

- 保存 builtin、plugin、MCP Tool 及 canonical `config.ToolConfig`；
- 按 `AgentConfig.Tools []string` 过滤能力；
- 校验 JSON Schema、应用 timeout、有限重试和结果上限；
- 在 Runtime 全局和 Session 两层限制并发；
- 将当前 Agent 可用 Tool 与 Session 历史冻结为 Provider wire 投影。

Manager 不从 HTTP identity 推导 Agent，不执行 Skill，也不提供 Remote 直接执行端点。

---

## 2. 类型与 API

```go
type ExecutionScope struct {
    AgentID   string
    SessionID string // MCP 等非 Session 调用为空
}

type Manager struct {
    tools         map[string]Tool
    configs       map[string]config.ToolConfig
    reload        *config.ReloadManager
    providers     *provider.Manager
    agentBindings map[string]agentToolBinding
    globalGate    chan struct{}

    mu           sync.RWMutex
    sessionMu    sync.Mutex
    sessionGates map[string]*sessionGate
    logger       *slog.Logger
}

type agentToolBinding struct {
    AllowAll  bool
    Allowed   map[string]struct{}
    Overrides map[string]agentToolOverride
}

// Agent override 没有 Enabled；nil 表示字段未出现。
type agentToolOverride struct {
    Timeout *time.Duration
    Options map[string]any // nil=未出现；非 nil 时按 key 覆盖 root options
}

type EffectiveToolConfig struct {
    Timeout  time.Duration
    MaxRetry int
    Options  map[string]any
}

// RetryableError 是 Tool 对“尚未产生副作用且可安全重试”的显式 opt-in。
type RetryableError interface {
    error
    Retryable() bool
}

func NewManager(
    reload *config.ReloadManager,
    providers *provider.Manager,
    logger *slog.Logger,
) (*Manager, error)

func (m *Manager) Register(tool Tool, cfg config.ToolConfig, source string) error
func (m *Manager) Unregister(name string) error
func (m *Manager) Get(name string) (Tool, error)
func (m *Manager) List() []ToolInfo
func (m *Manager) ListForAgent(agentID string) []ToolInfo
func (m *Manager) CheckPermission(agentID, toolName string) bool
func (m *Manager) ToToolDefs(agentID string, history []provider.Message) (*ProviderToolProjection, error)
func (m *Manager) Execute(ctx context.Context, scope ExecutionScope, toolName string, params map[string]any) (ToolResult, error)
func (m *Manager) ExecuteBatch(ctx context.Context, scope ExecutionScope, calls []provider.ToolCall) ([]ToolResult, error)
```

`NewManager` 拒绝 nil 依赖，从 `reload.Current()` 深拷贝每个 Agent 的 restart-required `tools` allowlist 与已经严格解码的 `tools_config` 到 `agentBindings`，并按初始 `ToolsConfig` 创建两个 gate。Agent override 解码后只能产生 presence-aware `Timeout` 和 `Options`；出现 `enabled` 或其他 key 是配置错误，不能复用含 `Enabled` 的 root `config.ToolConfig`。Provider 集合由 Provider Manager 持有，不由 Tool Manager 关闭。`AgentID` 永远必填；空值或未知 Agent 返回 `ErrPermissionDenied`。Agent turn 必须传真实 `SessionID`。MCP Expose Server 使用配置的 `mcp.server.agent_id`，并把 `SessionID` 留空，因此只使用 Runtime 全局 gate。

### 2.1 ToolInfo

```go
type ToolInfo struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
    Enabled     bool            `json:"enabled"`
    Source      string          `json:"source"` // builtin | plugin | mcp
}
```

`List` 包含 disabled Tool，按 `Name` 升序返回深拷贝。`ListForAgent` 只包含 enabled 且授权的 Tool。暂时 unavailable 的 Plugin/MCP 稳定 Proxy 仍可出现在定义中，执行时返回对应 unavailable sentinel。

### 2.2 ProviderToolProjection

`ToToolDefs` 返回一次 turn 使用的不可变 `ProviderToolProjection`，而不是调用方可修改的裸 `[]provider.ToolDef`。它只把当前 enabled、authorized Tool 放入 definitions，同时把历史 canonical Tool name 纳入 `canonical -> alias` 映射和全局碰撞检查；历史专用名称不进入 executable 反查表。具体算法、深拷贝字段、`specific` ToolChoice 和 direct/stream 反查见 [Provider-safe Tool alias 契约](provider.md)。

Runtime 启动 binding 校验必须对每个 Agent 的当前 definitions 执行一次空历史投影；每次 turn 仍用当前 Session snapshot 重新构造并冻结投影。`ExecuteBatch` 不接受 wire alias，只接受 Agent 已完整反查为 canonical name 的 calls。

---

## 3. 注册与 enabled

启动顺序固定为 builtin -> plugin proxy -> MCP proxy。`Register`：

1. 校验 canonical name 是合法 UTF-8、1..256 bytes 且无 Unicode 控制字符，并校验 source、description 和 Parameters JSON Schema；
2. 拒绝重复 name；
3. 深拷贝 schema、配置和 options；
4. 无论 `cfg.Enabled` 为何都写入注册表。

保留 disabled 条目使 Config、`ToolInfo` 和 `ErrToolDisabled` 的语义一致。`Unregister` 只用于 Plugin/MCP Manager 永久停止后的 catalog 清理；暂时断线只把稳定 Proxy 置 unavailable，builtin 不在运行时注销。

所有来源共用同一 canonical name 规则；不能为了某个 Provider 把点号、Unicode 或数字开头的名称改写后注册。Provider-safe 限制只在 turn-local wire 投影中处理。重复 canonical name 继续由 `Register` 拒绝；不同 canonical name 的 alias 碰撞由启动 binding 与 `ToToolDefs` 返回 `ErrToolAliasCollision`。

`resolveConfig` 每次从同一个 `reload.Current()` snapshot 读取 root hot 字段，并与冻结的 Agent override 按以下顺序合并：

```text
tools.default_timeout
  <- tools.builtin.<name>.timeout
  <- agents[].tools_config.<name>.timeout
```

Options 同样按 map key 覆盖；nil Agent `Options` 表示未出现，非 nil 空 map 表示没有新增 key，不会清空 root map。最终 timeout 必须位于 `0..tools.max_timeout`；越界配置在 binding 阶段拒绝。`Enabled` 只来自 root Tool 配置，Agent override 无权改变；`MaxRetry` 只来自 `tools.default_max_retry`，不在 ToolConfig/Agent override 重复声明。结果限长从该 snapshot 找到同一 Agent 当前的 Provider ID/Model，经 `providers.Get` 获得 estimator；找不到 Agent/Provider 或估算失败都返回硬错误，不注入未受限 Content。一次 Execute 不再读取第二个 snapshot。

---

## 4. 权限

Canonical 配置只有：

```yaml
agents:
  - id: safe-agent
    tools: [http, file_read]
  - id: full-access-agent
    tools: []
```

规则：

- `tools: []` 或省略表示允许所有已注册 Tool；
- 非空数组是精确 name allowlist；
- 不存在 `allow`/`deny` object shape；
- 未注册或 disabled Tool 永远不可执行；
- `CheckPermission` 只回答 Agent allowlist，调用方仍需检查存在与 enabled。

Tool name 来自模型，不能作为 principal。所有调用都必须携带 Runtime 已解析的真实 `AgentID`。

---

## 5. 并发 gate

`tools.max_concurrent` 是整个 Runtime 同时运行的 Tool 上限；`tools.max_concurrent_per_session` 是同一非空 Session 的子上限。两者在 Manager 构造时创建，变更需要重启。

```go
type sessionGate struct {
    sem  chan struct{}
    refs int
}
```

acquire 顺序为 Session gate -> global gate，release 逆序。创建/引用 Session gate 时在 `sessionMu` 下增加 refs；release 后减少 refs，降为 0 时从 map 删除，避免每个历史 Session 永久占用内存。等待任一 gate 都使用：

```go
select {
case sem <- struct{}{}:
case <-ctx.Done():
    return nil, context.Cause(ctx)
}
```

不得在持有 `mu` 或 `sessionMu` 时等待 channel。gate 等待必须保留 caller 设置的 cancel cause（例如 Agent Stop 或 Runtime shutdown），不能收窄为 `ctx.Err()`。空 `SessionID` 跳过 Session gate，但仍获取 global gate。

---

## 6. Execute

单次执行顺序固定为：

1. 校验 `scope.AgentID`；查找 Tool。
2. 检查 config enabled；disabled 返回 `ErrToolDisabled`。
3. 检查 Agent allowlist；拒绝返回 `ErrPermissionDenied`。
4. 用注册时保存的 JSON Schema 校验 params；失败返回 `ErrInvalidParams`。
5. 解析一个 `EffectiveToolConfig` snapshot。
6. 获取 Session/global gate；等待可由 caller context 取消。
7. 以 Go 1.20 的 `context.WithCancelCause(ctx)` 和 `time.AfterFunc(effective.Timeout, ...)` 创建一次带 `ErrToolTimeout` cause 的 `callCtx`；Tool 调用、退避和全部重试共用它。
8. 每次 Tool 返回或退避结束后先检查 caller `ctx`，再检查 child `callCtx`；只有两者都有效时，才用 `var retryable RetryableError; errors.As(err, &retryable)` 判定是否重试。
9. 使用 Agent Provider 的 token estimator 将 `Content` 限制到 `max_result_tokens`。
10. release gate，返回结果并记录不含 params/content 的结构化日志。

核心错误优先级可直接实现为：

```go
callCtx, cancel := context.WithCancelCause(ctx)
timer := time.AfterFunc(effective.Timeout, func() {
    cancel(ErrToolTimeout)
})
defer func() {
    timer.Stop()
    cancel(nil)
}()

result, err := tool.Execute(callCtx, scope, params)
if ctx.Err() != nil {
    return ToolResult{}, context.Cause(ctx)
}
if callCtx.Err() != nil {
    return ToolResult{}, context.Cause(callCtx)
}

var retryable RetryableError
if err != nil && errors.As(err, &retryable) && retryable.Retryable() {
    // 在同一个 callCtx 内等待退避并开始下一 attempt。
}
```

caller cancel/deadline 已发生时始终返回 `context.Cause(ctx)`；只有 caller 仍有效而 Manager timer 先触发时，`context.Cause(callCtx)` 才是稳定的 `ErrToolTimeout`。不能把 caller deadline、Agent Stop 或 Runtime shutdown 改写为 Tool timeout。每次重试和可取消退避后重复同一顺序；cleanup 必须先 `timer.Stop()` 再 `cancel(nil)`，且只能在读取两个 cause 之后执行，不能先 cancel 再把人为产生的 `context.Canceled` 当成执行失败。

Go 1.20 没有 `context.WithTimeoutCause`（该 API 从 Go 1.21 才提供），因此不能在目标 toolchain 下引用它。上述 `WithCancelCause` + `time.AfterFunc` 是 v1 的唯一兼容实现；timer callback 与 cleanup 的并发取消依赖 Context 的 first-cause-wins 语义。

返回 `Retryable()==true` 即由 Tool 保证本次尚未产生外部副作用；Manager 不自行推断。`errors.As` 的 target 必须是指向接口变量的 `&retryable`，不能写成不可编译的 `*RetryableError`。参数错误、权限错误、取消、timeout、`ToolResult{IsError:true}` 和可能已经产生副作用的错误不重试。MCP Proxy 一旦成功发送 `tools/call`，其后发生的断线、timeout 或结果不确定错误都必须分类为不可重试，避免 Manager 在外层重放远端副作用。Manager 不按错误字符串或 `net.Error.Temporary` 猜测 retryability。

---

## 7. ExecuteBatch

调用前置条件是所有 `calls[i].Function.Name` 已由 Agent 使用当前 turn 的冻结投影精确反查为 canonical name。Batch 不认识 Provider alias，也不尝试 hash、猜测或反查；该边界避免 alias 被传入 MCP `remoteName` 或持久化链路。

Batch 保持输入顺序：`results[i]` 永远对应 `calls[i]`。实现最多启动
`min(len(calls), tools.max_concurrent, tools.max_concurrent_per_session)` 个 worker；空 Session 不应用最后一个上限。worker 从共享原子 index 取下一项并调用同一个 `Execute`，不得绕过 gate、权限或 timeout。

每个 call 的 `Function.Arguments` 必须严格解码为一个 JSON object，拒绝 trailing token。参数/Tool 执行错误通过 `tool.ErrorResult` 转换为该 call 的 `ToolResult{IsError:true}`，让 Agent 仍可组成完整 Tool unit。worker 返回时先检查共享 caller `ctx.Err()`：只有它非 nil 才使 Batch 等待全部 worker 后返回 `context.Cause(ctx)`；Tool 自己的子 context timeout/cancel 而 caller 仍有效时只生成安全单项结果。

Batch 不另加 retry loop，结果顺序不受完成顺序影响。

---

## 8. 配置更新边界

- timeout、options、`default_max_retry`、`max_result_tokens` 在下一次调用读取新 snapshot；
- enabled、Tool 集合、`max_concurrent`、`max_concurrent_per_session` 需要重启；
- 运行中的调用固定使用开始时的 effective snapshot；
- Agent `tools`/`tools_config` 结构变更需要重启。
