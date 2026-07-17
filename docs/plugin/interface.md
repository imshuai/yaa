# Plugin 接口详解

## 概述

Plugin 是 Yaa! Runtime 的扩展机制，允许第三方在不修改核心代码的情况下向 Runtime 注入新能力。Plugin 接口遵循 Go interface 兼容性规则：新增方法必须带默认实现（通过嵌入接口或默认结构体），确保已发布的插件在版本升级时不会编译失败。

## 文件结构

```
plugin/
├── plugin.go    # Plugin 接口定义与默认实现
├── manager.go   # 插件管理器（注册、生命周期、依赖解析）
└── loader.go    # 插件加载器（发现、实例化、配置注入）
```

## Plugin 接口定义

### 核心接口

```go
// Plugin 是所有插件必须实现的核心接口。
// 新版本新增方法时，通过嵌入 PluginV2 等子接口实现向后兼容。
type Plugin interface {
    // ID 返回插件的全局唯一标识符，格式为 reverse-DNS，如 "com.yaa.plugin.metrics"。
    ID() string

    // Name 返回插件的人类可读名称，如 "Metrics Collector"。
    Name() string

    // Version 返回插件语义化版本号，如 "1.2.0"。
    Version() string

    // Init 在插件实例化后、启动前调用，用于读取配置、建立连接等准备工作。
    // 返回 error 表示初始化失败，Runtime 将阻止插件启动。
    Init(ctx context.Context, cfg Config) error

    // Start 启动插件，使其进入工作状态。
    // 对于后台任务型插件，此处应启动 goroutine 并立即返回。
    Start(ctx context.Context) error

    // Stop 停止插件，释放所有资源。
    // Runtime 保证 Stop 在 Init/Start 成功后才会调用。
    // ctx 超时后 Runtime 将强制回收资源。
    Stop(ctx context.Context) error

    // Health 返回插件当前健康状态。
    // Runtime 定期调用以进行存活检测。
    Health() HealthStatus
}
```

### 健康状态

```go
// HealthStatus 描述插件的健康状况。
type HealthStatus struct {
    Status    HealthLevel // 健康等级
    Message   string      // 人类可读描述
    Timestamp time.Time   // 检测时间
}

type HealthLevel int

const (
    HealthHealthy   HealthLevel = iota // 正常运行
    HealthDegraded                     // 降级运行（部分功能不可用）
    HealthUnhealthy                    // 异常（需要重启）
    HealthUnknown                      // 未知（尚未检测）
)
```

### 接口方法一览

| 方法 | 调用时机 | 返回值说明 | 是否可阻塞 |
|------|---------|-----------|-----------|
| `ID()` | 全生命周期 | 插件唯一标识 | 否 |
| `Name()` | 全生命周期 | 可读名称 | 否 |
| `Version()` | 全生命周期 | 语义化版本 | 否 |
| `Init()` | 加载后、启动前 | error 非 nil 则阻止启动 | 是（受超时控制） |
| `Start()` | Init 成功后 | error 非 nil 则标记启动失败 | 是（受超时控制） |
| `Stop()` | 关闭/重启时 | error 非 nil 记录日志 | 是（受超时控制） |
| `Health()` | 定期巡检 | HealthStatus | 否（应快速返回） |

## 插件能力注册

Plugin 通过 `Capabilities()` 方法向 Runtime 声明自身提供的能力。Runtime 根据能力注册表将插件功能接入对应子系统。

### 能力接口

```go
// Capability 描述插件向 Runtime 注册的能力。
type Capability struct {
    Type     CapabilityType // 能力类型
    Name     string         // 能力名称（在同类中唯一）
    Instance any            // 能力实例（由 Runtime 类型断言后使用）
}

type CapabilityType string

const (
    CapToolProvider  CapabilityType = "tool_provider"  // 提供 Tool
    CapSkillProvider CapabilityType = "skill_provider" // 提供 Skill
    CapHook          CapabilityType = "hook"           // 生命周期 Hook
    CapMiddleware    CapabilityType = "middleware"      // 中间件
    CapStorage       CapabilityType = "storage"        // 存储后端
    CapProvider      CapabilityType = "provider"       // LLM Provider
)
```

### 能力注册扩展接口

```go
// PluginWithCapabilities 是可选接口，插件按需实现。
// 未实现该接口的插件被视为"纯生命周期插件"。
type PluginWithCapabilities interface {
    Plugin
    Capabilities() []Capability
}
```

