# 配置热更新

> 文档路径: `docs/config/hot-reload.md`
> 依赖: [loading.md](loading.md)、[validation.md](validation.md)

---

## 1. 契约

热更新只应用明确列入 allowlist 的纯配置值。任何需要重新绑定端口、重建客户端、切换存储、启动进程、改变注册表或重建并发 gate 的字段都保持旧 snapshot，并在 `ReloadResult` 中标记 `restart_required`。

固定流程：

```text
file event
  -> debounce
  -> serialize reload
  -> config.Load(path, flags)
  -> validate bindings
  -> diff(old, candidate)
  -> classify allowlist/restart-required
  -> atomic.Value.Store(candidate)  (only when no restart path)
```

`config.Load` 已完成解析、迁移、环境变量展开、presence-aware 解码和基础校验；热更新不得重复实现其中任何阶段。`ReloadManager` 是 watcher 与 `config_reload` Tool 共用的唯一发布入口。

## 2. 文件监听

监听配置文件所在目录，以覆盖编辑器的临时文件 + Rename 保存方式。只处理目标路径的 `Write|Create|Rename|Remove`，300ms 防抖；文件暂时不存在时保留旧配置，下一次 Create 重新尝试。

```go
type Watcher struct {
    fs        *fsnotify.Watcher
    path      string
    debounce  time.Duration
    reload    func() (ReloadResult, error)
    onReload  func(ReloadResult)
    onError   func(error)
}

func (w *Watcher) Run(ctx context.Context) error {
    defer w.fs.Close()
    timer := time.NewTimer(time.Hour)
    if !timer.Stop() {
        <-timer.C
    }
    defer timer.Stop()
    var timerC <-chan time.Time

    for {
        select {
        case <-ctx.Done():
            return context.Cause(ctx)
        case err, ok := <-w.fs.Errors:
            if !ok {
                return nil
            }
            if err != nil && w.onError != nil {
                w.onError(fmt.Errorf("config watcher: %w", err))
            }
        case event, ok := <-w.fs.Events:
            if !ok {
                return nil
            }
            if filepath.Clean(event.Name) != w.path ||
                event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
                continue
            }
            if !timer.Stop() && timerC != nil {
                select {
                case <-timer.C:
                default:
                }
            }
            timer.Reset(w.debounce)
            timerC = timer.C
        case <-timerC:
            timerC = nil
            result, err := w.reload()
            if err != nil {
                if w.onError != nil {
                    w.onError(err)
                }
                continue
            }
            if w.onReload != nil {
                w.onReload(result)
            }
        }
    }
}
```

`Run` 对两个 fsnotify channel 都检查 `ok`，统一负责关闭 watcher 和 timer；reload 在同一个 goroutine 内执行，不会在 `Run` 返回后遗留 callback。Watcher 错误由 Runtime supervisor 记录并重建 watcher，当前 snapshot 不清空。

## 3. 原子发布

```go
var ErrConfigNotActive = errors.New("config reload manager not active")

type ReloadResult struct {
    Applied         bool     `json:"applied"`
    Changed         []string `json:"changed"`
    RestartRequired bool     `json:"restart_required"`
    Paths           []string `json:"paths"` // 仅 restart-required paths
}

type ReloadManager struct {
    path             string
    flags            map[string]any
    validateBindings func(*Config) error
    value            atomic.Value // stores *Config; snapshots are immutable
    reload           sync.Mutex   // serializes watcher/tool requests
    active           bool         // protected by reload
}

func NewReloadManager(initial *Config, path string, flags map[string]any, validateBindings func(*Config) error) (*ReloadManager, error)
func (m *ReloadManager) Activate() error
func (m *ReloadManager) Current() *Config
func (m *ReloadManager) Reload() (ReloadResult, error)
```

Runtime 只执行一次 `initial := config.Load(path, flags)`。`NewReloadManager` 拒绝 nil，并立即保存这个已经通过基础校验的不可变 snapshot，因此 bootstrap 组件可以读取 `Current()`，且不会对配置文件做第二次读取。构造后 `path`、`flags` 和 validator 不变，`flags` 的 map 在构造时深拷贝。

