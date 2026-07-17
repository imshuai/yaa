# Plugin 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/plugin/`
> 依赖: `docs/architecture.md` §3.10 (架构位置), §8 (接口兼容性)

---

## 1. 概述

### 1.1 什么是 Plugin

Plugin 是 Yaa! 中**最灵活的扩展机制**，位于架构中与 Tool、Skill、MCP 并列的核心层。

| 层级 | 抽象 | 类比 |
|------|------|------|
| Tool | 原子操作（Shell、HTTP、File） | 系统调用 |
| Skill | 多步骤工作流 + Tool 编排 | 应用程序 |
| MCP | 标准化协议接入外部能力 | 网络服务 |
| **Plugin** | **运行时可加载的自定义模块** | **浏览器扩展** |

Plugin 系统允许第三方开发者**不修改 Yaa! 核心代码**的前提下：
- 注册新的 Tool
- 注册新的 Skill
- 实现自定义 Memory Backend
- 注入自定义中间件 / Hook

### 1.2 设计理念

| 特性 | 说明 |
|------|------|
| **接口驱动** | Plugin 实现预定义 Go interface，Yaa! 通过接口调用 |
| **动态加载** | Runtime 启动时自动扫描，支持运行时热加载 |
| **命名隔离** | 每个 Plugin 有唯一 ID，避免符号冲突 |
| **安全沙箱** | Plugin 运行在受限上下文中，可配置权限 |
| **生命周期管理** | 初始化 → 启用 → 禁用 → 卸载，全程受控 |

### 1.3 核心原则

1. **Interface First** — Plugin 实现固定接口，Yaa! 不依赖具体实现
2. **Config over Code** — 插件行为通过配置控制，无需重新编译
3. **Graceful Degradation** — 插件加载失败不影响 Runtime 启动
4. **Backward Compatible** — 接口变更遵循 Go interface 兼容规则（优先新增方法，不修改现有方法）

---

## 2. 核心接口

### 2.1 Plugin 接口

所有插件必须实现 `Plugin` 接口：

```go
// Plugin 是所有 Yaa! 插件的根接口
type Plugin interface {
    // ID 返回插件唯一标识（小写中划线，如 "my-tool-pack"）
    ID() string

    // Info 返回插件元信息
    Info() PluginInfo

    // Init 在插件加载时调用，接收运行时上下文与配置
    Init(ctx context.Context, rt RuntimeAccessor, cfg PluginConfig) error

    // Start 启用插件，注册其提供的能力
    Start(ctx context.Context) error

    // Stop 禁用插件，释放已注册的能力
    Stop(ctx context.Context) error
}

// PluginInfo 描述插件元信息
type PluginInfo struct {
    Name        string
    Version     string
    Description string
    Author      string
}

// PluginConfig 是传递给插件的配置（来自 yaa.yaml 的 plugins 段）
type PluginConfig map[string]any

// RuntimeAccessor 让插件安全访问运行时能力
type RuntimeAccessor interface {
    RegisterTool(tool tool.Tool) error
    RegisterSkill(skill skill.Skill) error
    SetMemoryBackend(backend memory.Memory) error
    Logger() *slog.Logger
    Storage() storage.Storage
}
```

### 2.2 Plugin Manager

Manager 负责插件的集中管理：

```go
// Manager 管理所有已加载的插件
type Manager struct {
    loader  *Loader
    plugins map[string]*entry  // plugin ID → entry
    mu      sync.RWMutex
    log     *slog.Logger
}

type entry struct {
    plugin  Plugin
    state   PluginState  // Loaded → Started → Stopped
    config  PluginConfig
}

// Register 注册插件实例
func (m *Manager) Register(p Plugin, cfg PluginConfig) error

// LoadAll 从配置批量加载所有插件
func (m *Manager) LoadAll(ctx context.Context) error

// StartAll 启动所有已加载的插件
func (m *Manager) StartAll(ctx context.Context) error

// StopAll 停止所有运行中的插件
func (m *Manager) StopAll(ctx context.Context) error

// Get 按 ID 获取插件
func (m *Manager) Get(id string) (Plugin, error)

// List 列出所有插件及其状态
func (m *Manager) List() []PluginStatus
```

### 2.3 Plugin Loader

Loader 负责从文件系统发现并实例化插件：

```go
// Loader 从指定路径加载插件
type Loader struct {
    paths  []string          // 插件搜索路径
    log    *slog.Logger
}

// Discover 扫描所有路径，返回发现的插件描述
func (l *Loader) Discover() ([]PluginDescriptor, error)

// Load 加载单个插件并返回实例
func (l *Loader) Load(desc PluginDescriptor) (Plugin, error)

// PluginDescriptor 描述磁盘上的一个插件
type PluginDescriptor struct {
    Path    string   // 插件目录或 .so 文件路径
    Type    PluginType  // native / wasm / process
}
```

**加载策略：**

```text
yaa.yaml: plugins.paths → 扫描目录
  │
  ├── *.so / *.dll  → Go Plugin（CGO-free，标准 plugin 包）
  ├── */plugin.yaml → 声明式插件（配置驱动，无需编译）
  └── */main.go    → 源码插件（需要编译工具链）
  │
  ▼
Loader.Discover() → []PluginDescriptor
Loader.Load()     → Plugin 实例
Manager.Register() → 进入管理
```

---

## 3. 插件类型

Plugin 通过 `RuntimeAccessor` 向 Runtime 注册不同类型的能力：

