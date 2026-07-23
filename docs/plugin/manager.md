# Plugin Manager

> 文档路径: `docs/plugin/manager.md`
> 上级: [README.md](README.md)
> 依赖: [loader.md](loader.md)、[config-ref.md](config-ref.md)

---

## 1. 职责与状态

Manager 负责发现结果、依赖图、启用决策、启动顺序、Proxy 注册、健康检查、进程监控和关闭。单个 Plugin 失败进入启动报告并继续处理其他 Plugin；若最终 Agent/Skill binding 引用其缺失 Tool，Runtime 不进入 Ready。

```go
type PluginState string

const (
    StateDiscovered PluginState = "discovered"
    StateStarting   PluginState = "starting"
    StateReady      PluginState = "ready"
    StateError      PluginState = "error"
    StateStopped    PluginState = "stopped"
)

type Entry struct {
    Descriptor PluginDescriptor
    Client    *RPCClient
    Handle    *ProxyHandle
    ProxyNames []string
    State     PluginState
    Health    HealthStatus
    Config    map[string]any
    Enabled   *bool
    StartedAt time.Time
    LastError error
}

type Manager struct {
    loader               *Loader
    tools                *tool.Manager
    entries              map[string]*Entry
    discoveryDiagnostics []error
    config               config.PluginsConfig
    runCtx               context.Context
    runCancel            context.CancelFunc
    stopping             atomic.Bool
    lifecycleMu          sync.Mutex // 关闭启动 gate，禁止 wg.Add 与 wg.Wait 竞态
    mu                    sync.RWMutex
    logger                *slog.Logger
    wg                    sync.WaitGroup
    stopOnce              sync.Once
    stopDone              chan struct{}
    stopErr               error
}

type ProxyHandle struct { client atomic.Pointer[RPCClient] }

func (h *ProxyHandle) Load() (*RPCClient, error) {
    client := h.client.Load()
    if client == nil {
        return nil, ErrPluginUnavailable
    }
    return client, nil
}

func (h *ProxyHandle) Store(client *RPCClient) { h.client.Store(client) }

func (h *ProxyHandle) Invalidate(client *RPCClient) bool {
    return h.client.CompareAndSwap(client, nil)
}

func clonePluginConfig(src map[string]any) map[string]any {
    if src == nil {
        return map[string]any{}
    }
    dst := make(map[string]any, len(src))
    for key, value := range src {
        dst[key] = clonePluginConfigValue(value)
    }
    return dst
}

func clonePluginConfigValue(value any) any {
    switch value := value.(type) {
    case map[string]any:
        return clonePluginConfig(value)
    case []any:
        cloned := make([]any, len(value))
        for i, item := range value {
            cloned[i] = clonePluginConfigValue(item)
        }
        return cloned
    default:
        return value // nil、string、bool 和数值均为 JSON scalar。
    }
}

func NewManager(
    ctx context.Context,
    cfg config.PluginsConfig,
    loader *Loader,
    tools *tool.Manager,
    logger *slog.Logger,
) (*Manager, error) {
    if ctx == nil || loader == nil || tools == nil || logger == nil {
        return nil, errors.New("plugin manager: nil dependency")
    }
    runCtx, cancel := context.WithCancel(ctx)
    m := &Manager{
        loader: loader, tools: tools, config: cfg,
        entries: make(map[string]*Entry),
        runCtx: runCtx, runCancel: cancel,
        logger: logger, stopDone: make(chan struct{}),
    }

    descriptors, diagnostics := loader.Discover()
    for _, d := range descriptors {
        m.entries[d.Manifest.ID] = &Entry{
            Descriptor: d, State: StateDiscovered,
            Health: HealthStatus{Level: "unknown"},
            Config: map[string]any{},
        }
    }
    for _, diagnostic := range diagnostics {
        m.discoveryDiagnostics = append(m.discoveryDiagnostics, diagnostic)
        if diagnostic.PluginID == "" {
            continue
        }
        e := m.entries[diagnostic.PluginID]
        if e == nil {
            e = &Entry{
                State: StateError, Health: HealthStatus{Level: "unknown"},
                Config: map[string]any{},
            }
            if diagnostic.Descriptor != nil {
                e.Descriptor = *diagnostic.Descriptor
            }
            m.entries[diagnostic.PluginID] = e
        }
        e.State = StateError
        e.LastError = errors.Join(e.LastError, diagnostic)
    }
    for _, configured := range cfg.Entries {
        e := m.entries[configured.ID]
        if e == nil {
            err := fmt.Errorf("%w: %s", ErrPluginNotFound, configured.ID)
            m.discoveryDiagnostics = append(m.discoveryDiagnostics, err)
            e = &Entry{
                State: StateError, LastError: err,
                Health: HealthStatus{Level: "unknown"}, Config: map[string]any{},
            }
            m.entries[configured.ID] = e
        }
        e.Enabled = configured.Enabled
        e.Config = clonePluginConfig(configured.Config)
    }
    return m, nil
}
```

