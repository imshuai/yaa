# Plugin Manager 详解

> Plugin Manager 负责插件的加载、依赖解析、生命周期管理、启用/禁用及隔离。
> Plugin 是 Yaa! 扩展能力的核心机制，允许在不修改 Runtime 主体的前提下增加 Tool、Provider、Skill 等能力。

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| **热插拔** | Runtime 运行期间可动态加载、卸载插件，无需重启 |
| **依赖安全** | 自动解析插件依赖关系，检测循环依赖与缺失依赖 |
| **生命周期可控** | 每个插件拥有独立的状态机，支持优雅启停 |
| **隔离性** | 插件之间资源隔离，单个插件崩溃不影响 Runtime |
| **配置驱动** | 插件通过 YAML 配置声明，遵循 Config over Code 原则 |
| **向后兼容** | 插件接口变更遵循 Go interface 兼容规则，提供适配层 |

---

## 2. 核心接口

```go
// Plugin 是所有插件必须实现的核心接口
type Plugin interface {
    // Manifest 返回插件元信息
    Manifest() Manifest
    // Init 在插件加载时调用，接收 Runtime 注入的上下文
    Init(ctx *PluginContext) error
    // Start 启动插件资源（如连接池、后台协程）
    Start() error
    // Stop 停止插件，释放资源
    Stop() error
    // Provide 返回插件向 Runtime 注入的能力扩展点
    Provide() Extensions
}

// Manifest 描述插件元信息
type Manifest struct {
    ID          string   // 唯一标识，如 "weather-provider"
    Name        string   // 人类可读名称
    Version     string   // 语义化版本号
    Description string
    Author      string
    Dependencies []Dependency // 依赖的其他插件或 Runtime 版本
    Category    Category      // Tool / Provider / Skill / Hook
}

// Dependency 描述插件依赖
type Dependency struct {
    ID      string // 插件 ID 或 "runtime"
    Version string // 版本约束，如 ">=1.2.0"
}

// Extensions 是插件可提供的扩展点集合
type Extensions struct {
    Tools    []tool.Tool
    Providers []provider.Provider
    Skills   []skill.Skill
    Hooks    []Hook
}

// PluginContext 是 Runtime 注入给插件的运行上下文
type PluginContext struct {
    Config  *config.Config
    Storage storage.Storage
    Logger  *slog.Logger
}
```

---

## 3. 插件加载流程

```text
Runtime 启动
  │
  ▼
扫描插件目录 (./plugins/*.so 或 ./plugins/*/plugin.yaml)
  │
  ├─ 内置插件: Go interface 直接注册
  ├─ 外部插件: 使用 Go plugin 包加载 .so 文件
  └─ 声明式插件: 解析 YAML，通过配置注册 Tool/Provider
  │
  ▼
解析每个插件的 Manifest
  │
  ▼
依赖拓扑排序（检测循环依赖 → 报错退出）
  │
  ▼
按排序顺序逐个执行：
  1. Init(ctx)    — 注入运行上下文
  2. Start()      — 启动资源
  3. Provide()    — 注册扩展能力到对应 Registry
  │
  ▼
所有插件就绪，Runtime 标记为 Ready
```

---

## 4. 依赖解析

Plugin Manager 使用 **有向无环图（DAG）** 进行拓扑排序，确保被依赖的插件先加载。

```go
// Resolver 依赖解析器
type Resolver struct {
    plugins map[string]*node // 插件依赖图
}

type node struct {
    manifest  Manifest
    dependsOn []*node
    state     PluginState
}

// Resolve 执行拓扑排序，返回加载顺序
func (r *Resolver) Resolve() ([]string, error) {
    var order []string
    visited := make(map[string]bool)
    visiting := make(map[string]bool) // 用于检测循环依赖

    var visit func(n *node) error
    visit = func(n *node) error {
        if visited[n.manifest.ID] {
            return nil
        }
        if visiting[n.manifest.ID] {
            return fmt.Errorf("检测到循环依赖: 插件 %s", n.manifest.ID)
        }
        visiting[n.manifest.ID] = true

        for _, dep := range n.dependsOn {
            if dep == nil {
                return fmt.Errorf("缺少依赖: %s 依赖的插件不存在", n.manifest.ID)
            }
            if err := visit(dep); err != nil {
                return err
            }
        }

        visiting[n.manifest.ID] = false
        visited[n.manifest.ID] = true
        order = append(order, n.manifest.ID)
        return nil
    }

    for _, n := range r.plugins {
        if err := visit(n); err != nil {
            return nil, err
        }
    }
    return order, nil
}
```

