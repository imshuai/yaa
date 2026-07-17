# Plugin 集成设计

> Plugin 与 Tool / Skill / Memory / Runtime 的集成方案
> 依赖: `architecture.md` §3.7-§3.10, `tool/manager.md` §3, `skill/manager.md` §3, `memory/integration.md` §1

---

## 1. 集成总览

Plugin 是 Yaa! 的扩展机制，它不是孤立模块，而是 Runtime 各子系统的"注入点"。一个插件可以同时注册 Tool、注册 Skill、提供 Memory Backend，并参与 Runtime 生命周期。

| 集成方向 | 目标模块 | 注册接口 | 触发时机 |
|----------|----------|----------|----------|
| Plugin → Tool | Tool Manager | `toolMgr.Register(tool, config)` | 插件 `Init()` 阶段 |
| Plugin → Skill | Skill Manager | `skillMgr.Register(skill, config)` | 插件 `Init()` 阶段 |
| Plugin → Memory | Memory System | `memory.RegisterBackend(backend)` | 插件 `Init()` 阶段 |
| Plugin ↔ Runtime | Runtime 生命周期 | `Plugin.Init()` / `Plugin.Shutdown()` | Runtime 启动/关闭 |

---

## 2. 插件注册 Tool

插件通过实现 `tool.Tool` 接口并向 Tool Manager 注册，将自定义能力暴露给 Agent。

### 2.1 接口契约

```go
// Plugin 在 Init 阶段向 Tool Manager 注册 Tool。
type ToolPlugin interface {
    Plugin
    // RegisterTools 将插件提供的 Tool 注册到 Manager。
    RegisterTools(tm *tool.Manager) error
}
```

### 2.2 代码示例

```go
package weatherplugin

import (
    "context"
    "encoding/json"
    "github.com/imshuai/yaa/pkg/tool"
)

type WeatherTool struct{}

func (t *WeatherTool) Name() string { return "weather" }
func (t *WeatherTool) Description() string { return "查询指定城市的天气" }
func (t *WeatherTool) Parameters() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {"type": "string", "description": "城市名称"}
        },
        "required": ["city"]
    }`)
}
func (t *WeatherTool) Execute(ctx context.Context, params map[string]any) (tool.ToolResult, error) {
    city := params["city"].(string)
    // ... 调用天气 API ...
    return tool.ToolResult{Output: fmt.Sprintf("%s: 晴, 25°C", city)}, nil
}

// RegisterTools 实现接口
func (p *WeatherPlugin) RegisterTools(tm *tool.Manager) error {
    return tm.Register(&WeatherTool{}, tool.ToolConfig{
        Enabled:  true,
        Timeout:  10 * time.Second,
        MaxRetry: 1,
    })
}
```

注册后，Tool Manager 中该 Tool 的 `Source` 字段标记为 `"plugin"`，与内置 Tool `"builtin"` 和 MCP Tool `"mcp"` 区分。

---

## 3. 插件注册 Skill

插件可以打包完整的 Skill 定义（Prompt + 依赖 Tool + 配置），通过 Skill Manager 注册。

### 3.1 接口契约

```go
type SkillPlugin interface {
    Plugin
    // RegisterSkills 将插件提供的 Skill 注册到 Manager。
    RegisterSkills(sm *skill.Manager) error
}
```

### 3.2 代码示例

```go
package weatherplugin

import (
    "github.com/imshuai/yaa/pkg/skill"
)

func (p *WeatherPlugin) RegisterSkills(sm *skill.Manager) error {
    s := &skill.Skill{
        Name:        "weather-assistant",
        Description: "天气助手 Skill，组合天气查询与出行建议",
        Tools:       []string{"weather", "http"},
        Prompt:      "你是一个天气助手。使用 weather 工具查询天气，给出出行建议。",
        Options:     map[string]any{"default_units": "metric"},
    }
    return sm.Register(s, skill.SkillConfig{
        Enabled:  true,
        Timeout:  30 * time.Second,
        MaxRetry: 2,
    })
}
```

Skill Manager 注册后，`SkillEntry.Source` 标记为 `"plugin"`，与 `"local"` / `"git"` / `"registry"` 区分。插件注册的 Skill 同样受权限控制和 Tool 绑定检查。

---

## 4. 插件提供 Memory Backend

插件可以实现自定义存储后端（如 Redis、PostgreSQL、向量数据库），作为 Memory 系统的 Backend。

### 4.1 接口契约

```go
type MemoryPlugin interface {
    Plugin
    // ProvideMemoryBackend 返回一个 Memory Backend 实例。
    ProvideMemoryBackend() (memory.Backend, error)
}
```

### 4.2 代码示例

```go
package redismemory

import (
    "context"
    "github.com/imshuai/yaa/pkg/memory"
)

type RedisBackend struct {
    client *redis.Client
}