`NewManager` 把每个规范化的 `PluginDescriptor` 原样冻结在对应 Entry，再合并 `plugins.entries[]` 的 enabled/config；后续启动和重启始终复用该 Descriptor，不能从可变路径重新解析。`Descriptor.Manifest` 是唯一 Manifest 来源。Config Loader 必须先把配置归一化为 `map[string]any`、`[]any` 和 JSON scalar；`clonePluginConfig` 再递归复制所有 map/slice，避免调用方修改嵌套值，此后 Descriptor/Config/Enabled 均不可变。该实现只使用 Go 1.20 标准库。空 ID discovery diagnostic 只进入 StartupReport；带 ID diagnostic 和显式配置引用的缺失 ID 都建立 `error` Entry。所有该 Plugin 的 Proxy 共享启动时创建的同一个 `ProxyHandle`；handle 在运行期退出和重启期间保留，`Load` 对 nil 返回 `ErrPluginUnavailable`。`mu` 保护 Entry 的 Client/Handle/ProxyNames/State/Health/StartedAt/LastError；任何 RPC、进程等待或退避都不得持有该锁。

## 2. 依赖图

```go
type Dependency struct {
    ID       string `yaml:"id"`
    Version  string `yaml:"version"`
    Optional bool   `yaml:"optional"`
}
```

Manager 在启动任何进程前完成 ID 唯一性、缺失依赖、SemVer range 和循环检查，再做稳定拓扑排序。Optional dependency 缺失只记 WARN；存在但版本不匹配仍记 WARN 并按“不可用”处理。

启动某 Entry 前，所有非 optional dependency 必须是 `ready`。依赖被禁用或启动失败时，下游 Entry 进入 `error`，错误包含完整依赖链，不再尝试启动。

## 3. 启动

