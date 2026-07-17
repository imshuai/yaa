# 配置校验与默认值合并

> 文档路径: docs/config/validation.md
> 上级: [README.md](README.md)
> 依赖: `overview.md` §3.2 Effective Config, `loading.md`（多源加载）

---

## 1. 概述

配置校验与默认值合并是配置加载管线的最后两道工序：

```text
原始配置文件
    │
    ├─ 1. 格式解析 (YAML / TOML / JSON)
    ├─ 2. 环境变量展开 (${VAR_NAME})
    ├─ 3. 默认值注入          ← 本文档
    ├─ 4. 命令行参数覆盖
    ├─ 5. 配置校验            ← 本文档
    │
    ▼
Effective Config (*config.Config)
```

**默认值注入** 在校验之前执行——先补全缺失字段，再对完整结构进行校验。这样可以简化校验逻辑：校验器只需检查"值是否合法"，而无需同时处理"字段缺失"的情况。

---

## 2. 默认值注入

### 2.1 注入策略

| 策略 | 说明 | 示例 |
|------|------|------|
| **零值检测** | Go 零值（`""`, `0`, `false`, `nil`）视为未设置，注入默认值 | `addr: ""` → `:8080` |
| **递归注入** | 对嵌套结构体递归执行，包括切片元素 | `agents[0]` 内部字段逐一注入 |
| **不覆盖** | 已有非零值不被覆盖 | `addr: ":9090"` 保持不变 |
| **切片初始化** | `nil` 切片注入为空切片 `[]T{}`，避免下游 nil 判断 | `agents: nil` → `agents: []` |

### 2.2 默认值定义

默认值集中定义在 `Default()` 函数中，与 `Config` 结构体位于同一文件，确保单一数据源。

```go
// Default 返回带完整默认值的 Config 实例。
// 该实例作为多源合并的基底（最低优先级）。
func Default() *Config {
    return &Config{
        ConfigVersion: "1",
        Runtime: RuntimeConfig{
            Storage: StorageConfig{
                Type: "sqlite",
                Path: "data/yaa.db",
            },
            API: APIConfig{
                HTTP: HTTPConfig{
                    Addr:    ":8080",
                    Enabled: true,
                },
                WS: WSConfig{
                    Enabled:  true,
                    Addr:     ":8081",
                    MaxConns: 1000,
                },
                SSE: SSEConfig{
                    Enabled: true,
                },
            },
            Log: LogConfig{
                Level:  "info",
                Format: "text",
                Output: "stderr",
            },
        },
        Tools: ToolsConfig{
            Builtin: BuiltinToolsConfig{
                Shell: ShellToolConfig{
                    Enabled: true,
                    Timeout: 30 * time.Second,
                },
                HTTP: HTTPToolConfig{
                    Enabled: true,
                    Timeout: 30 * time.Second,
                },
            },
            MaxParallel: 10,
        },
        Skills: SkillsConfig{
            Dir: "skills",
        },
        MCP: MCPConfig{
            Servers: []MCPServerConfig{},
        },
        Agents:      []AgentConfig{},
        Providers:   []ProviderConfig{},
    }
}
```

### 2.3 合并函数

`Merger` 负责将用户配置覆盖到默认值基底上，采用递归合并而非整体替换：

