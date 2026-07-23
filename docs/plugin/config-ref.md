# Plugin 配置参考

> 文档路径: `docs/plugin/config-ref.md`
> 上级: [README.md](README.md)

---

## 1. Runtime 配置

配置 DTO 由 `internal/config` 持有，Plugin 模块接收 `config.PluginsConfig`，不重复声明反序列化类型。

```go
type PluginsConfig struct {
    Paths          []string      `yaml:"paths" json:"paths"`
    AutoStart      bool          `yaml:"auto_start" json:"auto_start"`
    StartupTimeout time.Duration `yaml:"startup_timeout" json:"startup_timeout"`
    StopTimeout    time.Duration `yaml:"stop_timeout" json:"stop_timeout"`
    HealthInterval time.Duration `yaml:"health_interval" json:"health_interval"`
    HealthTimeout  time.Duration `yaml:"health_timeout" json:"health_timeout"`
    Restart        RestartConfig `yaml:"restart" json:"restart"`
    Entries        []PluginEntry `yaml:"entries" json:"entries"`
}

type RestartConfig struct {
    Enabled     bool          `yaml:"enabled" json:"enabled"`
    MaxAttempts int           `yaml:"max_attempts" json:"max_attempts"`
    Backoff     time.Duration `yaml:"backoff" json:"backoff"`
}

type PluginEntry struct {
    ID      string         `yaml:"id" json:"id"`
    Enabled *bool          `yaml:"enabled" json:"enabled"`
    Config  map[string]any `yaml:"config" json:"config"`
}
```

| 字段 | 默认值 | 规则 |
|------|--------|------|
| `paths` | `["./plugins"]` | 每项非空且唯一；相对路径以主配置文件目录为基准，规范化后扫描 |
| `auto_start` | `true` | false 时只发现/校验 Manifest，不启动进程 |
| `startup_timeout` | `30s` | `>0`；覆盖进程启动、Dial、Handshake、Init、Ready |
| `stop_timeout` | `10s` | `>0`；覆盖 Stop RPC 和等待退出；超时 Kill |
| `health_interval` | `30s` | `>0`；Health 调用周期 |
| `health_timeout` | `5s` | `>0` 且 `<= health_interval`；失败不自动重启 |
| `restart.enabled` | `true` | Runtime 未进入 Stop 时进程退出后是否重启 |
| `restart.max_attempts` | `3` | `>=0`；每个 Plugin 每次 Runtime 生命周期的重启上限 |
| `restart.backoff` | `1s` | `(0,60s]`；首次等待，之后指数退避，最大 60s |
| `entries[].id` | 无 | 非空且在配置内唯一，随后必须与 Manifest ID 唯一匹配 |
| `entries[].enabled` | 未设置 | 显式 true/false 优先；未设置时使用 `default_enabled` |
| `entries[].config` | `{}` | 启动前按 Manifest `config_schema` 校验，再通过 Init 传递 |

所有 `plugins.*` 都需要重启，不能通过 config reload 改变正在运行的 Plugin。

```yaml
plugins:
  paths: ["./plugins"]
  auto_start: true
  startup_timeout: 30s
  stop_timeout: 10s
  health_interval: 30s
  health_timeout: 5s
  restart:
    enabled: true
    max_attempts: 3
    backoff: 1s
  entries:
    - id: weather
      enabled: true
      config:
        api_key: "${WEATHER_API_KEY}"
        default_unit: celsius
```

## 2. Manifest

每个直接子目录包含一个 `plugin.yaml` 和同目录可执行文件。MVP 不支持远程 endpoint，也不加载动态库。

```yaml
id: weather
display_name: Weather
description: Weather query capability
version: 0.1.0
protocol_version: "1"
requires_runtime: ">=0.1.0 <1.0.0"
entry: yaa-plugin-weather
default_enabled: false
dependencies:
  - id: shared-cache
    version: ">=1.0.0 <2.0.0"
    optional: true
provides:
  - type: tool
    name: weather
    description: Query current weather by city
    schema:
      type: object
      properties:
        city: {type: string}
      required: [city]
config_schema:
  type: object
  additionalProperties: false
  properties:
    api_key: {type: string}
    default_unit: {type: string, enum: [celsius, fahrenheit]}
  required: [api_key]
```

| 字段 | 必填 | 规则 |
|------|:----:|------|
| `id` | 是 | 小写字母、数字和 `-`；全局唯一 |
| `display_name`, `description` | 否 | 展示信息，不参与身份校验 |
| `version` | 是 | Plugin 业务 SemVer |
| `protocol_version` | 是 | Plugin RPC major 字符串；MVP 只接受 `"1"` |
| `requires_runtime` | 否 | Runtime 业务版本 SemVer range |
| `entry` | 是 | Manifest 目录内的可执行文件；规范化后不得逃逸目录 |
| `default_enabled` | 否 | 默认 false |
| `dependencies[]` | 否 | `id`、SemVer `version`、`optional` |
| `provides[]` | 是 | typed capability list；Tool 的 name、description、schema 必填 |
| `config_schema` | 否 | JSON Schema；省略时只接受空 config |

`provides[].type` 的 v1 枚举只有 `tool`，且 `name`、`description`、`schema` 必填。Skill 继续由 SKILL.md 目录加载；Hook/Middleware、Provider 和 Memory 不使用 v1 Plugin RPC。出现其他 type 时 Manifest 校验直接失败。

JSON Schema 的 `default` 仅是说明，不由 Runtime 注入。Plugin 在 Init 内对未配置字段应用自己的默认值；Runtime 只校验用户提供的值。

## 3. 合并与启动

```text
discover Manifest
  → resolve explicit entry by id
  → enabled = entry.enabled ?? manifest.default_enabled
  → validate requires_runtime/dependencies
  → validate entry.config against config_schema
  → start local process
  → Handshake → Init → Ready
```

目录扫描但无显式 entry 时按 `default_enabled`；显式 entry 找不到 Manifest 是配置错误。多个 `paths` 发现相同 ID 时拒绝启动该 ID，不采用“第一个获胜”。

环境变量只由主 Config Loader 展开一次。Plugin RPC、日志、错误、Health 和观测端点不得回显展开后的 Secret。

---

*最后更新: 2025-07-17*