```go
type StartupReport struct {
    Diagnostics []error
    FailedIDs   []string
}

func (m *Manager) StartAll() StartupReport {
    report := StartupReport{Diagnostics: append([]error(nil), m.discoveryDiagnostics...)}
    finish := func() StartupReport {
        sort.Strings(report.FailedIDs)
        unique := report.FailedIDs[:0]
        for _, id := range report.FailedIDs {
            if len(unique) == 0 || unique[len(unique)-1] != id {
                unique = append(unique, id)
            }
        }
        report.FailedIDs = unique
        return report
    }
    m.lifecycleMu.Lock()
    if m.stopping.Load() {
        m.lifecycleMu.Unlock()
        report.Diagnostics = append(report.Diagnostics, context.Canceled)
        return finish()
    }
    m.wg.Add(1) // Stop 的 gate 关闭前登记整个启动流程。
    m.lifecycleMu.Unlock()
    defer m.wg.Done()

    order, errs := m.resolveDependencies()
    report.Diagnostics = append(report.Diagnostics, errs...)

    m.mu.RLock()
    for id, e := range m.entries {
        if e.State == StateError {
            report.FailedIDs = append(report.FailedIDs, id)
        }
    }
    m.mu.RUnlock()

    if !m.config.AutoStart {
        m.mu.Lock()
        for _, e := range m.entries {
            if e.State == StateDiscovered {
                e.State = StateStopped
            }
        }
        m.mu.Unlock()
        return finish()
    }

    for _, id := range order {
        e := m.entries[id]
        m.mu.RLock()
        state := e.State
        m.mu.RUnlock()
        if state != StateDiscovered {
            continue
        }
        if !effectiveEnabled(e) {
            m.mu.Lock()
            e.State = StateStopped
            m.mu.Unlock()
            continue
        }
        if err := m.requireReadyDependencies(e); err != nil {
            m.fail(e, err)
            report.Diagnostics = append(report.Diagnostics, err)
            report.FailedIDs = append(report.FailedIDs, id)
            continue
        }

        m.mu.Lock()
        e.State = StateStarting
        m.mu.Unlock()
        startCtx, cancel := context.WithTimeout(m.runCtx, m.config.StartupTimeout)
        client, err := m.loader.Start(startCtx, e.Descriptor, e.Config)
        cancel()
        if err != nil {
            m.fail(e, err)
            report.Diagnostics = append(report.Diagnostics, err)
            report.FailedIDs = append(report.FailedIDs, id)
            continue
        }

        handle, names, rollback, err := m.registerProxies(e, client)
        if err != nil {
            rollback()
            _ = client.Terminate()
            failure := fmt.Errorf("%w: %v", ErrPluginCapabilityConflict, err)
            m.fail(e, failure)
            report.Diagnostics = append(report.Diagnostics, failure)
            report.FailedIDs = append(report.FailedIDs, id)
            continue
        }

        m.lifecycleMu.Lock()
        if m.stopping.Load() {
            m.lifecycleMu.Unlock()
            rollback()
            _ = client.Terminate()
            break
        }
        m.mu.Lock()
        e.Client, e.Handle, e.ProxyNames = client, handle, names
        e.StartedAt = time.Now()
        e.State = StateReady // Proxy 全部注册成功后才 Ready
        handle.Store(client) // Entry 发布完成后才开放调用。
        m.wg.Add(1)          // lifecycleMu 保证 Stop 关闭 gate 后不再 Add。
        m.mu.Unlock()
        m.lifecycleMu.Unlock()
        go m.monitor(e)
    }
    return finish()
}

func effectiveEnabled(e *Entry) bool {
    if e.Enabled != nil {
        return *e.Enabled
    }
    return e.Descriptor.Manifest.DefaultEnabled
}

func (m *Manager) registerProxies(e *Entry, client *RPCClient) (
    handle *ProxyHandle,
    names []string,
    rollback func(),
    err error,
) {
    handle = &ProxyHandle{}
    rollback = func() {
        handle.Store(nil)
        for i := len(names) - 1; i >= 0; i-- {
            _ = m.tools.Unregister(names[i])
        }
    }
    for _, capability := range client.Capabilities {
        if capability.Type != "tool" {
            return handle, names, rollback, ErrPluginProtocolIncompatible
        }
        proxy, err := NewPluginToolProxy(e.Descriptor.Manifest.ID, capability, handle)
        if err != nil {
            return handle, names, rollback, err
        }
        if err := m.tools.Register(proxy, config.ToolConfig{Enabled: true}, "plugin"); err != nil {
            return handle, names, rollback, err
        }
        names = append(names, capability.Name)
    }
    return handle, names, rollback, nil
}
```

`registerProxies` 必须事务化：返回成功前任何部分注册失败都注销本次已经注册的 Proxy，并终止、Wait、清理刚启动的进程。新 Proxy 的 handle 初始为 nil；只有 StartAll 在 `lifecycleMu` gate 内发布完整 Entry 后才 `Store(client)`，因此 Stop 与启动并发时不会短暂暴露未受 Manager 管理的 Client。`PluginToolProxy` 位于 `internal/plugin` 并实现 `tool.Tool`，避免 `internal/tool` 反向导入 Plugin 形成 import cycle。`m.fail` 在 `mu` 下更新 State/LastError，`requireReadyDependencies` 在 `mu.RLock` 下读取依赖状态。StartAll 只处理仍为 `StateDiscovered` 的 Entry，已有 discovery/DAG error 不会被覆盖；所有返回路径都对累积的 `FailedIDs` 去重排序。StartupReport 是 non-fatal diagnostics；Runtime 在随后唯一的 `Config.Activate(binding)` 中决定缺失 capability 是否阻止 Ready。初始 `StartAll` 对每个 Plugin 只尝试一次，`restart.*` 不能用于 Dial/Handshake/Init/Ready 的启动失败。

## 4. 进程退出与重启

Runtime 尚未进入 Stop 时，无论 exit code 是否为 0，Plugin 退出都视为 unexpected。唯一 monitor 等待 `RPCClient.Exited` 关闭并读取 `WaitErr()`，先将 `ProxyHandle` 原子置空，使调用立即返回 `ErrPluginUnavailable`，再关闭旧 RPC transport、清理旧 endpoint并清空 Entry client。随后才按 `restart.enabled/max_attempts/backoff` 重启。每次重启生成新 nonce，重新执行 Handshake/Init/Ready，并要求 capabilities 与首次注册的 type/name/description/schema 集合精确相等；不相等则终止新进程并计为一次失败。成功后原子替换 handle 中的 Client，现有 Proxy 恢复服务。

