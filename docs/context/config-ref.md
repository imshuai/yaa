# Context 配置参考

> 文档路径: `docs/context/config-ref.md`
> 上级: [README.md](README.md)

---

## 1. 权威类型

配置 DTO 统一位于 `internal/config`。根配置保存完整值；Agent 只保存 pointer override，确保显式 `false` 和 `0` 不会被误判为缺省。

```go
type ContextConfig struct {
    MaxTokens      int                      `yaml:"max_tokens" json:"max_tokens"`
    ReservedTokens int                      `yaml:"reserved_tokens" json:"reserved_tokens"`
    Strategy       string                   `yaml:"strategy" json:"strategy"`
    Compression    ContextCompressionConfig `yaml:"compression" json:"compression"`
}

type ContextCompressionConfig struct {
    Enabled        bool          `yaml:"enabled" json:"enabled"`
    Threshold      float64       `yaml:"threshold" json:"threshold"`
    TargetRatio    float64       `yaml:"target_ratio" json:"target_ratio"`
    MinMessages    int           `yaml:"min_messages" json:"min_messages"`
    PreserveRecent int           `yaml:"preserve_recent" json:"preserve_recent"`
    Timeout        time.Duration `yaml:"timeout" json:"timeout"`
}

type ContextOverride struct {
    MaxTokens      *int                        `yaml:"max_tokens" json:"max_tokens"`
    ReservedTokens *int                        `yaml:"reserved_tokens" json:"reserved_tokens"`
    Strategy       *string                     `yaml:"strategy" json:"strategy"`
    Compression    *ContextCompressionOverride `yaml:"compression" json:"compression"`
}

type ContextCompressionOverride struct {
    Enabled        *bool          `yaml:"enabled" json:"enabled"`
    Threshold      *float64       `yaml:"threshold" json:"threshold"`
    TargetRatio    *float64       `yaml:"target_ratio" json:"target_ratio"`
    MinMessages    *int           `yaml:"min_messages" json:"min_messages"`
    PreserveRecent *int           `yaml:"preserve_recent" json:"preserve_recent"`
    Timeout        *time.Duration `yaml:"timeout" json:"timeout"`
}
```

`Config.Context` 的类型是 `ContextConfig`；`AgentConfig.Context` 的类型是 `*ContextOverride`。不得在其他模块重复定义这两个 DTO。

## 2. 字段与默认值

| 字段 | 默认值 | 校验规则 |
|------|--------|----------|
| `max_tokens` | `0` | `>= 0`；0 表示使用模型窗口，正数只能进一步收紧 |
| `reserved_tokens` | `4096` | `>= 0`；运行时必须 `request.max_tokens <= reserved_tokens < effective_window` |
| `strategy` | `hybrid` | `hybrid` / `truncate` / `reject` |
| `compression.enabled` | `true` | false 时 `hybrid` 只在超限后执行截断 |
| `compression.threshold` | `0.85` | `(0, 1]`；相对于输入预算计算 |
| `compression.target_ratio` | `0.60` | `(0, threshold)`；相对于输入预算计算 |
| `compression.min_messages` | `6` | `>= 2`；候选历史少于此值时不调用摘要模型 |
| `compression.preserve_recent` | `3` | `>= 0`；单位为完整历史 turn，不是单条消息 |
| `compression.timeout` | `20s` | `> 0` |

```yaml
context:
  max_tokens: 0
  reserved_tokens: 4096
  strategy: hybrid
  compression:
    enabled: true
    threshold: 0.85
    target_ratio: 0.60
    min_messages: 6
    preserve_recent: 3
    timeout: 20s
```

单条 Tool 结果上限只由 `tools.max_result_tokens` 管理，默认 4000。Tool Manager 在结果进入 Context 前应用该限制；Context 不提供第二个同义字段。

## 3. Agent 覆盖

未出现的字段继承根配置，出现的字段按值覆盖。覆盖后的 Effective Context Config 必须重新校验。

```yaml
agents:
  - id: light-agent
    name: Light Agent
    provider: ollama
    model: llama3
    max_tokens: 1024
    context:
      max_tokens: 8192
      reserved_tokens: 1024
      strategy: truncate
      compression:
        enabled: false
```

```go
func ResolveContextConfig(base ContextConfig, override *ContextOverride) ContextConfig {
    out := base
    if override == nil {
        return out
    }
    if override.MaxTokens != nil { out.MaxTokens = *override.MaxTokens }
    if override.ReservedTokens != nil { out.ReservedTokens = *override.ReservedTokens }
    if override.Strategy != nil { out.Strategy = *override.Strategy }
    if c := override.Compression; c != nil {
        if c.Enabled != nil { out.Compression.Enabled = *c.Enabled }
        if c.Threshold != nil { out.Compression.Threshold = *c.Threshold }
        if c.TargetRatio != nil { out.Compression.TargetRatio = *c.TargetRatio }
        if c.MinMessages != nil { out.Compression.MinMessages = *c.MinMessages }
        if c.PreserveRecent != nil { out.Compression.PreserveRecent = *c.PreserveRecent }
        if c.Timeout != nil { out.Compression.Timeout = *c.Timeout }
    }
    return out
}
```

## 4. 两阶段校验与预算

配置加载阶段校验字段范围和枚举。Runtime 创建每个 Agent 时，再用目标 `provider.ModelInfo` 和 Agent 输出上限执行模型相关校验。以下函数属于 Context 包；`internal/config` 不导入 Provider 包：

```go
func ResolveContextBudget(
    cfg config.ContextConfig,
    modelWindow int,
    modelMaxOutput int,
    outputTokens int,
) (Budget, error) {
    if modelWindow <= 0 || modelMaxOutput <= 0 {
        return Budget{}, ErrProviderWindowUnknown
    }
    if outputTokens <= 0 || outputTokens > modelMaxOutput {
        return Budget{}, ErrContextConfigInvalid
    }

    window := modelWindow
    if cfg.MaxTokens > 0 && cfg.MaxTokens < window {
        window = cfg.MaxTokens
    }
    if cfg.ReservedTokens < outputTokens || cfg.ReservedTokens >= window {
        return Budget{}, ErrContextConfigInvalid
    }
    return Budget{
        EffectiveWindow: window,
        ReservedOutput:  cfg.ReservedTokens,
        Input:           window - cfg.ReservedTokens,
    }, nil
}
```

Provider 未声明目标模型的 `ContextWindow` 或 `MaxOutput` 时，引用该模型的 Agent 不能启动。不得把未知窗口当作无限窗口，也不得让 `context.max_tokens` 扩大 Provider 硬上限。

---

*最后更新: 2026-07-22*