func (b *RedisBackend) Store(ctx context.Context, item *memory.Item) error {
    data, _ := json.Marshal(item)
    return b.client.Set(ctx, item.Key, data, 0).Err()
}
func (b *RedisBackend) Retrieve(ctx context.Context, key string) (*memory.Item, error) {
    val, err := b.client.Get(ctx, key).Result()
    // ... 反序列化 ...
    return item, nil
}
func (b *RedisBackend) Search(ctx context.Context, query string, limit int) ([]*memory.Item, error) {
    // ... Redis Search 或向量检索 ...
    return results, nil
}
func (b *RedisBackend) Delete(ctx context.Context, key string) error {
    return b.client.Del(ctx, key).Err()
}

// ProvideMemoryBackend 实现接口
func (p *RedisMemoryPlugin) ProvideMemoryBackend() (memory.Backend, error) {
    return &RedisBackend{client: p.client}, nil
}
```

Memory System 在启动时调用 `ProvideMemoryBackend()`，将返回的 Backend 注册到 Backend 列表中。配置中可通过 `memory.backend: "redis-plugin"` 指定使用该后端。

---

## 5. 插件与 Runtime 生命周期集成

插件遵循统一的 `Plugin` 基础接口，嵌入 Runtime 的启动与关闭流程。

### 5.1 Plugin 基础接口

```go
// Plugin 是所有插件的基础接口。
type Plugin interface {
    // Name 返回插件唯一标识。
    Name() string
    // Init 在 Runtime 启动阶段调用，传入 Runtime 上下文。
    Init(ctx *PluginContext) error
    // Shutdown 在 Runtime 关闭阶段调用，释放资源。
    Shutdown(ctx context.Context) error
}

// PluginContext 提供插件访问 Runtime 各子系统的能力。
type PluginContext struct {
    Config    *config.Config
    ToolMgr   *tool.Manager
    SkillMgr  *skill.Manager
    Memory    memory.Memory
    Storage   storage.Storage
    Logger    *slog.Logger
}
```

### 5.2 生命周期流程图

```text
Runtime 启动
  │
  ▼
┌─────────────────────────────────────┐
│  1. 加载配置 (config.yaml)           │
│  2. 初始化 Storage / Provider        │
│  3. 初始化 Memory / Tool / Skill Mgr │
│  4. 扫描插件目录或配置中的插件列表    │
└──────────────────┬──────────────────┘
                   │
                   ▼
          ┌────────────────────────┐
          │  Plugin Manager 遍历插件 │
          │                         │
          │  for each plugin:       │
          │    ① plugin.Init(ctx)   │
          │    ② if ToolPlugin:     │
          │       RegisterTools()   │
          │    ③ if SkillPlugin:    │
          │       RegisterSkills()  │
          │    ④ if MemoryPlugin:   │
          │       RegisterBackend() │
          └───────────┬────────────┘
                      │
                      ▼
          ┌────────────────────────┐
          │  5. 校验依赖完整性       │
          │     (Tool/Skill 引用    │
          │      是否全部注册成功)   │
          └───────────┬────────────┘
                      │
                      ▼
          ┌────────────────────────┐
          │  6. Runtime Ready       │
          │     API Server 开始    │
          └────────────────────────┘

Runtime 关闭
  │
  ▼
  Plugin Manager 按加载逆序调用
    plugin.Shutdown(ctx)
  → 释放连接、保存状态、清理资源
  → Runtime 优雅退出
```

---

## 6. 插件配置声明

插件通过配置文件声明，Runtime 启动时自动加载：

```yaml
plugins:
  - name: "weather"
    type: "builtin"          # builtin | local | git
    enabled: true
    options:
      api_key: "${WEATHER_API_KEY}"
      default_units: "metric"

  - name: "redis-memory"
    type: "local"
    enabled: true
    options:
      addr: "localhost:6379"
      db: 0
```

| 配置字段 | 类型 | 说明 |
|----------|------|------|
| `name` | string | 插件唯一标识，对应 `Plugin.Name()` |
| `type` | string | 来源类型：`builtin`(内置) / `local`(本地路径) / `git`(远程拉取) |
| `enabled` | bool | 是否启用，`false` 则跳过加载 |
| `options` | map | 插件特有配置，通过 `PluginContext.Config` 传递 |

---

## 7. 集成约束与注意事项

| 约束 | 说明 |
|------|------|
| **注册顺序** | 插件在 Tool/Skill/Memory Manager 初始化之后加载，确保 Manager 已就绪 |
| **依赖校验** | 插件注册的 Skill 引用的 Tool 必须在所有插件注册完成后存在，否则启动失败 |
| **冲突处理** | 同名 Tool/Skill 后注册者覆盖先注册者，并记录 WARN 日志 |
| **隔离性** | 插件间不共享状态，通过 Runtime 子系统间接协作 |
| **错误传播** | `Init()` 返回 error 将阻止 Runtime 启动；`Shutdown()` 返回 error 仅记录日志 |
| **向后兼容** | 插件接口新增方法使用默认实现，已有插件无需修改 |

---

*最后更新: 2025-07-17*