monitor 的唯一循环同时 select 当前 `Exited`、health ticker 和 `runCtx.Done()`，并在开始退避、调用 Loader 前、以及发布新 Client 前检查停止状态。Loader 启动使用 `context.WithTimeout(m.runCtx, startup_timeout)`；发布时先持有 `lifecycleMu` 再持有 `mu`，仅当 `stopping=false` 且 Entry 仍指向本轮预期状态时才能写入 `e.Client` 和 `handle.Store(newClient)`。检查失败时在锁外立即 `newClient.Terminate()`。这样 Stop 一旦在 `lifecycleMu` 下关闭 gate，任何已在退避、Dial 或 Ready 阶段的重启都不能再发布进程。RPC、退避、Wait 和 cleanup 全部在锁外执行。

重启窗口并非透明：在途请求失败，不自动 replay；依赖该 Plugin 的其他 Plugin 不级联重启。达到上限后 Entry 保持 `error`，Proxy 继续返回 unavailable，直到 Runtime 重启。每个 restart client 都只有一个 Wait goroutine；monitor 循环切换到新的 `Exited` channel，Manager 不直接等待 OS process handle。StopAll 设置 `stopping` 后，monitor 从 `runCtx.Done()` 退出，资源清理由 StopAll 的幂等方法统一完成。

Health RPC 使用 `health_timeout`。monitor 在 `mu` 下更新 Entry.Health snapshot；单次失败只把健康级别更新为 degraded，不 Kill/重启进程，只有实际进程退出触发自动重启。

## 5. 关闭

```go
func (m *Manager) StopAll(ctx context.Context) error {
    m.stopOnce.Do(func() {
        // 先同步关闭启动/发布 gate 和所有 Proxy，再让 teardown 收拢资源。
        m.lifecycleMu.Lock()
        m.stopping.Store(true)
        m.mu.Lock()
        for _, e := range m.entries {
            if e.Handle != nil {
                e.Handle.Store(nil)
            }
        }
        m.mu.Unlock()
        m.runCancel()
        m.lifecycleMu.Unlock()

        go func() {
            m.stopErr = m.teardown()
            close(m.stopDone)
        }()
    })

    select {
    case <-m.stopDone:
        return m.stopErr
    case <-ctx.Done():
        return context.Cause(ctx)
    }
}

func (m *Manager) Done() <-chan struct{} { return m.stopDone }

func (m *Manager) WaitStopped() error {
    <-m.stopDone
    return m.stopErr
}
```

```text
Runtime.Stop
  → stopping=true
  → reverse startup order
  → set ProxyHandle unavailable
  → RPC Stop with stop_timeout
  → wait process; timeout Kill+Wait
  → unregister proxies
  → cleanup endpoint
  → state=stopped
```

关闭期的进程退出不触发重启。所有 Kill 后都必须 Wait，所有 endpoint 都必须 cleanup。

`teardown` 先 `m.wg.Wait()`，确保 StartAll、monitor、health 和 restart goroutine 全部离开且不再写 Entry；Proxy 已在 gate 关闭时同步 unavailable。随后按逆拓扑处理每个 Entry，在 `mu` 下取走 Client/ProxyNames，再在锁外为该 Client 创建一个独立 `stop_timeout` deadline：若进程尚未退出，先调用 RPC Stop，再用同一个剩余预算等待 `Exited`；预算耗尽则调用 `KillAndWait`。是否调用 Stop 只由进程是否已经退出决定，不能由 handle 是否 nil 决定。最后无条件读取 `WaitErr`、关闭 transport、注销全部 Proxy、cleanup endpoint并在 `mu` 下标记 stopped。某一步失败不能跳过后续 Plugin 或 cleanup，最终用 `errors.Join` 返回全部错误。

调用方 `ctx` 只限制本次 `StopAll` 等待时间；到期后后台 teardown 继续运行，后续 `StopAll` 调用等待同一个 `stopDone` 并取得同一个最终错误。Runtime 即使收到 `StopAll` 的 deadline error，也必须在关闭 Tool Manager 和退出主进程前等待 `Done()`，再用 `WaitStopped()` 取得最终聚合错误；因此 teardown 不会越过 Plugin → Tool 的 owner 顺序，也不会因 Go 主进程退出而遗留子进程。`Done/WaitStopped` 只能在已经调用 `StopAll` 后等待。

## 6. 对外边界

v1 Remote API 索引没有 Plugin 管理端点，也没有 `plugin_list/plugin_health` Tool schema，因此本模块只承诺 Runtime health、结构化日志和指标。未来增加任何端点或 Tool 时，先在 Remote API/Tool 文档登记请求、响应、权限和错误语义。

---

*最后更新: 2025-07-17*