| 插件类型 | 注册方法 | 用途 | 示例 |
|----------|----------|------|------|
| **Tool Provider** | `RegisterTool()` | 向 Runtime 注入自定义 Tool | 数据库查询、第三方 API 封装 |
| **Skill Provider** | `RegisterSkill()` | 向 Runtime 注入自定义 Skill | 复杂工作流包 |
| **Memory Backend** | `SetMemoryBackend()` | 替换默认 Memory 实现 | Redis 向量存储、PostgreSQL |
| **Custom** | 直接访问 `Storage()` / `Logger()` | 自定义 Hook、中间件 | 请求日志、审计、指标采集 |

### 3.1 Tool Provider 示例

```go
type DBQueryPlugin struct {
    log *slog.Logger
}

func (p *DBQueryPlugin) ID() string { return "db-query" }
func (p *DBQueryPlugin) Info() PluginInfo {
    return PluginInfo{
        Name:    "DB Query",
        Version: "1.0.0",
        Author:  "iDodev",
    }
}

func (p *DBQueryPlugin) Init(ctx context.Context, rt RuntimeAccessor, cfg PluginConfig) error {
    p.log = rt.Logger()
    return nil
}

func (p *DBQueryPlugin) Start(ctx context.Context) error {
    // 注册自定义 Tool
    return nil // rt.RegisterTool(&DBQueryTool{...})
}

func (p *DBQueryPlugin) Stop(ctx context.Context) error {
    p.log.Info("db-query plugin stopped")
    return nil
}
```

### 3.2 Memory Backend 示例

```go
type RedisMemoryPlugin struct{}

func (p *RedisMemoryPlugin) Start(ctx context.Context) error {
    // rt 和 cfg 在 Init() 中已保存
    backend := NewRedisMemory(p.redisAddr) // 实现 memory.Memory 接口
    return p.rt.SetMemoryBackend(backend)
}
```

---

## 4. 配置

```yaml
# yaa.yaml
plugins:
  paths:
    - "./plugins"
    - "/etc/yaa/plugins"
  auto_start: true
  entries:
    - id: "db-query"
      enabled: true
      config:
        connection_string: "${DB_URL}"
    - id: "redis-memory"
      enabled: false
      config:
        redis_addr: "localhost:6379"
```

| 配置项 | 说明 |
|--------|------|
| `plugins.paths` | 插件搜索目录列表 |
| `plugins.auto_start` | 加载后是否自动 Start |
| `plugins.entries[].id` | 插件 ID |
| `plugins.entries[].enabled` | 是否启用 |
| `plugins.entries[].config` | 传递给 `Init()` 的配置 |

---

## 5. 生命周期

```text
Discovered → Loaded → Initialized → Started ⇄ Stopped → Unloaded
     │          │          │            │          │
     │          │          │            │          └─ Manager.Remove()
     │          │          │            └─ Manager.Stop() / StopAll()
     │          │          └─ Plugin.Init()
     │          └─ Loader.Load()
     └─ Loader.Discover()
```

**错误处理：**
- 单个插件加载/启动失败 → 记录错误日志，跳过该插件，Runtime 继续启动
- `StopAll()` 在 Runtime 优雅关闭时调用，确保资源释放
- 插件 Panic 会被 Manager 捕获（recover），避免崩溃整个 Runtime

---

## 6. 接口兼容性

遵循 `architecture.md` §8 的兼容性规则：

| 变更类型 | 策略 |
|----------|------|
| 新增方法 | ✅ 直接在接口上新增（旧插件不实现新方法时使用默认适配层） |
| 修改方法签名 | ❌ 禁止，新建 `PluginV2` 接口 |
| 删除方法 | ❌ 禁止 |
| 新增接口 | ✅ 如 `ToolProvider`、`SkillProvider` 等细分接口 |

**适配层模式：**

```go
// 当 Plugin 接口新增方法时，提供默认实现
type PluginV2 interface {
    Plugin
    HealthCheck() error
}

// 适配旧插件
type pluginAdapter struct {
    Plugin
}

func (a *pluginAdapter) HealthCheck() error {
    return nil // 旧插件默认健康
}
```

---

## 7. 与其他模块的关系

| 模块 | 关系 |
|------|------|
| **Tool** | Plugin 可注册 Tool（Tool Provider） |
| **Skill** | Plugin 可注册 Skill（Skill Provider） |
| **Memory** | Plugin 可替换 Memory Backend |
| **MCP** | 互补：MCP 接入外部标准协议，Plugin 注入本地自定义能力 |
| **Config** | Plugin 配置在 `plugins` 段统一管理 |
| **Storage** | Plugin 可通过 `RuntimeAccessor.Storage()` 访问持久化 |

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [interface.md](interface.md) | Plugin 接口详解 — `Plugin`、`PluginInfo`、`RuntimeAccessor`、插件类型 |
| [manager.md](manager.md) | Plugin Manager — 注册、加载、启停、状态查询 |
| [loader.md](loader.md) | Plugin Loader — 路径扫描、发现、实例化、加载策略 |
| [integration.md](integration.md) | 与各模块的集成 — Tool / Skill / Memory / Config / Storage |
| [config-ref.md](config-ref.md) | 配置参考 — 全局配置、插件级配置、示例 |
| [errors.md](errors.md) | 错误处理 — 加载失败、启动失败、Panic 恢复 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策 + 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-17*