| 场景 | 处理方式 |
|------|----------|
| 缺少依赖 | 启动阶段直接报错，列出缺失的插件 ID |
| 循环依赖 | 启动阶段检测到后报错，拒绝启动 |
| 版本不兼容 | 对比 Manifest.Version 约束，不满足则跳过并告警 |
| 可选依赖 | 标注 `optional: true` 时，缺失不影响加载 |

---

## 5. 生命周期管理

每个插件拥有独立的状态机：

```text
Discovered → Loaded → Initialized → Started → (Paused ↔ Started) → Stopped → Unloaded
```

| 状态 | 说明 | 触发条件 |
|------|------|----------|
| Discovered | 插件被发现但未加载 | 扫描目录完成 |
| Loaded | 插件代码/配置已加载 | 文件解析成功 |
| Initialized | Init() 执行成功 | 依赖注入完成 |
| Started | Start() 执行成功 | 资源就绪 |
| Paused | 临时暂停，保留状态 | 手动暂停或资源不足 |
| Stopped | Stop() 执行成功 | 手动停止或 Runtime 关闭 |
| Unloaded | 资源完全释放 | 卸载插件 |

```go
// Manager 管理所有插件的生命周期
type Manager struct {
    plugins map[string]*PluginEntry
    mu      sync.RWMutex
    resolver *Resolver
    logger  *slog.Logger
}

type PluginEntry struct {
    Plugin    Plugin
    State     PluginState
    Config    *PluginConfig
    LoadedAt  time.Time
}

// Load 加载单个插件
func (m *Manager) Load(p Plugin) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    manifest := p.Manifest()
    if _, exists := m.plugins[manifest.ID]; exists {
        return fmt.Errorf("插件 %s 已存在", manifest.ID)
    }

    ctx := &PluginContext{
        Config:  m.config,
        Storage: m.storage,
        Logger:  m.logger.With("plugin", manifest.ID),
    }

    if err := p.Init(ctx); err != nil {
        return fmt.Errorf("插件 %s 初始化失败: %w", manifest.ID, err)
    }
    if err := p.Start(); err != nil {
        return fmt.Errorf("插件 %s 启动失败: %w", manifest.ID, err)
    }

    m.plugins[manifest.ID] = &PluginEntry{
        Plugin:   p,
        State:    StateStarted,
        LoadedAt: time.Now(),
    }

    // 将插件提供的能力注册到对应 Registry
    exts := p.Provide()
    m.registerExtensions(exts)

    m.logger.Info("插件加载成功", "id", manifest.ID, "version", manifest.Version)
    return nil
}
```

---

## 6. 启用 / 禁用

Plugin Manager 支持运行时动态启用和禁用插件，无需重启 Runtime。

```go
// Enable 启用已注册但被禁用的插件
func (m *Manager) Enable(id string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    entry, ok := m.plugins[id]
    if !ok {
        return fmt.Errorf("插件 %s 未注册", id)
    }
    if entry.State == StateStarted {
        return nil // 已经在运行
    }

    if err := entry.Plugin.Start(); err != nil {
        return err
    }
    entry.State = StateStarted
    m.registerExtensions(entry.Plugin.Provide())
    return nil
}

// Disable 禁用插件，停止其提供的所有能力
func (m *Manager) Disable(id string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    entry, ok := m.plugins[id]
    if !ok {
        return fmt.Errorf("插件 %s 未注册", id)
    }

    // 检查是否有其他插件依赖它
    for _, other := range m.plugins {
        for _, dep := range other.Plugin.Manifest().Dependencies {
            if dep.ID == id {
                return fmt.Errorf("无法禁用 %s: 插件 %s 依赖它", id, other.Plugin.Manifest().ID)
            }
        }
    }

    if err := entry.Plugin.Stop(); err != nil {
        return err
    }
    entry.State = StateStopped
    m.unregisterExtensions(entry.Plugin.Provide())
    return nil
}
```

---

## 7. 插件隔离