Provider/Tool/Plugin/MCP/Skill catalog 使用同一个 `initial` 建立后，Runtime 调用一次 `Activate()`。它在 `reload` mutex 下对当前初始 snapshot 执行 `validateBindings`，成功后设置 `active=true`；失败保持 inactive 并触发 Runtime 逆序 rollback。`Reload()` 在 active 前返回 `ErrConfigNotActive`。Watcher、`config_query`、`config_reload` 和 Remote API 只能在 Activate 成功后启动，因此未完成 binding 的 bootstrap snapshot 不会成为外部可见配置。

`Reload` 的核心语义：

1. 持有 `reload` mutex，要求 `active=true`，读取旧 snapshot。
2. `Load` 候选文件并执行 `validateBindings`。
3. 计算并按字典序排序 `Changed`。
4. 计算 restart-required paths；非空时返回 `Applied=false, RestartRequired=true, Paths=...`，不调用 `Store`，且 `error=nil`。
5. 没有 restart path 时原子 `Store` 候选，返回 `Applied=true, RestartRequired=false`。

Load、绑定校验或内部发布错误返回非 nil error，旧 snapshot 保持不变。调用方不得修改 `Current()` 返回的字段、slice 或 map；需要可变数据的模块必须复制自己的字段。

## 4. 唯一 hot-reload allowlist

数组元素必须按稳定 ID/name 匹配；新增、删除或修改 ID 一律需要重启。

| 路径 | 生效时机 |
|------|----------|
| `log.level` | 下一条日志 |
| `agents[].model` | 下一次模型请求 |
| `agents[].system_prompt` | 下一次 Context 构建 |
| `agents[].max_tokens`, `agents[].temperature` | 下一次模型请求 |
| `tools.default_timeout`, `tools.max_timeout`, `tools.default_max_retry`, `tools.max_result_tokens` | 下一次 Tool 调用 |
| `tools.builtin.<name>.timeout`, `tools.builtin.<name>.options` | 下一次对应 Tool 调用 |
| `session.max_messages`, `session.max_message_bytes`, `session.ttl`, `session.max_lifetime`, `session.persist`, `agents[].session.*` | reload 后新建的 Session |
| `session.max_sessions_per_agent` | 下一次 Create |
| `session.cleanup_interval` | 下一次 cleanup ticker 重置 |
| `context.*`, `agents[].context.*` | 下一次 Context 构建 |
| `memory.max_items`, `memory.default_ttl`, `memory.eviction_policy` | 下一次 Agent turn 或 Remote Memory 请求 |
| `memory.expire_interval`, `memory.expire_batch_size` | 下一次 cleanup worker tick |
| `agents[].memory.{max_items,default_ttl,eviction_policy}` | 下一次该 Agent turn 或 Remote Memory 请求 |

以下分组全部需要重启：

- `runtime.storage.*`、`runtime.api.*`、`runtime.auth.*`
- Provider 新增/删除及 `providers[].*`
- Agent 新增/删除、ID/provider/tools/skills、`agents[].planner.*`、`agents[].tools_config`、`agents[].skills_config`
- 根 `planner.*`、Planner Executor 的 `max_concurrent`
- `mcp.*`、`plugins.*`
- `skills.dir`、Skill 新增/删除、`skills.per_skill.<name>.enabled` 或结构/options 变化
- Tool 新增/删除、`tools.builtin.<name>.enabled`、`tools.max_concurrent`、`tools.max_concurrent_per_session`
- `memory.enabled`, `memory.storage.*`, `memory.vector.*`, `memory.embedding.*`
- `agents[].memory.enabled`, `agents[].memory.vector.*`
- `log.format`, `log.output`

若一批变更同时包含可热更新和需重启路径，整批不应用。已创建 Session 使用 snapshot 中持久化的 resolved policy；reload 不扫描、不改写现有 Session，也不改变其 TTL、max lifetime 或 persist 语义。Context/Session 的 `validateBindings` 必须在 `Store` 前完成所有 Agent 的有效配置检查。

## 5. 结果与可观测性

文件监听失败只记录结构化日志并保留旧快照。`config_reload` Tool 直接返回 `ReloadResult`；成功或 restart-required 结果只包含路径，不包含旧/新 Secret 值。失败记录 `config.reload_failed`（错误分类、路径和 request ID），不通过 Remote SSE 广播；Remote API 不提供 `/api/v1/config/reload`。

---

*最后更新: 2026-07-22*
