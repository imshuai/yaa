# Plugin Loader

> 文档路径: `docs/plugin/loader.md`
> 上级: [README.md](README.md)

---

## 1. 职责

Loader 发现 Manifest、校验文件和配置、分配本地 IPC、启动进程并完成一次 RPC 启动序列。它不解析依赖图、不注册 Proxy、不执行重试，也不加载 Go symbol；这些分别由 Manager 和 Runtime 模块负责。

```text
Discover
  → read plugin.yaml
  → validate canonical Manifest
  → resolve entry inside plugin directory

Start
  → validate entry config against config_schema
  → allocate Unix Socket / loopback TCP
  → exec local process
  → Dial
  → Handshake
  → Init(config)
  → Ready(capabilities)
```

## 2. 路径与发现

`plugins.paths` 的相对路径以主配置文件目录为基准。每个直接子目录最多读取一个 `plugin.yaml`。Loader 对搜索目录、Manifest 目录和 entry 分别执行 `filepath.Abs`、`filepath.EvalSymlinks` 与 `os.Lstat`，要求 entry 是普通文件（Unix 还要求任一 execute bit）；随后以解析后的真实 Manifest 目录为 base 重新计算 `filepath.Rel`。只有 `rel == ".."`、`strings.HasPrefix(rel, ".."+string(filepath.Separator))` 或 `filepath.IsAbs(rel)` 才是逃逸；文件名 `..helper` 不是逃逸。这样目录内 symlink 也不能指向目录外可执行文件。

```go
type PluginDescriptor struct {
    ManifestPath string
    EntryPath    string
    Manifest     Manifest
}

type DiscoveryDiagnostic struct {
    PluginID   string            // 无法恢复 ID 时为空
    Descriptor *PluginDescriptor // 已解析出 ID 时保留部分 Descriptor
    Err        error             // 始终非 nil
}

func (d DiscoveryDiagnostic) Error() string { return d.Err.Error() }
func (d DiscoveryDiagnostic) Unwrap() error { return d.Err }

type Loader struct {
    paths           []string
    protocolVersion string // "1"
    logger          *slog.Logger
}

func NewLoader(configDir string, paths []string, logger *slog.Logger) (*Loader, error)

// pluginRPC 是 pkg/pluginrpc 对生成 gRPC client 的最小生命周期适配器。
// Loader/Manager 不直接依赖 grpc.ClientConn；Close 由 RPCClient 统一拥有。
type pluginRPC interface {
    Handshake(ctx context.Context, protocolVersion, expectedPluginID string) (HandshakeResponse, error)
    Init(ctx context.Context, cfg map[string]any) error
    Ready(ctx context.Context) (ReadyResponse, error)
    Health(ctx context.Context) (HealthResponse, error)
    Stop(ctx context.Context) error
    InvokeTool(ctx context.Context, req ToolRequest) (ToolResponse, error)
    Close() error
}

// RPCClient 同时拥有 RPC transport、已启动进程和 endpoint cleanup。
// cmd.Start 成功后由它启动唯一 Wait goroutine；其他模块不得调用 Cmd.Wait。
type RPCClient struct {
    rpc          pluginRPC
    cmd          *exec.Cmd
    Exited       <-chan struct{}
    Capabilities []CapabilityDescriptor
    cleanup      func()

    waitErr      error
    closeOnce    sync.Once
    closeErr     error
    cleanupOnce  sync.Once
}

func (c *RPCClient) WaitErr() error {
    <-c.Exited
    return c.waitErr
}

func (c *RPCClient) CloseTransport() error {
    c.closeOnce.Do(func() {
        if c.rpc != nil {
            c.closeErr = c.rpc.Close()
        }
    })
    return c.closeErr
}

func (c *RPCClient) Health(ctx context.Context) (HealthResponse, error) {
    return c.rpc.Health(ctx)
}

func (c *RPCClient) Stop(ctx context.Context) error {
    return c.rpc.Stop(ctx)
}

func (c *RPCClient) InvokeTool(ctx context.Context, req ToolRequest) (ToolResponse, error) {
    return c.rpc.InvokeTool(ctx, req)
}

func (c *RPCClient) CleanupEndpoint() {
    c.cleanupOnce.Do(func() {
        if c.cleanup != nil {
            c.cleanup()
        }
    })
}

func (c *RPCClient) KillAndWait() error {
    var killErr error
    if c.cmd != nil && c.cmd.Process != nil {
        if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
            killErr = err
        }
    }
    <-c.Exited
    return killErr
}

func (c *RPCClient) Terminate() error {
    transportErr := c.CloseTransport()
    killErr := c.KillAndWait()
    c.CleanupEndpoint()
    return errors.Join(transportErr, killErr)
}

func (l *Loader) Discover() (descriptors []PluginDescriptor, diagnostics []DiscoveryDiagnostic)
func (l *Loader) Start(ctx context.Context, d PluginDescriptor, cfg map[string]any) (*RPCClient, error)
```

`NewLoader` 以主配置目录解析、去重 `plugins.paths` 并固定 RPC major；nil logger、空配置目录或无法规范化的搜索路径直接返回错误。`Discover` 只返回完整且 ID 唯一的 Descriptor。无法解析出 ID 的 Manifest 错误使用空 `PluginID` diagnostic，不产生 Entry；已经解析出 ID 后发现 entry、版本或 config 错误时，diagnostic 同时携带 ID 和部分 Descriptor，Manager 为该 ID 建立 `error` Entry。重复 ID 的所有 Descriptor 都从成功结果移除，并各自产生同 ID diagnostic，不能由路径顺序决定胜者。