| 隔离维度 | 机制 | 说明 |
|----------|------|------|
| **崩溃隔离** | panic recovery | 每个插件调用包裹 `recover()`，单插件 panic 不影响 Runtime |
| **资源隔离** | 独立 context + timeout | 每个插件操作有独立超时与取消控制 |
| **日志隔离** | 独立 Logger | 每个插件拥有带插件 ID 标签的 Logger |
| **配置隔离** | 独立配置命名空间 | 插件配置存储在 `plugins.<id>` 命名空间下 |
| **状态隔离** | 独立状态机 | 每个插件维护独立状态，互不干扰 |

```go
// safeCall 包装插件调用，实现 panic 隔离
func (m *Manager) safeCall(id string, fn func() error) (err error) {
    defer func() {
        if r := recover(); r != nil {
            m.logger.Error("插件 panic", "plugin", id, "panic", r)
            err = fmt.Errorf("插件 %s panic: %v", id, r)
        }
    }()
    return fn()
}
```

---

## 8. 插件配置

```yaml
# yaa.yaml 插件配置示例
plugins:
  - id: "weather-provider"
    enabled: true
    source: "./plugins/weather/weather.so"
    config:
      api_key: "${WEATHER_API_KEY}"
      cache_ttl: 600s

  - id: "custom-tools"
    enabled: true
    source: "builtin"       # 内置插件
    config:
      timeout: 30s
```

---

## 9. 卸载流程

```text
收到卸载请求 (API / 配置热更新)
  │
  ▼
检查是否有其他插件依赖目标插件
  │
  ├─ 有依赖 → 拒绝卸载，返回依赖列表
  └─ 无依赖 → 继续
  │
  ▼
调用 Stop() 停止插件
  │
  ▼
从各 Registry 注销插件提供的 Tool / Provider / Skill
  │
  ▼
调用 Cleanup() 释放底层资源（关闭连接、清理临时文件）
  │
  ▼
从 Manager 中移除插件条目
  │
  ▼
记录日志，通知监听者（通过 SSE/WS 事件推送）
```

---

## 10. Remote API 集成

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v1/plugins` | GET | 列出所有插件及状态 |
| `/api/v1/plugins/:id` | GET | 获取插件详情 |
| `/api/v1/plugins/:id/enable` | POST | 启用插件 |
| `/api/v1/plugins/:id/disable` | POST | 禁用插件 |
| `/api/v1/plugins/:id` | DELETE | 卸载插件 |

```go
// GET /api/v1/plugins
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
    plugins := s.pluginManager.List()
    respondJSON(w, http.StatusOK, plugins)
}

// POST /api/v1/plugins/:id/enable
func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    if err := s.pluginManager.Enable(id); err != nil {
        respondError(w, http.StatusBadRequest, err)
        return
    }
    respondJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}
```

---

## 11. 完整加载流程图

```text
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
│  扫描目录    │────▶│  解析 Manifest │────▶│  依赖解析 (DAG)  │
│  .so / .yaml │     │  版本/依赖/类型 │     │  拓扑排序        │
└─────────────┘     └──────────────┘     └────────┬────────┘
                                                 │
                                          有循环依赖? ──Yes──▶ 报错退出
                                                 │ No
                                                 ▼
                                    ┌────────────────────────┐
                                    │  按拓扑顺序逐个加载       │
                                    │                         │
                                    │  Init(ctx) ──▶ Start()  │
                                    │       │           │      │
                                    │    失败? ◀──       │      │
                                    │       │           ▼      │
                                    │   跳过并告警   Provide()  │
                                    │       │           │      │
                                    │       ▼           ▼      │
                                    │   记录日志   注册到 Registry│
                                    └────────────────────────┘
                                                 │
                                                 ▼
                                    ┌────────────────────────┐
                                    │  所有插件就绪            │
                                    │  Runtime → Ready        │
                                    │  推送事件 (SSE/WS)       │
                                    └────────────────────────┘
```

---

## 12. 事件通知

插件状态变更通过 Runtime 事件总线广播，客户端可通过 SSE/WebSocket 订阅：

| 事件 | 触发时机 | Payload |
|------|----------|---------|
| `plugin.loaded` | 插件加载完成 | `{id, version, loaded_at}` |
| `plugin.started` | 插件启动成功 | `{id}` |
| `plugin.stopped` | 插件停止 | `{id, reason}` |
| `plugin.error` | 插件发生错误 | `{id, error}` |
| `plugin.unloaded` | 插件卸载完成 | `{id}` |

---

*最后更新: 2025-07-17*