### 能力类型对照表

| 能力类型 | 接口 | Runtime 接入点 | 典型场景 |
|---------|------|---------------|---------|
| `tool_provider` | `ToolProvider` | Tool Registry | 自定义工具集 |
| `skill_provider` | `SkillProvider` | Skill Manager | 领域技能包 |
| `hook` | `LifecycleHook` | Event Bus | 会话前后处理 |
| `middleware` | `Middleware` | Request Pipeline | 请求拦截/转换 |
| `storage` | `StorageBackend` | Storage Layer | 自定义存储引擎 |
| `provider` | `LLMProvider` | Provider Manager | 接入新 LLM |

## 完整示例：自定义指标插件

```go
package metrics

import (
    "context"
    "fmt"
    "time"
)

// MetricsPlugin 收集 Runtime 运行指标并暴露给 Prometheus。
type MetricsPlugin struct {
    id      string
    version string
    config  MetricsConfig
    server  *http.Server
    healthy bool
}

// --- Plugin 接口实现 ---

func (p *MetricsPlugin) ID() string         { return "com.yaa.plugin.metrics" }
func (p *MetricsPlugin) Name() string       { return "Metrics Collector" }
func (p *MetricsPlugin) Version() string    { return p.version }

func (p *MetricsPlugin) Init(ctx context.Context, cfg Config) error {
    if err := cfg.Decode(&p.config); err != nil {
        return fmt.Errorf("metrics: decode config: %w", err)
    }
    if p.config.Port == 0 {
        p.config.Port = 9090 // 默认端口
    }
    return nil
}

func (p *MetricsPlugin) Start(ctx context.Context) error {
    mux := http.NewServeMux()
    mux.HandleFunc("/metrics", p.handleMetrics)
    p.server = &http.Server{
        Addr:    fmt.Sprintf(":%d", p.config.Port),
        Handler: mux,
    }
    go func() {
        _ = p.server.ListenAndServe()
    }()
    p.healthy = true
    return nil
}

func (p *MetricsPlugin) Stop(ctx context.Context) error {
    p.healthy = false
    if p.server != nil {
        return p.server.Shutdown(ctx)
    }
    return nil
}

func (p *MetricsPlugin) Health() HealthStatus {
    level := HealthHealthy
    if !p.healthy {
        level = HealthUnhealthy
    }
    return HealthStatus{
        Status:    level,
        Message:   fmt.Sprintf("listening on :%d", p.config.Port),
        Timestamp: time.Now(),
    }
}

// --- PluginWithCapabilities 实现 ---

func (p *MetricsPlugin) Capabilities() []Capability {
    return []Capability{
        {Type: CapMiddleware, Name: "metrics_collector", Instance: p},
    }
}

// --- 工厂函数 ---

func New() Plugin {
    return &MetricsPlugin{version: "1.0.0"}
}
```

## 向后兼容策略

| 场景 | 策略 | 说明 |
|------|------|------|
| 新增方法 | 嵌入默认实现结构体 | 旧插件不实现新方法也能编译 |
| 修改方法签名 | 新建 V2 接口 | 保留旧接口，Runtime 双重断言 |
| 废弃方法 | 标注 `// Deprecated` | 保留 2 个大版本后移除 |
| 能力扩展 | 新增 CapabilityType | 不影响已有能力注册 |

```go
// 默认实现结构体，供插件嵌入以获得新方法的默认行为。
type PluginBase struct{}

func (PluginBase) Health() HealthStatus {
    return HealthStatus{Status: HealthUnknown, Timestamp: time.Now()}
}

// 未来版本新增方法示例：
// type PluginV2 interface {
//     Plugin
//     Reload(ctx context.Context) error  // 热重载
// }
// PluginBase 实现 Reload 的默认 no-op 版本。
```

## Plugin 与 Manager/Loader 的协作关系

```
Loader 发现插件 → 实例化 Plugin → 调用 Init(cfg)
    → Manager 注册到插件表
    → 调用 Start() → 注册 Capabilities → 接入子系统
    → 定期调用 Health() 巡检
    → 关闭时调用 Stop() → 注销 Capabilities → 从插件表移除
```

Manager 负责维护插件依赖图、启动顺序和并行控制；Loader 负责从配置或文件系统发现插件并实例化。两者通过 `Plugin` 接口与插件交互，不依赖具体实现类型。