## 3. 启动

```go
func (l *Loader) Start(ctx context.Context, d PluginDescriptor, cfg map[string]any) (*RPCClient, error) {
    if err := validateDescriptor(d, l.protocolVersion); err != nil {
        return nil, err
    }
    if err := validateJSONSchema(d.Manifest.ConfigSchema, cfg); err != nil {
        return nil, fmt.Errorf("%w: %v", ErrPluginConfigInvalid, err)
    }

    endpoint, cleanup, err := allocateLocalEndpoint(d.Manifest.ID)
    if err != nil {
        return nil, fmt.Errorf("allocate plugin endpoint: %w", err)
    }
    nonce, err := newStartupNonce(32) // crypto/rand + base64.RawURLEncoding
    if err != nil {
        cleanup()
        return nil, fmt.Errorf("generate plugin nonce: %w", err)
    }
    if ctx.Err() != nil {
        cleanup()
        return nil, context.Cause(ctx)
    }

    // startup ctx 只约束启动协议；长期进程不能绑定 CommandContext。
    cmd := exec.Command(d.EntryPath, "--yaa-plugin-endpoint", endpoint)
    cmd.Dir = filepath.Dir(d.EntryPath)
    cmd.Env = append(filteredPluginEnv(), "YAA_PLUGIN_STARTUP_NONCE="+nonce)
    if err := cmd.Start(); err != nil {
        cleanup()
        return nil, fmt.Errorf("%w: %v", ErrPluginProcessStart, err)
    }

    exited := make(chan struct{})
    client := &RPCClient{cmd: cmd, Exited: exited, cleanup: cleanup}
    go func() {
        client.waitErr = cmd.Wait() // cmd.Start 成功后唯一的 Wait owner。
        close(exited)
    }()

    rpc, err := DialPlugin(ctx, endpoint)
    if err != nil {
        _ = client.Terminate()
        return nil, fmt.Errorf("%w: %v", ErrPluginConnectionTimeout, err)
    }
    client.rpc = rpc

    fail := func(err error) (*RPCClient, error) {
        _ = client.Terminate()
        return nil, err
    }
    hello, err := rpc.Handshake(ctx, "1", d.Manifest.ID)
    if err != nil {
        return fail(err)
    }
    if hello.PluginID != d.Manifest.ID || hello.PluginVersion != d.Manifest.Version ||
        hello.ProtocolVersion != "1" ||
        subtle.ConstantTimeCompare([]byte(hello.StartupNonce), []byte(nonce)) != 1 {
        return fail(ErrPluginProtocolIncompatible)
    }
    if err := rpc.Init(ctx, cfg); err != nil {
        return fail(fmt.Errorf("%w: %v", ErrPluginInitFailed, err))
    }
    ready, err := rpc.Ready(ctx)
    if err != nil {
        return fail(fmt.Errorf("%w: %v", ErrPluginInitFailed, err))
    }
    if err := matchCapabilities(d.Manifest.Provides, ready.Capabilities); err != nil {
        return fail(err)
    }
    client.Capabilities = ready.Capabilities
    return client, nil
}
```

调用方用 `plugins.startup_timeout` 创建 ctx；该 ctx 只覆盖 exec、Dial、Handshake、Init 和 Ready，返回成功后 cancel 不得终止进程。`cmd.Start` 成功后立即创建 `RPCClient` 并启动唯一 `cmd.Wait` goroutine；因此 Dial/Handshake/Init/Ready 任一失败都只调用 `Terminate()`，不得自行 Kill/Wait。`Terminate` 先幂等关闭 transport，再 Kill（已退出视为成功）、等待 `Exited`，最后 cleanup endpoint。成功路径只启动一个 Wait goroutine，并在写入 `waitErr` 后关闭只读 `Exited <-chan struct{}`；多个 waiter 可等待 channel close，再调用 `WaitErr()` 读取同一结果，Manager 不得再次调用 Wait。底层 `pluginRPC`、`cmd` 和 cleanup 都是私有字段；Loader 只在 Client 尚未发布的构造阶段借用刚注入的 `pluginRPC` 完成 Handshake/Init/Ready，发布后 Manager 和 Proxy 只能使用 `RPCClient` 的转发及幂等生命周期方法，不能绕过 `CloseTransport`。

## 4. 平台

| 平台 | endpoint |
|------|----------|
| Linux/macOS | 临时目录中的 Unix Socket，目录/文件仅当前用户可访问 |
| Windows 7 | 随机 loopback TCP 端口；32-byte startup nonce 只经子进程环境传入并在 HandshakeResponse 回显验证 |

每次启动和重启都生成新 nonce，Unix 也执行同一验证；nonce 不进入参数、配置、错误或日志。MVP 只允许 Runtime 自己启动的本机进程。远程 TCP、TLS、签名信任库和下载/安装协议不在 v1 范围；需要时先新增配置和威胁模型。

## 5. 版本校验

- `protocol_version` 必须精确等于 RPC major `"1"`。
- `version` 与 `requires_runtime` 使用 SemVer parser，不用字符串比较。
- Dependency range 在 Manager 拓扑排序前校验。
- Manifest 和 Ready capabilities 必须集合相等，type/name/description/schema 不得漂移。

---

*最后更新: 2025-07-17*
