# 配置校验与默认值注入

> 文档路径: docs/config/validation.md
> 上级: [README.md](README.md)
> 依赖: `overview.md` §3.2 Effective Config, `loading.md`（多源加载）

---

## 1. 概述

配置校验与默认值注入是配置加载管线的最后两道工序：

```text
原始配置文件
    │
    ├─ 1. 格式解析 (YAML / TOML / JSON)
    ├─ 2. config_version 迁移
    ├─ 3. 环境变量展开 (${VAR_NAME})
    ├─ 4. 默认值注入          ← 本文档
    ├─ 5. 命令行参数覆盖
    ├─ 6. 基础配置校验        ← 本文档
    │
    ▼
Effective Config (*config.Config)
```

**默认值注入** 在校验之前执行——先补全缺失字段，再对完整结构进行校验。这样可以简化校验逻辑：校验器只需检查"值是否合法"，而无需同时处理"字段缺失"的情况。

---

## 2. 默认值注入

### 2.1 注入策略

配置不能用 Go 零值判断字段是否出现，否则用户显式配置的 `false`、`0` 或空切片会被错误地当成“未设置”。加载器必须保留字段存在性：

1. 将文件解析为 `map[string]any`，完成迁移并展开环境变量。
2. 创建 `cfg := Default()` 作为根结构基底。
3. 调用 `ApplyElementDefaults(raw)`，只为新数组元素和动态 Map 元素补入缺失键。
4. 只把原始 Map 中实际出现的字段解码到 `cfg`；显式零值正常覆盖默认值。
5. 应用命令行参数后执行校验。

切片字段在出现时整体替换；Map 字段按 key 合并。未知字段默认报错，避免拼写错误被静默忽略。

### 2.2 默认值定义

根结构默认值集中定义在 `Default()` 及同包的 `Default*Config` 函数中；配置文件中新出现的切片/动态 Map 元素由 `ApplyElementDefaults(raw)` 补齐缺失键。两者共同构成唯一默认值阶段，且必须以同一份 canonical 默认值表为准。

```go
// Default 返回带完整默认值的 Config 实例。
// 各子系统默认值由同包内对应的 Default*Config 函数维护。
func Default() *Config {
    return &Config{
        ConfigVersion: "1.0",
        Runtime:       DefaultRuntimeConfig(),
        MCP:           DefaultMCPConfig(),
        Tools:         DefaultToolsConfig(),
        Skills:        DefaultSkillsConfig(),
        Memory:        DefaultMemoryConfig(),
        Session:       DefaultSessionConfig(),
        Context:       DefaultContextConfig(),
        Planner:       DefaultPlannerConfig(),
        Plugins:       DefaultPluginsConfig(),
        Log:           DefaultLogConfig(),
        Agents:        []AgentConfig{},
        Providers:     []ProviderConfig{},
    }
}
```

MCP 根配置与 `servers[]` 元素分别提供默认构造器；`ApplyElementDefaults(raw)` 在 typed decode 前使用 `DefaultMCPServerConfig()` 的可选字段基底，否则省略的 `transport` 和 `auto_start` 会错误落为 Go 零值：

```go
func DefaultMCPServerConfig() MCPServerConfig {
    return MCPServerConfig{
        Args:      []string{},
        Env:       map[string]string{},
        Headers:   map[string]string{},
        Transport: "stdio",
        Timeout:   0,
        AutoStart: true,
    }
}

func DefaultMCPConfig() MCPConfig {
    return MCPConfig{
        Servers: []MCPServerConfig{},
        Server: MCPExposeConfig{
            Enabled:         false,
            Transport:       "stdio",
            Addr:            "127.0.0.1:9090",
            Path:            "/mcp",
            MessagesPath:    "/message",
            ExposedTools:     []string{},
            OriginAllowlist:  []string{},
        },
        Timeout: MCPTimeoutConfig{
            Connect: 10 * time.Second,
            Init:    15 * time.Second,
            Tool:    0,
        },
        Reconnect: MCPReconnectConfig{
            Enabled:      true,
            MaxAttempts:  3,
            InitialDelay: time.Second,
            MaxDelay:     time.Minute,
        },
    }
}
```

Context 默认值由同一配置包提供，不能在 Context Manager 中另设一份：

```go
func DefaultContextConfig() ContextConfig {
    return ContextConfig{
        MaxTokens:      0,
        ReservedTokens: 4096,
        Strategy:       "hybrid",
        Compression: ContextCompressionConfig{
            Enabled:        true,
            Threshold:      0.85,
            TargetRatio:    0.60,
            MinMessages:    6,
            PreserveRecent: 3,
            Timeout:        20 * time.Second,
        },
    }
}
```

Session 默认值也只在配置包维护：

```go
func DefaultSessionConfig() SessionConfig {
    return SessionConfig{
        MaxMessages:         1000,
        MaxMessageBytes:     10485760,
        TTL:                 24 * time.Hour,
        MaxLifetime:         720 * time.Hour,
        Persist:             true,
        MaxSessionsPerAgent: 100,
        CleanupInterval:     time.Minute,
    }
}
```

Memory 默认值同样只在配置包维护；vector 关闭时允许 embedding 连接字段为空：

```go
func DefaultMemoryConfig() MemoryConfig {
    return MemoryConfig{
        Enabled:         true,
        MaxItems:        10000,
        DefaultTTL:      0,
        ExpireInterval:  5 * time.Minute,
        ExpireBatchSize: 500,
        EvictionPolicy:  "fifo",
        Storage: MemoryStorageConfig{
            Type: "sqlite",
            Path: "./data/yaa-memory.db",
        },
        Vector: MemoryVectorConfig{
            Enabled:             false,
            SimilarityThreshold: 0.7,
            TopK:                10,
            FallbackToKeyword:   true,
        },
        Embedding: MemoryEmbeddingConfig{
            Provider: "openai-compatible",
            Timeout:  30 * time.Second,
        },
    }
}
```

#### 数组与动态 Map 元素

`Default()` 只能预填根结构和已知 Map entry，不能为配置文件中新出现的切片元素提供基底。`mapstructure` 解码新切片元素时从 Go 零值开始；仅设置 `ZeroFields=false` 不能保留这些元素的非零默认值。因此在 typed decode 前必须对原始 Map 调用：

```go
func ApplyElementDefaults(raw map[string]any) error
```

该函数按完整配置路径检查 object/array 形状，并只插入不存在的 key；用 `_, exists := object[key]` 判断 presence，绝不能按值判断。显式 `false`、`0`、`""`、`[]`、`{}` 和 `null` 保持原样。需要元素基底的路径固定为：