```go
// Merge 将 src 中的非零值合并到 dst（dst 已包含默认值）。
// 合并完成后 dst 即为 Effective Config 的配置文件层。
func Merge(dst, src *Config) {
    // Runtime
    mergeString(&dst.Runtime.Storage.Type, src.Runtime.Storage.Type)
    mergeString(&dst.Runtime.Storage.Path, src.Runtime.Storage.Path)

    mergeString(&dst.Runtime.API.HTTP.Addr, src.Runtime.API.HTTP.Addr)
    mergeBool(&dst.Runtime.API.HTTP.Enabled, src.Runtime.API.HTTP.Enabled)

    mergeString(&dst.Runtime.API.WS.Addr, src.Runtime.API.WS.Addr)
    mergeBool(&dst.Runtime.API.WS.Enabled, src.Runtime.API.WS.Enabled)
    mergeInt(&dst.Runtime.API.WS.MaxConns, src.Runtime.API.WS.MaxConns)

    mergeString(&dst.Runtime.Log.Level, src.Runtime.Log.Level)
    mergeString(&dst.Runtime.Log.Format, src.Runtime.Log.Format)
    mergeString(&dst.Runtime.Log.Output, src.Runtime.Log.Output)

    // Tools
    mergeBool(&dst.Tools.Builtin.Shell.Enabled, src.Tools.Builtin.Shell.Enabled)
    mergeDuration(&dst.Tools.Builtin.Shell.Timeout, src.Tools.Builtin.Shell.Timeout)
    mergeInt(&dst.Tools.MaxParallel, src.Tools.MaxParallel)

    // 切片：用户配置非空则整体替换（不做元素级合并）
    if len(src.Agents) > 0 {
        dst.Agents = src.Agents
    }
    if len(src.Providers) > 0 {
        dst.Providers = src.Providers
    }
    if len(src.MCP.Servers) > 0 {
        dst.MCP.Servers = src.MCP.Servers
    }
}

// --- 基础合并辅助函数 ---

func mergeString(dst *string, src string) {
    if src != "" {
        *dst = src
    }
}

func mergeBool(dst *bool, src bool) {
    if src {
        *dst = src
    }
}

func mergeInt(dst *int, src int) {
    if src != 0 {
        *dst = src
    }
}

func mergeDuration(dst *time.Duration, src time.Duration) {
    if src != 0 {
        *dst = src
    }
}
```

> **注意：** 切片类型采用"整体替换"策略。如果用户在配置文件中定义了 `agents` 列表，则完全替换默认的空列表，不做逐元素合并。这符合直觉：用户显式声明了完整列表。

---

## 3. 配置校验

### 3.1 校验阶段

校验在默认值注入和命令行参数覆盖之后执行，此时 `*Config` 已是完整的 Effective Config。

### 3.2 校验规则分类

| 类别 | 检查内容 | 失败行为 | 示例 |
|------|----------|----------|------|
| **必填检查** | 关键字段不能为空 | 拒绝启动 | `providers` 为空且 `agents` 引用了 provider |
| **类型检查** | 枚举值在允许集合内 | 拒绝启动 | `storage.type` 必须是 `sqlite`/`postgres`/`mysql` |
| **范围检查** | 数值在合法区间 | 拒绝启动 | `tools.max_parallel` 必须 > 0 |
| **引用检查** | 引用目标存在 | 拒绝启动 | `agents[0].provider` 引用的 provider 必须存在 |
| **互斥检查** | 互斥配置不同时启用 | 拒绝启动 | `api.http.enabled` 和 `api.ws.enabled` 至少一个为 true |
| **格式检查** | 字符串格式合法 | 拒绝启动 | `api.http.addr` 符合 `host:port` 格式 |

### 3.3 校验错误格式

校验错误使用结构化格式，包含路径、规则、消息，便于定位：

```go
// ValidationError 描述单个配置校验错误。
type ValidationError struct {
    Path    string // 配置路径，如 "agents[0].provider"
    Rule    string // 规则名称，如 "required", "enum", "range"
    Message string // 人类可读的错误描述
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("config validation failed at %q [%s]: %s",
        e.Path, e.Rule, e.Message)
}

// ValidationErrors 是多个校验错误的集合。
// 校验器收集所有错误后一次性返回，而非遇到第一个就停止。
type ValidationErrors []*ValidationError

func (errs ValidationErrors) Error() string {
    msgs := make([]string, len(errs))
    for i, e := range errs {
        msgs[i] = e.Error()
    }
    return strings.Join(msgs, "\n")
}
```

### 3.4 校验器实现

