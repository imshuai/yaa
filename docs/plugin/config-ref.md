# Plugin 配置参考

> Yaa! Yet Another Agent Runtime
> 文档路径: docs/plugin/config-ref.md
> 上级: [README.md](../README.md)
> 依赖: [architecture.md](../architecture.md) §3.12, [config/reference.md](../config/reference.md)

---

## 1. 概述

Plugin 是 Yaa! 的扩展机制，允许在不修改核心代码的前提下注册自定义 Tool、Provider、Hook 等能力。Plugin 配置位于 `yaa.yaml` 的 `plugins` 节点，支持目录扫描、按名启用/禁用、以及插件级配置传递。

---

## 2. 顶层结构

```go
type PluginsConfig struct {
    Dir     string         `yaml:"dir"      json:"dir"`      // 插件扫描目录
    Entries []PluginEntry  `yaml:"entries"  json:"entries"`  // 插件条目列表
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `dir` | `string` | `"./plugins"` | ✅ | 插件扫描目录，Runtime 启动时递归扫描 |
| `entries` | `[]PluginEntry` | `[]` | ✅ | 显式声明的插件条目，覆盖扫描结果 |

---

## 3. 插件条目

```go
type PluginEntry struct {
    Name    string         `yaml:"name"     json:"name"`     // 插件名称（唯一标识）
    Enabled bool           `yaml:"enabled"  json:"enabled"`  // 是否启用
    Config  map[string]any `yaml:"config"   json:"config"`   // 传递给插件的配置
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `name` | `string` | — | ✅ | 插件唯一标识，需与插件 `plugin.yaml` 中 `name` 一致 |
| `enabled` | `bool` | `true` | ✅ | 是否启用该插件；设为 `false` 可临时禁用 |
| `config` | `map[string]any` | `{}` | ✅ | 传递给插件的自由配置，由插件自行解析 |

---

## 4. 完整 YAML 示例

```yaml
# yaa.yaml — plugins 节点
plugins:
  dir: "./plugins"
  entries:
    # 天气插件：启用 + 自定义配置
    - name: "weather"
      enabled: true
      config:
        api_key: "${WEATHER_API_KEY}"
        default_unit: "celsius"
        cache_ttl: 600s

    # 日志增强插件：启用，无额外配置
    - name: "log-enhancer"
      enabled: true

    # 实验性插件：显式禁用
    - name: "experiment-feature"
      enabled: false
      config:
        flag: "preview"
```

---

## 5. 插件目录结构

扫描目录下每个子目录视为一个独立插件，需包含 `plugin.yaml` 声明文件：

```text
plugins/
├── weather/
│   ├── plugin.yaml        # 插件声明
│   ├── main.so            # Go 插件（.so / .dll）
│   └── assets/            # 插件资源文件
├── log-enhancer/
│   ├── plugin.yaml
│   └── main.so
└── experiment-feature/
    ├── plugin.yaml
    └── main.so
```

`plugin.yaml` 示例：

```yaml
# plugins/weather/plugin.yaml
name: "weather"
version: "0.1.0"
author: "Yaa! Team"
description: "Weather query tool plugin"
type: "tool"          # tool / provider / hook / mixed
entry: "main.so"      # 入口文件（相对路径）
provides:
  tools: ["weather"]
```

---

## 6. 插件级配置传递

Runtime 在加载插件时，将 `entries[].config` 原样传递给插件入口函数。插件自行解析所需字段。

```go
// 插件入口函数签名
type PluginInit func(ctx context.Context, cfg PluginContext) error

type PluginContext struct {
    Name    string
    Config  map[string]any   // 来自 yaa.yaml entries[].config
    Runtime *Runtime          // Runtime 引用，可注册 Tool / Provider 等
    Logger  *slog.Logger
}
```

插件实现示例：

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "time"
)

// WeatherPlugin 天气插件
type WeatherPlugin struct {
    apiKey      string
    defaultUnit string
    cacheTTL    time.Duration
    logger      *slog.Logger
}

// Init 插件入口函数，由 Runtime 调用
func Init(ctx context.Context, pctx PluginContext) error {
    p := &WeatherPlugin{
        apiKey:      getString(pctx.Config, "api_key", ""),
        defaultUnit: getString(pctx.Config, "default_unit", "celsius"),
        cacheTTL:    getDuration(pctx.Config, "cache_ttl", 300*time.Second),
        logger:      pctx.Logger,
    }

    if p.apiKey == "" {
        return fmt.Errorf("weather plugin: api_key is required")
    }

    // 注册 Tool
    pctx.Runtime.Tools.Register(&WeatherTool{plugin: p})
    pctx.Logger.Info("weather plugin loaded", "unit", p.defaultUnit)
    return nil
}

func getString(cfg map[string]any, key, def string) string {
    if v, ok := cfg[key].(string); ok {
        return v
    }
    return def
}

func getDuration(cfg map[string]any, key string, def time.Duration) time.Duration {
    if v, ok := cfg[key].(string); ok {
        if d, err := time.ParseDuration(v); err == nil {
            return d
        }
    }
    return def
}
```

---

## 7. 启用/禁用机制

| 场景 | 行为 |
|------|------|
| `entries` 中声明 `enabled: false` | 插件被跳过，不加载、不注册 |
| `entries` 中声明 `enabled: true` | 正常加载并传递 `config` |
| 目录扫描但 `entries` 未声明 | 按 `plugin.yaml` 默认启用 |
| `entries` 声明但目录不存在 | 启动报错，提示插件缺失 |
| 热更新 `enabled` 字段 | 运行时动态加载/卸载插件 |

---

## 8. 配置优先级

```text
命令行参数 --plugin.<name>.<key>=<value>
    ↓ 覆盖
yaa.yaml entries[].config
    ↓ 覆盖
plugin.yaml 默认值
```

环境变量引用同样支持：`config` 字段值中使用 `${VAR_NAME}` 语法，Runtime 加载时自动替换。

---

*最后更新: 2025-07-17*