| 路径 | 元素基底 |
|------|----------|
| `agents[]` | `max_tokens=4096`，`system_prompt=""`，`tools=[]`，`skills=[]`，`temperature=null`，四个 override 为 `null`，`tools_config={}`，`skills_config={}` |
| `agents[].skills_config.<name>` | `options={}` |
| `providers[]` | `api_key=""`，`timeout=120s`，`max_retries=3`，`retry_interval=1s`，`models=[]`，`extra={}` |
| `providers[].models[]` | capability bool 为 `false`，`thinking_efforts=[]`，`min_thinking_budget=0` |
| `runtime.auth.tokens[]` | `roles=["viewer"]` |
| `mcp.servers[]` | `args=[]`、`env={}`、`headers={}`、`transport="stdio"`、`timeout=0`、`auto_start=true` |
| `tools.builtin.<canonical>` | 与 `DefaultToolsConfig().Builtin[name]` 递归合并缺失键 |
| `skills.per_skill.<name>` | `enabled=true`，`options={}` |
| `plugins.entries[]` | `enabled` 保持未设置，`config={}` |

Provider 的 `base_url` 是唯一 type-dependent 默认值：先读取同一 raw element 的 `type`；只有 `base_url` key 缺失时，才为 `openai|claude|gemini|ollama` 插入 [配置参考](reference.md#4-providers-节点) 的对应 URL。`azure` 和未注册扩展 type 不注入 URL，留给基础/binding 校验处理。`ApplyElementDefaults` 不添加数组元素、不恢复被显式 `[]` 清空的数组，也不补必填的 ID/name/type/model。

### 2.3 Presence-aware 解码

`DecodeInto` 只遍历原始 Map 中存在的字段，并将其写入已经带默认值的 `cfg`。它必须支持 `time.Duration` 转换、未知字段检测和字段路径错误；不要先解码为零值 `Config` 再做非零值合并。由于 `mapstructure` 在 `ZeroFields=false` 时会保留旧 slice 尾部并忽略 `nil`，`DecodeInto` 必须先执行 presence 预处理：已出现的 slice 清零后整体替换，非 null map 按 key 合并，nullable 的 `null` 清零，非 pointer/slice/map/interface 的 `null` 以完整路径返回类型错误。`ApplyElementDefaults` 不覆盖显式 `null`；预处理失败时不得进入 typed decoder。

```go
cfg := Default()
raw, err := ParseToMap(data, format)
if err != nil {
    return nil, err
}
if err := new(EnvResolver).ResolveMap(raw); err != nil {
    return nil, err
}
if err := ApplyElementDefaults(raw); err != nil {
    return nil, err
}
if err := DecodeInto(raw, cfg); err != nil {
    return nil, err
}
```

---

## 3. 配置校验

### 3.1 校验阶段

校验在默认值注入和命令行参数覆盖之后执行，此时 `*Config` 已是完整的 Effective Config。

### 3.2 校验规则分类

| 类别 | 检查内容 | 失败行为 | 示例 |
|------|----------|----------|------|
| **必填检查** | 关键字段不能为空 | 拒绝启动 | `providers` 为空且 `agents` 引用了 provider |
| **类型检查** | 枚举值在允许集合内 | 拒绝启动 | `storage.type` 必须是 `sqlite`/`memory` |
| **范围检查** | 数值在合法区间 | 拒绝启动 | `tools.max_concurrent` 必须 > 0 |
| **引用检查** | 引用目标存在 | 拒绝启动 | `agents[0].provider` 引用的 provider 必须存在 |
| **依赖检查** | 启用某能力时相关字段齐全 | 拒绝启动 | `runtime.auth.enabled=true` 时至少配置一种认证方式 |
| **格式检查** | 字符串格式合法 | 拒绝启动 | `api.http.addr` 符合 `host:port` 格式 |

基础 Validator 覆盖完整 DTO 的静态规则，而不只覆盖示例中的字段：

| 配置区域 | 基础规则 |
|----------|----------|
| `runtime` | SQLite path 非空；HTTP addr、正数 read/write timeout、正数 max header bytes；Auth 结构与凭据依赖 |
| `agents[]` | ID/name/provider/model 必填，ID 唯一，`max_tokens > 0`，temperature 为 nil 或 `0..2`，Tool/Skill allowlist 项非空且唯一 |
| `providers[]` / `models[]` | Provider 连接、重试和 URL 规则；Model ID 唯一、窗口范围及 Thinking 元数据一致性 |
| `mcp` / `tools` / `skills` | transport、地址、路径、timeout、并发和静态结构规则；动态引用与 options 延后到 binding 校验 |
| `memory` / `session` / `context` / `planner` | 根值与每个 Agent 合并后的 effective 值都满足对应模块范围和枚举 |
| `plugins` / `log` | Plugin 路径/timeout/restart/entry 结构，以及日志 level/format/output |

关闭功能只跳过运行期依赖，不跳过其结构、枚举和范围校验。例如 `memory.enabled=false` 仍校验 storage/vector 范围，`mcp.servers[].auto_start=false` 与 `mcp.server.enabled=false` 仍校验 descriptor；Auth 关闭时仍校验已提供 Token/JWT descriptor 的结构、Role 引用和范围，但不要求当前 `token_type` 的凭据完整性；只有“有效 effective Memory 同时启用且 vector 启用”才要求完整 embedding 连接。跨运行时 catalog 的 Provider type、模型、Tool、Skill、Plugin Manifest 和 MCP expose 引用由 §3.5/`validateBindings` 处理，基础 Validator 不猜测动态注册表。

所有浮点范围规则都显式拒绝 `NaN`；不能只写 `<`/`>` 比较，因为二者对 `NaN` 均为 false。`+Inf`/`-Inf` 由正常范围比较拒绝，Skill options 中的非有限浮点由 JSON-compatible 检查拒绝。

### 3.3 校验错误格式

校验错误使用结构化格式，包含路径、规则、消息，便于定位：

```go
var ErrConfigValidationFailed = errors.New("config validation failed")

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

func (errs ValidationErrors) Unwrap() error {
    return ErrConfigValidationFailed
}

func add(errs *ValidationErrors, path, rule, message string) {
    *errs = append(*errs, &ValidationError{Path: path, Rule: rule, Message: message})
}
```

`Validator.Validate` 返回 `nil` 或非空的 `ValidationErrors` 值；调用方可用 `errors.As` 读取全部结构化错误，并用 `errors.Is(err, ErrConfigValidationFailed)` 识别校验失败。返回前按 `Path`、`Rule`、`Message` 字典序稳定排序，禁止依赖 Map 遍历顺序。错误路径中数组统一使用 `[n]`；Config Tool 的点分隔索引不适用于错误路径。

### 3.4 校验器实现

```go
// Validator 对完整 Config 执行所有校验规则。
type Validator struct{}

// Validate 返回所有校验错误。返回 nil 表示配置合法。
func (v *Validator) Validate(cfg *Config) error {
    var errs ValidationErrors

    if cfg == nil {
        add(&errs, "", "required", "config must not be nil")
        return errs
    }

    // --- 根配置静态检查 ---
    validateRuntimeConfig(&errs, "runtime", cfg.Runtime)
    validateMCPConfig(&errs, "mcp", cfg.MCP)
    validateToolsConfig(&errs, "tools", cfg.Tools)
    validateSkillsConfig(&errs, "skills", cfg.Skills)
    validateMemoryConfig(&errs, "memory", cfg.Memory)
    validateContextConfig(&errs, "context", cfg.Context)
    validateSessionConfig(&errs, "session", cfg.Session)
    validatePlannerConfig(&errs, "planner", cfg.Planner)
    validatePluginsConfig(&errs, "plugins", cfg.Plugins)
    validateLogConfig(&errs, "log", cfg.Log)
    embeddingRequired := cfg.Memory.Enabled && cfg.Memory.Vector.Enabled

    // --- 引用检查 ---
    providerIDs := make(map[string]bool)
    for i, p := range cfg.Providers {
        path := fmt.Sprintf("providers[%d]", i)
        if p.ID == "" {
            add(&errs, path+".id", "required", "provider id must not be empty")
        } else {
            if providerIDs[p.ID] {
                add(&errs, path+".id", "unique", "provider id must be unique")
            }
            providerIDs[p.ID] = true
        }
        validateProviderConfig(&errs, path, p)
    }

    agentIDs := make(map[string]bool)
    for i, a := range cfg.Agents {
        path := fmt.Sprintf("agents[%d]", i)
        if a.ID == "" {
            add(&errs, path+".id", "required", "agent id must not be empty")
        } else {
            if agentIDs[a.ID] {
                add(&errs, path+".id", "unique", "agent id must be unique")
            }
            agentIDs[a.ID] = true
        }
        if a.Name == "" {
            errs = append(errs, &ValidationError{
                Path:    path + ".name",
                Rule:    "required",
                Message: "agent name must not be empty",
            })
        }
        if a.Provider == "" {
            add(&errs, path+".provider", "required", "provider must not be empty")
        } else if !providerIDs[a.Provider] {
            errs = append(errs, &ValidationError{
                Path:    path + ".provider",
                Rule:    "reference",
                Message: fmt.Sprintf("provider %q not defined in providers list", a.Provider),
            })
        }
        if a.Model == "" {
            add(&errs, path+".model", "required", "model must not be empty")
        }
        if a.MaxTokens <= 0 {
            add(&errs, path+".max_tokens", "range", "must be > 0")
        }
        if a.Temperature != nil &&
            (math.IsNaN(*a.Temperature) || *a.Temperature < 0 || *a.Temperature > 2) {
            add(&errs, path+".temperature", "range", "must be in 0..2")
        }
        validateUniqueNames(&errs, path+".tools", "tool name", a.Tools)
        validateUniqueNames(&errs, path+".skills", "skill name", a.Skills)
        validateAgentSkillConfigs(&errs, path+".skills_config", a.SkillsConfig)
        effectiveContext := ResolveContextConfig(cfg.Context, a.Context)
        validateContextConfig(&errs, fmt.Sprintf("agents[%d].context", i), effectiveContext)
        effectiveSession := ResolveSessionPolicy(cfg.Session, a.Session, nil)
        validateSessionPolicy(&errs, fmt.Sprintf("agents[%d].session", i), effectiveSession)
        effectiveMemory := ResolveMemoryPolicy(cfg.Memory, a.Memory)
        validateMemoryPolicy(&errs, fmt.Sprintf("agents[%d].memory", i), effectiveMemory)
        invalidMemoryEnable := a.Memory != nil && a.Memory.Enabled != nil &&
            *a.Memory.Enabled && !cfg.Memory.Enabled
        if invalidMemoryEnable {
            add(&errs, fmt.Sprintf("agents[%d].memory.enabled", i), "dependency",
                "cannot enable memory when root memory.enabled is false")
        }
        if !invalidMemoryEnable {
            embeddingRequired = embeddingRequired ||
                (effectiveMemory.Enabled && effectiveMemory.Vector.Enabled)
        }
        effectivePlanner := ResolvePlannerConfig(cfg.Planner, a.Planner)
        validatePlannerConfig(&errs, fmt.Sprintf("agents[%d].planner", i), effectivePlanner)
    }
    if embeddingRequired {
        validateMemoryEmbedding(&errs, "memory.embedding", cfg.Memory.Embedding)
    }

    if len(errs) > 0 {
        sort.SliceStable(errs, func(i, j int) bool {
            if errs[i].Path != errs[j].Path { return errs[i].Path < errs[j].Path }
            if errs[i].Rule != errs[j].Rule { return errs[i].Rule < errs[j].Rule }
            return errs[i].Message < errs[j].Message
        })
        return errs
    }
    return nil
}
```

这些基础规则不得只写在表格或各模块构造器里；统一 Validator 使用手写 helper。`validateRuntimeConfig` 必须校验 `storage.type=sqlite|memory`、SQLite path 非空、HTTP read/write timeout 与 max header bytes 均为正数，并用 `validateListenAddr` 的 `valid` 返回值阻止伪 Auth 级联错误。`validateToolsConfig` 使用 [配置参考 §6](reference.md#6-tools-节点) 的范围、拒绝未知的 root builtin key，并要求每个 root Tool timeout 位于 `0..max_timeout`；Agent `tools_config` 的 presence-aware timeout/options 解码留给 binding 阶段。`validateSkillsConfig` 要求 `dir` 和动态 key 非空，并检查 root/Agent options 只含 JSON-compatible 值且可被标准 JSON 编码；合并后的敏感 key、大小和 Skill catalog 引用留给 binding 阶段。下列 helper 给出稳定的 Path/Rule/Message：

```go
import (
    "crypto/sha256"
    "encoding/json"
    "errors"
    "fmt"
    "math"
    "net"
    "net/url"
    "path"
    "reflect"
    "regexp"
    "sort"
    "strconv"
    "strings"
    "time"
)

var mcpServerNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validateRuntimeConfig(errs *ValidationErrors, field string, c RuntimeConfig) {
    if c.Storage.Type != "sqlite" && c.Storage.Type != "memory" {
        add(errs, field+".storage.type", "enum", "must be sqlite or memory")
    }
    if c.Storage.Type == "sqlite" && c.Storage.Path == "" {
        add(errs, field+".storage.path", "required", "must not be empty for sqlite")
    }

    http := c.API.HTTP
    loopback, valid := validateListenAddr(errs, field+".api.http.addr", http.Addr)
    if http.ReadTimeout <= 0 {
        add(errs, field+".api.http.read_timeout", "range", "must be > 0")
    }
    if http.WriteTimeout <= 0 {
        add(errs, field+".api.http.write_timeout", "range", "must be > 0")
    }
    if http.MaxHeaderBytes <= 0 {
        add(errs, field+".api.http.max_header_bytes", "range", "must be > 0")
    }
    validateAuthConfig(errs, field+".auth", c.Auth, loopback, valid)
}

func validateToolsConfig(errs *ValidationErrors, field string, c ToolsConfig) {
    if c.DefaultTimeout <= 0 {
        add(errs, field+".default_timeout", "range", "must be > 0")
    }
    if c.MaxTimeout <= 0 {
        add(errs, field+".max_timeout", "range", "must be > 0")
    } else if c.MaxTimeout < c.DefaultTimeout {
        add(errs, field+".max_timeout", "range", "must be >= default_timeout")
    }
    if c.MaxConcurrent <= 0 {
        add(errs, field+".max_concurrent", "range", "must be > 0")
    }
    if c.MaxConcurrentPerSession <= 0 ||
        (c.MaxConcurrent > 0 && c.MaxConcurrentPerSession > c.MaxConcurrent) {
        add(errs, field+".max_concurrent_per_session", "range",
            "must be > 0 and <= max_concurrent")
    }
    if c.DefaultMaxRetry < 0 || c.DefaultMaxRetry > 10 {
        add(errs, field+".default_max_retry", "range", "must be in 0..10")
    }
    if c.MaxResultTokens <= 0 {
        add(errs, field+".max_result_tokens", "range", "must be > 0")
    }

    canonical := DefaultToolsConfig().Builtin
    for name, tool := range c.Builtin {
        if name == "" {
            add(errs, field+".builtin", "required", "builtin tool name must not be empty")
            continue
        }
        p := field + ".builtin." + name
        if _, ok := canonical[name]; !ok {
            add(errs, p, "enum", "must be a canonical builtin tool configuration key")
        }
        if tool.Timeout < 0 || (c.MaxTimeout > 0 && tool.Timeout > c.MaxTimeout) {
            add(errs, p+".timeout", "range", "must be in 0..max_timeout")
        }
    }
}

func validateSkillsConfig(errs *ValidationErrors, field string, c SkillsConfig) {
    if c.Dir == "" {
        add(errs, field+".dir", "required", "must not be empty")
    }
    for name, skill := range c.PerSkill {
        if name == "" {
            add(errs, field+".per_skill", "required", "skill name must not be empty")
            continue
        }
        validateJSONOptions(errs, field+".per_skill."+name+".options", skill.Options)
    }
}

func validateAgentSkillConfigs(errs *ValidationErrors, field string, configs map[string]AgentSkillConfig) {
    for name, skill := range configs {
        if name == "" {
            add(errs, field, "required", "skill name must not be empty")
            continue
        }
        validateJSONOptions(errs, field+"."+name+".options", skill.Options)
    }
}

func validateJSONOptions(errs *ValidationErrors, field string, value any) {
    if _, err := json.Marshal(value); err != nil || !hasJSONKinds(reflect.ValueOf(value)) {
        add(errs, field, "type", "must contain only JSON-compatible values")
    }
}

func hasJSONKinds(value reflect.Value) bool {
    if !value.IsValid() {
        return true
    }
    if value.Kind() == reflect.Interface {
        if value.IsNil() { return true }
        return hasJSONKinds(value.Elem())
    }
    switch value.Kind() {
    case reflect.Bool, reflect.String,
        reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
        reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
        return true
    case reflect.Float32, reflect.Float64:
        f := value.Float()
        return !math.IsNaN(f) && !math.IsInf(f, 0)
    case reflect.Array, reflect.Slice:
        for i := 0; i < value.Len(); i++ {
            if !hasJSONKinds(value.Index(i)) { return false }
        }
        return true
    case reflect.Map:
        if value.Type().Key().Kind() != reflect.String { return false }
        iter := value.MapRange()
        for iter.Next() {
            if !hasJSONKinds(iter.Value()) { return false }
        }
        return true
    default:
        return false
    }
}

func validateUniqueNames(errs *ValidationErrors, field, noun string, values []string) {
    seen := make(map[string]bool, len(values))
    for i, value := range values {
        p := fmt.Sprintf("%s[%d]", field, i)
        if value == "" {
            add(errs, p, "required", noun+" must not be empty")
        } else if seen[value] {
            add(errs, p, "unique", noun+" must be unique")
        }
        if value != "" { seen[value] = true }
    }
}

func validateMCPConfig(errs *ValidationErrors, field string, c MCPConfig) {
    if c.Timeout.Connect <= 0 {
        add(errs, field+".timeout.connect", "range", "must be > 0")
    }
    if c.Timeout.Init <= 0 {
        add(errs, field+".timeout.init", "range", "must be > 0")
    }
    if c.Timeout.Tool < 0 {
        add(errs, field+".timeout.tool", "range", "must be >= 0")
    }
    if c.Reconnect.MaxAttempts < 0 {
        add(errs, field+".reconnect.max_attempts", "range", "must be >= 0")
    }
    if c.Reconnect.InitialDelay <= 0 {
        add(errs, field+".reconnect.initial_delay", "range", "must be > 0")
    }
    if c.Reconnect.MaxDelay < c.Reconnect.InitialDelay {
        add(errs, field+".reconnect.max_delay", "range", "must be >= initial_delay")
    }

    names := make(map[string]bool, len(c.Servers))
    for i, s := range c.Servers {
        p := fmt.Sprintf("%s.servers[%d]", field, i)
        if s.Name == "" {
            add(errs, p+".name", "required", "server name must not be empty")
        } else {
            if !mcpServerNameRE.MatchString(s.Name) {
                add(errs, p+".name", "format", "must match ^[a-z0-9][a-z0-9-]{0,63}$")
            }
            if names[s.Name] {
                add(errs, p+".name", "unique", "server name must be unique")
            }
            names[s.Name] = true
        }
        if s.Timeout < 0 {
            add(errs, p+".timeout", "range", "must be >= 0")
        }

        switch s.Transport {
        case "stdio":
            if s.Command == "" {
                add(errs, p+".command", "required", "must not be empty for stdio")
            }
            if s.URL != "" { add(errs, p+".url", "dependency", "is not valid for stdio") }
            if len(s.Headers) != 0 { add(errs, p+".headers", "dependency", "are not valid for stdio") }
            if s.TLS.CAFile != "" { add(errs, p+".tls.ca_file", "dependency", "is not valid for stdio") }
        case "sse", "streamable_http":
            u, err := url.ParseRequestURI(s.URL)
            if err != nil || u.Host == "" || u.User != nil ||
                (u.Scheme != "http" && u.Scheme != "https") {
                add(errs, p+".url", "format", "must be an absolute http/https URL")
            }
            if s.Command != "" { add(errs, p+".command", "dependency", "is not valid for network transports") }
            if len(s.Args) != 0 { add(errs, p+".args", "dependency", "are not valid for network transports") }
            if len(s.Env) != 0 { add(errs, p+".env", "dependency", "is not valid for network transports") }
        default:
            add(errs, p+".transport", "enum", "must be stdio, sse, or streamable_http")
        }
    }

    // enabled/auto_start 只控制启动；所有 descriptor 始终执行上述结构校验。
    if c.Server.Enabled && c.Server.AgentID == "" {
        add(errs, field+".server.agent_id", "required", "must not be empty when server is enabled")
    }
    exposed := make(map[string]bool, len(c.Server.ExposedTools))
    for i, name := range c.Server.ExposedTools {
        p := fmt.Sprintf("%s.server.exposed_tools[%d]", field, i)
        if name == "" {
            add(errs, p, "required", "tool name must not be empty")
        } else if exposed[name] {
            add(errs, p, "unique", "exposed tool name must be unique")
        }
        exposed[name] = true
    }
    switch c.Server.Transport {
    case "stdio":
    case "sse", "streamable_http":
        loopback, valid := validateListenAddr(errs, field+".server.addr", c.Server.Addr)
        if valid && !loopback {
            add(errs, field+".server.addr", "dependency",
                "must be loopback; expose through an authenticated TLS reverse proxy")
        }
        endpoint := c.Server.Path
        endpointField := field + ".server.path"
        if c.Server.Transport == "sse" {
            endpoint = c.Server.MessagesPath
            endpointField = field + ".server.messages_path"
        }
        u, err := url.ParseRequestURI(endpoint)
        if err != nil || !strings.HasPrefix(endpoint, "/") || u.IsAbs() ||
            u.RawQuery != "" || u.Fragment != "" || path.Clean(endpoint) != endpoint {
            add(errs, endpointField, "format", "must be a canonical absolute path without query or fragment")
        }
    default:
        add(errs, field+".server.transport", "enum", "must be stdio, sse, or streamable_http")
    }
    validateOriginAllowlist(errs, field+".server.origin_allowlist", c.Server.OriginAllowlist)
}

func validateOriginAllowlist(errs *ValidationErrors, field string, origins []string) {
    seen := make(map[string]bool, len(origins))
    for i, origin := range origins {
        p := fmt.Sprintf("%s[%d]", field, i)
        u, err := url.ParseRequestURI(origin)
        if err != nil || u.Host == "" || u.User != nil ||
            (u.Scheme != "http" && u.Scheme != "https") ||
            u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
            add(errs, p, "format", "must be an exact http/https origin without path, query, or fragment")
        }
        if seen[origin] {
            add(errs, p, "unique", "origin must be unique")
        }
        seen[origin] = true
    }
}

func validateListenAddr(errs *ValidationErrors, field, addr string) (loopback bool, valid bool) {
    host, portText, err := net.SplitHostPort(addr)
    if err != nil {
        add(errs, field, "format", "must be host:port")
        return false, false
    }
    port, err := strconv.Atoi(portText)
    if err != nil || port < 1 || port > 65535 {
        add(errs, field, "range", "port must be in 1..65535")
        valid = false
    } else {
        valid = true
    }
    if strings.EqualFold(host, "localhost") {
        return true, valid
    }
    ip := net.ParseIP(host)
    return ip != nil && ip.IsLoopback(), valid
}

func validateAuthConfig(errs *ValidationErrors, field string, c AuthConfig, loopback, listenValid bool) {
    if listenValid && !loopback && !c.Enabled {
        add(errs, field+".enabled", "dependency",
            "authentication must be enabled for non-loopback listen addresses")
    }
    if c.TokenType != "static" && c.TokenType != "jwt" {
        add(errs, field+".token_type", "enum", "must be static or jwt")
    }

    roles := make(map[string]bool, len(c.Roles))
    validActions := map[string]bool{"read": true, "write": true, "delete": true, "*": true}
    validResources := map[string]bool{
        "agents": true, "sessions": true, "tools": true, "skills": true,
        "providers": true, "memory": true, "mcp": true, "config": true,
        "system": true, "*": true,
    }
    for i, role := range c.Roles {
        p := fmt.Sprintf("%s.roles[%d]", field, i)
        if role.Name == "" {
            add(errs, p+".name", "required", "role name must not be empty")
        } else if roles[role.Name] {
            add(errs, p+".name", "unique", "role name must be unique")
        }
        if role.Name != "" { roles[role.Name] = true }
        for j, perm := range role.Permissions {
            pp := fmt.Sprintf("%s.permissions[%d]", p, j)
            if !validActions[perm.Action] {
                add(errs, pp+".action", "enum", "must be read, write, delete, or *")
            }
            if !validResources[perm.Resource] {
                add(errs, pp+".resource", "enum", "unknown Remote API resource")
            }
            if perm.Effect != "" && perm.Effect != "allow" && perm.Effect != "deny" {
                add(errs, pp+".effect", "enum", "must be allow or deny")
            }
        }
    }

    public := make(map[string]bool, len(c.PublicPaths))
    for i, p := range c.PublicPaths {
        pp := fmt.Sprintf("%s.public_paths[%d]", field, i)
        u, err := url.ParseRequestURI(p)
        if err != nil || !strings.HasPrefix(p, "/") || u.IsAbs() ||
            u.RawQuery != "" || u.Fragment != "" || path.Clean(p) != p {
            add(errs, pp, "format", "must be a canonical absolute path without query or fragment")
        }
        if public[p] {
            add(errs, pp, "unique", "public path must be unique")
        }
        public[p] = true
    }

    // 关闭 Auth 只跳过当前 token_type 的凭据完整性依赖；已提供的
    // Token、JWT 和 Role descriptor 始终执行结构、引用和范围校验。
    names := make(map[string]bool, len(c.Tokens))
    hashes := make(map[[32]byte]bool, len(c.Tokens))
    for i, token := range c.Tokens {
        p := fmt.Sprintf("%s.tokens[%d]", field, i)
        if token.Name == "" {
            add(errs, p+".name", "required", "token name must not be empty")
        } else if names[token.Name] {
            add(errs, p+".name", "unique", "token name must be unique")
        }
        if token.Name != "" { names[token.Name] = true }
        if token.Token == "" {
            add(errs, p+".token", "required", "token must not be empty")
        } else {
            hash := sha256.Sum256([]byte(token.Token))
            if hashes[hash] {
                add(errs, p+".token", "unique", "token value must be unique")
            }
            hashes[hash] = true
        }
        if len(token.Roles) == 0 {
            add(errs, p+".roles", "required", "at least one role is required")
        }
        for j, role := range token.Roles {
            if !roles[role] {
                add(errs, fmt.Sprintf("%s.roles[%d]", p, j), "reference", "role is not defined")
            }
        }
    }
    if c.JWT.Issuer == "" { add(errs, field+".jwt.issuer", "required", "must not be empty") }
    if c.JWT.Audience == "" { add(errs, field+".jwt.audience", "required", "must not be empty") }
    if c.JWT.ClockSkew < 0 || c.JWT.ClockSkew > 5*time.Minute {
        add(errs, field+".jwt.clock_skew", "range", "must be in 0..5m")
    }

    if c.Enabled && c.TokenType == "static" && len(c.Tokens) == 0 {
        add(errs, field+".tokens", "required", "at least one token is required")
    }
    if c.Enabled && c.TokenType == "jwt" && len(c.JWT.Secret) < 32 {
        add(errs, field+".jwt.secret", "range", "HS256 secret must contain at least 32 bytes")
    }
}

func validateProviderConfig(errs *ValidationErrors, field string, p ProviderConfig) {
    if p.Type == "" {
        add(errs, field+".type", "required", "provider type must not be empty")
    }
    if p.Timeout <= 0 {
        add(errs, field+".timeout", "range", "must be > 0")
    }
    if p.MaxRetries < 0 || p.MaxRetries > 10 {
        add(errs, field+".max_retries", "range", "must be in 0..10")
    }
    if p.RetryInterval <= 0 || p.RetryInterval > time.Minute {
        add(errs, field+".retry_interval", "range", "must be in (0,1m]")
    }
    switch p.Type {
    case "openai", "claude", "gemini", "ollama", "azure":
        if p.BaseURL == "" {
            add(errs, field+".base_url", "required", "base_url is required for built-in provider types")
        } else {
            u, err := url.ParseRequestURI(p.BaseURL)
            if err != nil || u.Host == "" || u.User != nil ||
                (u.Scheme != "http" && u.Scheme != "https") {
                add(errs, field+".base_url", "format", "must be an absolute http/https URL")
            }
        }
    }
    switch p.Type {
    case "openai", "claude", "gemini", "azure":
        if p.APIKey == "" {
            add(errs, field+".api_key", "required", "api_key is required for this provider type")
        }
    }

    models := make(map[string]bool, len(p.Models))
    for i, model := range p.Models {
        mp := fmt.Sprintf("%s.models[%d]", field, i)
        if model.ID == "" {
            add(errs, mp+".id", "required", "model id must not be empty")
        } else if models[model.ID] {
            add(errs, mp+".id", "unique", "model id must be unique")
        }
        if model.ID != "" { models[model.ID] = true }
        if model.ContextWindow <= 0 {
            add(errs, mp+".context_window", "range", "must be > 0")
        }
        if model.MaxOutput <= 0 || (model.ContextWindow > 0 && model.MaxOutput >= model.ContextWindow) {
            add(errs, mp+".max_output", "range", "must be > 0 and < context_window")
        }
        if model.MinThinkingBudget < 0 {
            add(errs, mp+".min_thinking_budget", "range", "must be >= 0")
        }
        efforts := make(map[string]bool, len(model.ThinkingEfforts))
        for j, effort := range model.ThinkingEfforts {
            ep := fmt.Sprintf("%s.thinking_efforts[%d]", mp, j)
            if effort != "low" && effort != "medium" && effort != "high" && effort != "max" {
                add(errs, ep, "enum", "must be low, medium, high, or max")
            }
            if efforts[effort] {
                add(errs, ep, "unique", "thinking effort must be unique")
            }
            efforts[effort] = true
        }
        if !model.SupportsThinking && (len(model.ThinkingEfforts) != 0 || model.MinThinkingBudget != 0) {
            add(errs, mp+".supports_thinking", "dependency",
                "thinking efforts and budget require supports_thinking=true")
        }
    }
}
```

Planner 的 Agent 覆盖先解析为完整 effective 值再校验；覆盖字段使用 `PlannerOverride` 的非 nil 指针，因此 `0` 不会被当成“缺失”：

```go
func ResolvePlannerConfig(root PlannerConfig, override *PlannerOverride) PlannerConfig {
    out := root
    if override == nil { return out }
    if override.Type != nil { out.Type = *override.Type }
    if override.Model != nil { out.Model = *override.Model }
    if override.Temperature != nil { value := *override.Temperature; out.Temperature = &value }
    if override.MaxTokens != nil { out.MaxTokens = *override.MaxTokens }
    if override.MaxSteps != nil { out.MaxSteps = *override.MaxSteps }
    if override.MaxConcurrent != nil { out.MaxConcurrent = *override.MaxConcurrent }
    if override.Timeout != nil { out.Timeout = *override.Timeout }
    return out
}

func validatePlannerConfig(errs *ValidationErrors, path string, c PlannerConfig) {
    if c.Type != "llm" && c.Type != "disabled" {
        add(errs, path+".type", "enum", "must be llm or disabled")
    }
    if c.Temperature == nil {
        add(errs, path+".temperature", "required", "must not be nil")
    } else if math.IsNaN(*c.Temperature) || *c.Temperature < 0 || *c.Temperature > 2 {
        add(errs, path+".temperature", "range", "must be in 0..2")
    }
    if c.MaxTokens < 1 || c.MaxTokens > 16384 {
        add(errs, path+".max_tokens", "range", "must be in 1..16384")
    }
    if c.MaxSteps < 1 || c.MaxSteps > 64 {
        add(errs, path+".max_steps", "range", "must be in 1..64")
    }
    if c.MaxConcurrent < 1 || c.MaxConcurrent > 16 {
        add(errs, path+".max_concurrent", "range", "must be in 1..16")
    }
    if c.Timeout < time.Second || c.Timeout > 5*time.Minute {
        add(errs, path+".timeout", "range", "must be in 1s..5m")
    }
}
```

`validateBindings` 在启动期 Provider factory registry、builtin Tool、Plugin Tool Proxy 和 MCP Tool Proxy 都建立后执行，再确认 `ProviderConfig.Type` 已注册，并校验 Agent、MCP expose Agent/Tool、Skill 与 Tool 引用。每个 `agents[i].skills_config.<name>` 必须同时出现在该 Agent 的 `skills` 精确 allowlist 和已加载 Skill catalog 中。它还对 `tools.builtin.<name>.options` 和 `agents[i].tools_config.<name>.options` 调用每个 builtin 的严格 option decoder，并校验 Agent Tool timeout 位于 `0..max_timeout`；越界错误固定为 `agents[i].tools_config.<name>.timeout / range / must be in 0..max_timeout`，未知 key、类型或范围错误必须带完整源路径。Skill binding 在 frontmatter、root options 和 Agent options 合并后递归规范化 key，拒绝敏感 key，并限制最终 JSON 编码不超过 64 KiB。Reload candidate 在发布前重跑适用于可热更新字段的同一校验。基础 Validator 不把静态链接并已注册的扩展 type 误判成未知枚举，也不假设扩展 Provider 的 `base_url` scheme；其地址语义由注册 factory 在 binding 阶段校验。进程外 Plugin 不提供 Provider type。

Plugin 和 Log 的静态规则也属于基础 Validator：

```go
func validatePluginsConfig(errs *ValidationErrors, path string, c PluginsConfig) {
    validateUniqueNames(errs, path+".paths", "plugin path", c.Paths)
    for _, item := range []struct{name string; value time.Duration}{
        {"startup_timeout", c.StartupTimeout}, {"stop_timeout", c.StopTimeout},
        {"health_interval", c.HealthInterval}, {"health_timeout", c.HealthTimeout},
    } {
        if item.value <= 0 { add(errs, path+"."+item.name, "range", "must be > 0") }
    }
    if c.HealthTimeout > c.HealthInterval {
        add(errs, path+".health_timeout", "range", "must be <= health_interval")
    }
    if c.Restart.MaxAttempts < 0 { add(errs, path+".restart.max_attempts", "range", "must be >= 0") }
    if c.Restart.Backoff <= 0 || c.Restart.Backoff > time.Minute {
        add(errs, path+".restart.backoff", "range", "must be in (0,1m]")
    }
    ids := make(map[string]bool, len(c.Entries))
    for i, entry := range c.Entries {
        p := fmt.Sprintf("%s.entries[%d]", path, i)
        if entry.ID == "" { add(errs, p+".id", "required", "plugin id must not be empty")
        } else if ids[entry.ID] { add(errs, p+".id", "unique", "plugin id must be unique") }
        ids[entry.ID] = true
    }
}

func validateLogConfig(errs *ValidationErrors, path string, c LogConfig) {
    if c.Level != "debug" && c.Level != "info" && c.Level != "warn" && c.Level != "error" {
        add(errs, path+".level", "enum", "must be debug, info, warn, or error")
    }
    if c.Format != "text" && c.Format != "json" {
        add(errs, path+".format", "enum", "must be text or json")
    }
    if c.Output == "" { add(errs, path+".output", "required", "must not be empty") }
    // stdout/stderr 或文件路径均可；路径存在性和可写性由启动 binding 检查。
}
```

Session 的根配置与 resolved policy 使用同一字段规则。Manager-only 字段只在根配置校验：

```go
func validateSessionConfig(errs *ValidationErrors, path string, c SessionConfig) {
    validateSessionPolicy(errs, path, SessionPolicy{
        MaxMessages:     c.MaxMessages,
        MaxMessageBytes: c.MaxMessageBytes,
        TTL:             c.TTL,
        MaxLifetime:     c.MaxLifetime,
        Persist:         c.Persist,
    })
    if c.MaxSessionsPerAgent <= 0 {
        add(errs, path+".max_sessions_per_agent", "range", "must be > 0")
    }
    if c.CleanupInterval < time.Second {
        add(errs, path+".cleanup_interval", "range", "must be >= 1s")
    }
}

func validateSessionPolicy(errs *ValidationErrors, path string, p SessionPolicy) {
    if p.MaxMessages <= 0 {
        add(errs, path+".max_messages", "range", "must be > 0")
    }
    if p.MaxMessageBytes <= 0 {
        add(errs, path+".max_message_bytes", "range", "must be > 0")
    }
    if p.TTL < 0 || (p.TTL > 0 && p.TTL < time.Minute) {
        add(errs, path+".ttl", "range", "must be 0 or >= 1m")
    }
    if p.MaxLifetime < 0 || (p.MaxLifetime > 0 && p.MaxLifetime < time.Minute) {
        add(errs, path+".max_lifetime", "range", "must be 0 or >= 1m")
    }
    if p.TTL > 0 && p.MaxLifetime > 0 && p.MaxLifetime < p.TTL {
        add(errs, path+".max_lifetime", "range", "must be >= ttl when both are enabled")
    }
}
```

Create 请求合并 `SessionOverride` 后必须再次调用 `validateSessionPolicy`。`persist=false` 和 `ttl=0` 是合法显式值，不能被默认值覆盖。

Memory 的根配置包含共享基础设施和 cleanup 字段；Agent 只解析成 `MemoryPolicy`：

```go
func validateMemoryConfig(errs *ValidationErrors, path string, c MemoryConfig) {
    validateMemoryPolicy(errs, path, MemoryPolicy{
        Enabled:        c.Enabled,
        MaxItems:       c.MaxItems,
        DefaultTTL:     c.DefaultTTL,
        EvictionPolicy: c.EvictionPolicy,
        Vector:         c.Vector,
    })
    if c.ExpireInterval < time.Second {
        add(errs, path+".expire_interval", "range", "must be >= 1s")
    }
    if c.ExpireBatchSize < 1 || c.ExpireBatchSize > 10000 {
        add(errs, path+".expire_batch_size", "range", "must be in 1..10000")
    }
    if c.Storage.Type != "sqlite" && c.Storage.Type != "memory" {
        add(errs, path+".storage.type", "enum", "must be sqlite or memory")
    }
    if c.Storage.Type == "sqlite" && c.Storage.Path == "" {
        add(errs, path+".storage.path", "required", "must not be empty for sqlite")
    }
}

func validateMemoryPolicy(errs *ValidationErrors, path string, p MemoryPolicy) {
    if p.MaxItems <= 0 {
        add(errs, path+".max_items", "range", "must be > 0")
    }
    if p.DefaultTTL < 0 || (p.DefaultTTL > 0 && p.DefaultTTL < time.Minute) {
        add(errs, path+".default_ttl", "range", "must be 0 or >= 1m")
    }
    if p.EvictionPolicy != "fifo" && p.EvictionPolicy != "ttl" {
        add(errs, path+".eviction_policy", "enum", "must be fifo or ttl")
    }
    if math.IsNaN(p.Vector.SimilarityThreshold) ||
        p.Vector.SimilarityThreshold <= 0 || p.Vector.SimilarityThreshold > 1 {
        add(errs, path+".vector.similarity_threshold", "range", "must be in (0,1]")
    }
    if p.Vector.TopK < 1 || p.Vector.TopK > 100 {
        add(errs, path+".vector.top_k", "range", "must be in 1..100")
    }
}

func validateMemoryEmbedding(errs *ValidationErrors, path string, e MemoryEmbeddingConfig) {
    if e.Provider != "openai-compatible" {
        add(errs, path+".provider", "enum", "must be openai-compatible")
    }
    if e.Model == "" {
        add(errs, path+".model", "required", "must not be empty when vector is enabled")
    }
    u, err := url.ParseRequestURI(e.BaseURL)
    if e.BaseURL == "" {
        add(errs, path+".base_url", "required", "must not be empty when vector is enabled")
    } else if err != nil || u.Host == "" || u.User != nil ||
        (u.Scheme != "http" && u.Scheme != "https") {
        add(errs, path+".base_url", "format", "must be an absolute http/https URL")
    }
    if e.Dimension <= 0 { add(errs, path+".dimension", "range", "must be > 0") }
    if e.Timeout <= 0 { add(errs, path+".timeout", "range", "must be > 0") }
}
```

Strict decoder 在这些函数之前拒绝未知字段。Agent `MemoryOverride` 不声明 storage、embedding 或 cleanup 字段，因此尝试覆盖这些路径会直接返回带完整路径的 unknown-field 错误。根和 Agent effective policy 无论 `enabled` 值都校验自身范围；只有至少一个有效 effective policy 同时满足 `enabled=true` 且 `vector.enabled=true` 时，顶层 Validator 才调用一次 `validateMemoryEmbedding("memory.embedding", ...)`，避免重复错误，也避免关闭 Memory 时强制填写未使用的连接字段。若 Agent 在 root `memory.enabled=false` 时显式重新启用 Memory，该覆盖先报 dependency，且不触发 embedding completeness 错误。

`validateContextConfig` 至少执行以下规则；非法值全部拒绝，不回退到默认值：

```go
func validateContextConfig(errs *ValidationErrors, path string, c ContextConfig) {
    if c.MaxTokens < 0 { add(errs, path+".max_tokens", "range", "must be >= 0") }
    if c.ReservedTokens < 0 { add(errs, path+".reserved_tokens", "range", "must be >= 0") }
    if c.MaxTokens > 0 && c.ReservedTokens >= c.MaxTokens {
        add(errs, path+".reserved_tokens", "range", "must be less than max_tokens when max_tokens is set")
    }
    if c.Strategy != "hybrid" && c.Strategy != "truncate" && c.Strategy != "reject" {
        add(errs, path+".strategy", "enum", "must be hybrid, truncate, or reject")
    }
    if math.IsNaN(c.Compression.Threshold) ||
        c.Compression.Threshold <= 0 || c.Compression.Threshold > 1 {
        add(errs, path+".compression.threshold", "range", "must be in (0,1]")
    }
    if math.IsNaN(c.Compression.TargetRatio) || c.Compression.TargetRatio <= 0 ||
        c.Compression.TargetRatio >= c.Compression.Threshold {
        add(errs, path+".compression.target_ratio", "range", "must be in (0,threshold)")
    }
    if c.Compression.MinMessages < 2 { add(errs, path+".compression.min_messages", "range", "must be >= 2") }
    if c.Compression.PreserveRecent < 0 { add(errs, path+".compression.preserve_recent", "range", "must be >= 0") }
    if c.Compression.Timeout <= 0 { add(errs, path+".compression.timeout", "range", "must be > 0") }
}
```

### 3.5 模型相关的第二阶段校验

加载器无法仅凭文件值知道模型窗口。Provider Manager 解析 Agent 引用后，必须对每个 Agent 执行：

1. 取 `provider.Models()` 中与 Agent `model` 对应的 `ModelInfo`。
2. 要求 `ContextWindow > 0`、`MaxOutput > 0`，否则返回 `ErrProviderWindowUnknown`。
3. 取 Agent `max_tokens` 作为 `request.MaxTokens`，要求 `0 < max_tokens <= ModelInfo.MaxOutput`。
4. 以 `ModelInfo.ContextWindow` 为有效窗口；若 `context.max_tokens` 为更小的正数，则改用该值。Context 的 0 表示不收紧。
5. 要求 `max_tokens <= context.reserved_tokens < effective_window`。
6. 对热更新候选配置重复全部步骤；任一 Agent 失败则整批 reload 拒绝。

Provider 只返回模型 ID 时不能跳过这些检查。动态模型元数据必须由 Provider 目录补全，或由用户在 `providers[].models[]` 声明。

### 3.6 校验在加载管线中的调用

基础校验只由 [`loading.md`](loading.md) 的统一 `config.Load` 入口在默认值、迁移、环境变量和命令行覆盖之后调用一次：`new(Validator).Validate(cfg)`。Runtime 用该返回值构造 Provider/Tool/Plugin/MCP/Skill catalog 后，对同一个 snapshot 调用 `ReloadManager.Activate()`；该方法执行一次 `validateBindings`，成功前不启动 watcher、Config Tool、Remote API 或任何请求入口。热更新候选复用同一 `config.Load` 和 `validateBindings`，不维护第二套加载/校验代码。

`Default*Config` 都是 `internal/config` 中的结构体字面量；动态元素的缺失键由 `ApplyElementDefaults(raw)` 按同一 canonical 表注入。文档无需为每个字面量复制一份函数体，测试必须逐字段比较根/子配置默认值与 [完整配置参考](reference.md) 的 canonical 值，并覆盖元素默认表。

---

## 4. 设计要点总结

| 要点 | 说明 |
|------|------|
| **默认值先行** | 先注入默认值再校验，校验器无需处理字段缺失 |
| **保留字段存在性** | 显式 `false`、`0`、空列表可以覆盖默认值 |
| **切片整体替换** | 切片不做元素级合并，用户声明即完整替换 |
| **收集所有错误** | 校验器一次性返回所有错误，而非遇到首个就停止 |
| **结构化错误** | `ValidationError` 包含路径和规则名，便于定位和程序化处理 |
| **单一数据源** | `Default()`/`Default*Config` 与 `ApplyElementDefaults` 共享 canonical 默认值表，避免分散维护 |

---

*最后更新: 2025-07-17*