```go
// Validator 对完整 Config 执行所有校验规则。
type Validator struct{}

// Validate 返回所有校验错误。返回 nil 表示配置合法。
func (v *Validator) Validate(cfg *Config) error {
    var errs ValidationErrors

    // --- 必填检查 ---
    if cfg.Runtime.Storage.Type == "" {
        errs = append(errs, &ValidationError{
            Path:    "runtime.storage.type",
            Rule:    "required",
            Message: "storage type must not be empty",
        })
    }

    // --- 类型检查（枚举） ---
    switch cfg.Runtime.Storage.Type {
    case "", "sqlite", "postgres", "mysql":
        // 合法值（空值已在上面检查）
    default:
        errs = append(errs, &ValidationError{
            Path:    "runtime.storage.type",
            Rule:    "enum",
            Message: fmt.Sprintf("unsupported storage type %q, must be sqlite/postgres/mysql",
                cfg.Runtime.Storage.Type),
        })
    }

    // --- 范围检查 ---
    if cfg.Tools.MaxParallel <= 0 {
        errs = append(errs, &ValidationError{
            Path:    "tools.max_parallel",
            Rule:    "range",
            Message: "max_parallel must be > 0",
        })
    }

    // --- 互斥检查 ---
    if !cfg.Runtime.API.HTTP.Enabled && !cfg.Runtime.API.WS.Enabled {
        errs = append(errs, &ValidationError{
            Path:    "runtime.api",
            Rule:    "mutex",
            Message: "at least one of http.enabled or ws.enabled must be true",
        })
    }

    // --- 引用检查 ---
    providerNames := make(map[string]bool)
    for i, p := range cfg.Providers {
        if p.Name == "" {
            errs = append(errs, &ValidationError{
                Path:    fmt.Sprintf("providers[%d].name", i),
                Rule:    "required",
                Message: "provider name must not be empty",
            })
            continue
        }
        providerNames[p.Name] = true
    }

    for i, a := range cfg.Agents {
        if a.Name == "" {
            errs = append(errs, &ValidationError{
                Path:    fmt.Sprintf("agents[%d].name", i),
                Rule:    "required",
                Message: "agent name must not be empty",
            })
        }
        if a.Provider != "" && !providerNames[a.Provider] {
            errs = append(errs, &ValidationError{
                Path:    fmt.Sprintf("agents[%d].provider", i),
                Rule:    "reference",
                Message: fmt.Sprintf("provider %q not defined in providers list", a.Provider),
            })
        }
    }

    if len(errs) > 0 {
        return errs
    }
    return nil
}
```

### 3.5 校验在加载管线中的调用

```go
// Load 加载并校验配置，返回 Effective Config。
func Load(opts ...LoadOption) (*Config, error) {
    // 1. 构建默认值基底
    cfg := Default()

    // 2. 解析配置文件（如果存在）
    fileCfg, err := parseFile(opts...)
    if err != nil {
        return nil, fmt.Errorf("parse config file: %w", err)
    }

    // 3. 环境变量展开
    fileCfg, err = ExpandEnvVars(fileCfg)
    if err != nil {
        return nil, fmt.Errorf("expand env vars: %w", err)
    }

    // 4. 合并：文件配置覆盖默认值
    Merge(cfg, fileCfg)

    // 5. 命令行参数覆盖
    applyFlags(cfg, opts...)

    // 6. 校验
    if err := new(Validator).Validate(cfg); err != nil {
        return nil, fmt.Errorf("config validation: %w", err)
    }

    return cfg, nil
}
```

---

## 4. 设计要点总结

| 要点 | 说明 |
|------|------|
| **默认值先行** | 先注入默认值再校验，校验器无需处理字段缺失 |
| **零值即未设置** | 利用 Go 零值语义判断字段是否被用户显式设置 |
| **切片整体替换** | 切片不做元素级合并，用户声明即完整替换 |
| **收集所有错误** | 校验器一次性返回所有错误，而非遇到首个就停止 |
| **结构化错误** | `ValidationError` 包含路径和规则名，便于定位和程序化处理 |
| **单一数据源** | 默认值集中在 `Default()` 函数，避免分散维护 |

---

*最后更新: 2025-07-17*
