# 完整配置参考

> Yaa! Yet Another Agent Runtime
> 文档路径: docs/config/reference.md
> 上级: [README.md](README.md)
> 依赖: [overview.md](overview.md) §3, [architecture.md](../architecture.md) §3.12

---

## 1. 顶层结构

`Config` 是 Yaa! 运行时的完整配置根结构，所有子系统从此结构中读取自己的配置。本文件是根节点位置、§2-§8/§10 字段名与默认值的 owner；模块 `config-ref.md` 只能补充行为与校验，不能引入同名字段或不同默认值。Session、Context、Planner、Plugin 的完整 DTO 特意委托给 §9 链接的模块文档，`internal/config` 仍是唯一 Go package owner。

```go
type Config struct {
    ConfigVersion string           `yaml:"config_version" json:"config_version"`
    Runtime       RuntimeConfig    `yaml:"runtime"        json:"runtime"`
    Agents        []AgentConfig    `yaml:"agents"         json:"agents"`
    Providers     []ProviderConfig `yaml:"providers"   json:"providers"`
    MCP           MCPConfig        `yaml:"mcp"            json:"mcp"`
    Tools         ToolsConfig      `yaml:"tools"          json:"tools"`
    Skills        SkillsConfig     `yaml:"skills"         json:"skills"`
    Memory        MemoryConfig     `yaml:"memory"         json:"memory"`
    Session       SessionConfig    `yaml:"session"        json:"session"`
    Context       ContextConfig    `yaml:"context"        json:"context"`
    Planner       PlannerConfig    `yaml:"planner"        json:"planner"`
    Plugins       PluginsConfig    `yaml:"plugins"        json:"plugins"`
    Log           LogConfig        `yaml:"log"            json:"log"`
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `config_version` | `string` | `"1.0"` | 配置格式版本号，用于自动迁移 |
| `runtime` | `RuntimeConfig` | 见 §2 | 运行时核心配置（存储、API、认证） |
| `agents` | `[]AgentConfig` | `[]` | Agent 列表；无 Agent 时 Runtime 仍可启动 |
| `providers` | `[]ProviderConfig` | `[]` | LLM Provider 列表；被 Agent 引用时必须存在 |
| `mcp` | `MCPConfig` | 见 §5 | MCP Server 配置 |
| `tools` | `ToolsConfig` | 见 §6 | Tool 系统配置 |
| `skills` | `SkillsConfig` | 见 §7 | Skill 系统配置 |
| `memory` | `MemoryConfig` | 见 §8 | Memory 系统配置 |
| `session` | `SessionConfig` | 见 §9 | Session 默认行为 |
| `context` | `ContextConfig` | 见 §9 | Context 窗口策略 |
| `planner` | `PlannerConfig` | 见 §9 | Planner 与执行并发配置 |
| `plugins` | `PluginsConfig` | 见 §9 | 进程外 Plugin 配置 |
| `log` | `LogConfig` | 见 §10 | 日志配置 |

---

## 2. runtime 节点

```go
type RuntimeConfig struct {
    Storage StorageConfig `yaml:"storage" json:"storage"`
    API     APIConfig     `yaml:"api"     json:"api"`
    Auth    AuthConfig    `yaml:"auth"    json:"auth"`
}

type StorageConfig struct {
    Type string `yaml:"type" json:"type"` // sqlite | memory
    Path string `yaml:"path" json:"path"`
}

type AuthConfig struct {
    Enabled     bool          `yaml:"enabled" json:"enabled"`
    TokenType   string        `yaml:"token_type" json:"token_type"` // static | jwt
    Tokens      []TokenConfig `yaml:"tokens" json:"tokens"`
    Roles       []RoleConfig  `yaml:"roles" json:"roles"`
    PublicPaths []string      `yaml:"public_paths" json:"public_paths"`
    JWT         JWTConfig     `yaml:"jwt" json:"jwt"`
}

type TokenConfig struct {
    Name  string   `yaml:"name" json:"name"`
    Token string   `yaml:"token" json:"token"`
    Roles []string `yaml:"roles" json:"roles"`
}

type RoleConfig struct {
    Name        string             `yaml:"name" json:"name"`
    Permissions []PermissionConfig `yaml:"permissions" json:"permissions"`
}

type PermissionConfig struct {
    Action   string `yaml:"action" json:"action"`
    Resource string `yaml:"resource" json:"resource"`
    Effect   string `yaml:"effect" json:"effect"` // allow | deny
}

type JWTConfig struct {
    Secret       string        `yaml:"secret" json:"secret"`
    Issuer       string        `yaml:"issuer" json:"issuer"`
    Audience     string        `yaml:"audience" json:"audience"`
    ClockSkew    time.Duration `yaml:"clock_skew" json:"clock_skew"`
}
```

### 2.1 runtime.storage

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `type` | `string` | `"sqlite"` | ❌ | 存储后端类型：`sqlite` / `memory` |
| `path` | `string` | `"./data/yaa.db"` | ❌ | 存储文件路径 |

### 2.2 runtime.api

```go
type APIConfig struct {
    HTTP HTTPConfig `yaml:"http" json:"http"`
    WS   WSConfig   `yaml:"ws"   json:"ws"`
    SSE  SSEConfig  `yaml:"sse"  json:"sse"`
}

type HTTPConfig struct {
    Addr           string        `yaml:"addr"             json:"addr"`
    ReadTimeout    time.Duration `yaml:"read_timeout"     json:"read_timeout"`
    WriteTimeout   time.Duration `yaml:"write_timeout"    json:"write_timeout"`
    MaxHeaderBytes int           `yaml:"max_header_bytes" json:"max_header_bytes"`
}

type WSConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
}

type SSEConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `http.addr` | `string` | `"127.0.0.1:8080"` | ❌ | HTTP 监听地址；非回环地址必须显式启用认证并配置凭据 |
| `http.read_timeout` | `duration` | `30s` | ❌ | HTTP 读超时 |
| `http.write_timeout` | `duration` | `30s` | ❌ | HTTP 写超时 |
| `http.max_header_bytes` | `int` | `1048576` (1MB) | ❌ | HTTP 最大请求头 |
| `ws.enabled` | `bool` | `true` | ❌ | 是否启用 WebSocket |
| `sse.enabled` | `bool` | `true` | ❌ | 是否启用 SSE |

### 2.3 runtime.auth

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `auth.enabled` | `bool` | `false` | ❌ | 是否启用认证；对非回环地址监听时必须显式启用 |
| `auth.token_type` | `string` | `"static"` | ❌ | 认证方式：`static` / `jwt` |
| `auth.tokens` | `[]TokenConfig` | `[]` | ❌ | 静态 Token 列表 |
| `auth.tokens[].name` | `string` | — | — | Token 名称（标识用途） |
| `auth.tokens[].token` | `string` | — | — | Token 值，建议 `${VAR_NAME}` 引用 |
| `auth.tokens[].roles` | `[]string` | `["viewer"]` | — | Token 绑定的角色 |
| `auth.roles` | `[]RoleConfig` | `admin`/`operator`/`viewer` | ❌ | RBAC 角色与结构化权限 |
| `auth.public_paths` | `[]string` | 健康与版本端点 | ❌ | 精确匹配的免认证路径 |
| `auth.jwt.secret` | `string` | `""` | ❌ | HS256 Secret；启用 JWT 时至少 32 bytes |
| `auth.jwt.issuer` | `string` | `"yaa-runtime"` | ❌ | JWT issuer 精确值 |
| `auth.jwt.audience` | `string` | `"yaa-client"` | ❌ | JWT audience 精确值 |
| `auth.jwt.clock_skew` | `duration` | `30s` | ❌ | `exp`/`nbf` 容差，范围 `0..5m` |

---

## 3. agents 节点

数组类型，每个元素定义一个 Agent 实例。

```go
type AgentConfig struct {
    ID           string                      `yaml:"id"            json:"id"`
    Name         string                      `yaml:"name"          json:"name"`
    Provider     string                      `yaml:"provider"      json:"provider"`
    Model        string                      `yaml:"model"         json:"model"`
    SystemPrompt string                      `yaml:"system_prompt" json:"system_prompt"`
    Tools        []string                    `yaml:"tools"         json:"tools"`
    Skills       []string                    `yaml:"skills"        json:"skills"`
    MaxTokens    int                         `yaml:"max_tokens"    json:"max_tokens"`
    Temperature  *float64                    `yaml:"temperature"   json:"temperature"`
    Memory       *MemoryOverride             `yaml:"memory"        json:"memory"`
    Session      *SessionOverride            `yaml:"session"       json:"session"`
    Context      *ContextOverride            `yaml:"context"       json:"context"`
    Planner      *PlannerConfig              `yaml:"planner"       json:"planner"`
    ToolsConfig  map[string]any              `yaml:"tools_config"  json:"tools_config"`
    SkillsConfig map[string]AgentSkillConfig `yaml:"skills_config" json:"skills_config"`
}

type AgentSkillConfig struct {
    Options map[string]any `yaml:"options" json:"options"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `id` | `string` | — | ❌ | Agent 唯一标识，**必填** |
| `name` | `string` | — | ❌ | Agent 显示名称，**必填** |
| `provider` | `string` | — | ❌ | 绑定的 Provider ID，**必填** |
| `model` | `string` | — | ✅ | 使用的模型名称，如 `gpt-4o`，**必填** |
| `system_prompt` | `string` | `""` | ✅ | 系统提示词 |
| `tools` | `[]string` | `[]` | ❌ | 可用 Tool 名称列表 |
| `skills` | `[]string` | `[]` | ❌ | Skill 精确 allowlist；空数组表示不使用 Skill |
| `max_tokens` | `int` | `4096` | ✅ | 单次响应最大 Token 数 |
| `temperature` | `*float64` | `nil`（用模型默认） | ✅ | 采样温度，范围 0.0–2.0 |
| `memory` | `*MemoryOverride` | `nil` | 见说明 | Agent 级 pointer override；字段、合并和重启边界见 [Memory 配置](../memory/config-ref.md) |
| `session` | `*SessionOverride` | `nil` | ✅ | Agent 级 Session pointer override；只影响之后新建的 Session |
| `context` | `*ContextOverride` | `nil` | ✅ | Agent 级 Context pointer override；省略字段继承根配置 |
| `planner` | `*PlannerConfig` | `nil` | ❌ | Agent 级 Planner 配置覆盖；Planner/Executor 在启动时构造 |
| `tools_config` | `map[string]any` | `{}` | ❌ | Agent 级 Tool 配置覆盖；结构在启动时校验并冻结 |
| `skills_config` | `map[string]AgentSkillConfig` | `{}` | ❌ | Agent 级 Skill options 覆盖；key 必须同时出现在 `skills` 中 |

---

## 4. providers 节点

数组类型，每个元素定义一个 LLM Provider。

```go
type ProviderConfig struct {
    ID            string         `yaml:"id"             json:"id"`
    Type          string         `yaml:"type"           json:"type"`
    APIKey        string         `yaml:"api_key"        json:"api_key"`
    BaseURL       string         `yaml:"base_url"       json:"base_url"`
    Timeout       time.Duration  `yaml:"timeout"        json:"timeout"`
    MaxRetries    int            `yaml:"max_retries"    json:"max_retries"`
    RetryInterval time.Duration  `yaml:"retry_interval" json:"retry_interval"`
    Models        []ModelConfig  `yaml:"models"         json:"models"`
    Extra         map[string]any `yaml:"extra"          json:"extra"`
}

type ModelConfig struct {
    ID                string `yaml:"id"                 json:"id"`
    Name              string `yaml:"name"               json:"name"`
    ContextWindow     int    `yaml:"context_window"     json:"context_window"`
    MaxOutput         int    `yaml:"max_output"         json:"max_output"`
    SupportsTools     bool   `yaml:"supports_tools"     json:"supports_tools"`
    SupportsVision    bool   `yaml:"supports_vision"    json:"supports_vision"`
    SupportsStreaming bool   `yaml:"supports_streaming" json:"supports_streaming"`
    SupportsThinking  bool   `yaml:"supports_thinking"  json:"supports_thinking"`
    ThinkingEfforts   []string `yaml:"thinking_efforts" json:"thinking_efforts"`
    MinThinkingBudget int    `yaml:"min_thinking_budget" json:"min_thinking_budget"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `id` | `string` | — | ❌ | Provider 唯一标识，**必填** |
| `type` | `string` | — | ❌ | 内置类型：`openai` / `claude` / `gemini` / `ollama` / `azure`，**必填**；静态链接扩展类型须在 Provider Manager 构造前注册，进程外 Plugin 不扩展 Provider |
| `api_key` | `string` | `""` | ❌ | API 密钥，建议 `${VAR_NAME}` 引用 |
| `base_url` | `string` | 类型相关 | ❌ | API 基础 URL；内置类型解码后必须为非空绝对 HTTP(S) URL |
| `timeout` | `duration` | `120s` | ❌ | 一次逻辑 Chat/Stream 调用的总超时，包含重试、退避和完整 stream 生命周期 |
| `max_retries` | `int` | `3` | ❌ | 可重试 Provider 请求的最大重试次数 |
| `retry_interval` | `duration` | `1s` | ❌ | 指数退避基数 |
| `models` | `[]ModelConfig` | `[]` | ❌ | 模型能力；引用模型必须最终解析出正的窗口和输出上限 |
| `models[].context_window` | `int` | 无 | ❌ | 模型总 Context 窗口，必须 > 0 |
| `models[].max_output` | `int` | 无 | ❌ | 模型最大输出 Token，必须 > 0 且小于窗口 |
| `models[].supports_thinking` | `bool` | `false` | ❌ | 是否支持 Thinking |
| `models[].thinking_efforts` | `[]string` | `[]` | ❌ | 支持的 effort 档位 |
| `models[].min_thinking_budget` | `int` | `0` | ❌ | 最小 Thinking budget，0 表示无额外下限 |
| `extra` | `map[string]any` | `{}` | ❌ | 唯一允许的 Provider 特有扩展配置 |

**各类型默认 `base_url`：**

| Type | 默认 base_url | 需要 api_key |
|------|---------------|:------------:|
| `openai` | `https://api.openai.com/v1` | ✅ |
| `claude` | `https://api.anthropic.com` | ✅ |
| `gemini` | `https://generativelanguage.googleapis.com` | ✅ |
| `ollama` | `http://localhost:11434` | ❌ |
| `azure` | 无，必须显式配置 | ✅ |

---

## 5. mcp 节点

```go
type MCPConfig struct {
    Servers   []MCPServerConfig `yaml:"servers" json:"servers"`
    Server    MCPExposeConfig   `yaml:"server" json:"server"`
    Timeout   MCPTimeoutConfig  `yaml:"timeout" json:"timeout"`
    Reconnect MCPReconnectConfig `yaml:"reconnect" json:"reconnect"`
}

type MCPServerConfig struct {
    Name      string            `yaml:"name"      json:"name"`
    Command   string            `yaml:"command"   json:"command"`
    Args      []string          `yaml:"args"      json:"args"`
    Env       map[string]string `yaml:"env"       json:"env"`
    Headers   map[string]string `yaml:"headers"   json:"headers"`
    TLS       MCPTLSConfig      `yaml:"tls"       json:"tls"`
    Transport string            `yaml:"transport" json:"transport"`
    URL       string            `yaml:"url"       json:"url"`
    Timeout   time.Duration     `yaml:"timeout"   json:"timeout"`
    AutoStart bool              `yaml:"auto_start" json:"auto_start"`
}

type MCPTLSConfig struct {
    CAFile string `yaml:"ca_file" json:"ca_file"`
}

type MCPExposeConfig struct {
    Enabled         bool     `yaml:"enabled" json:"enabled"`
    AgentID         string   `yaml:"agent_id" json:"agent_id"`
    Transport       string   `yaml:"transport" json:"transport"`
    Addr            string   `yaml:"addr" json:"addr"`
    Path            string   `yaml:"path" json:"path"`
    MessagesPath    string   `yaml:"messages_path" json:"messages_path"`
    ExposedTools    []string `yaml:"exposed_tools" json:"exposed_tools"`
    OriginAllowlist []string `yaml:"origin_allowlist" json:"origin_allowlist"`
}

type MCPTimeoutConfig struct {
    Connect time.Duration `yaml:"connect" json:"connect"`
    Init    time.Duration `yaml:"init" json:"init"`
    Tool    time.Duration `yaml:"tool" json:"tool"`
}

type MCPReconnectConfig struct {
    Enabled      bool          `yaml:"enabled" json:"enabled"`
    MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts"`
    InitialDelay time.Duration `yaml:"initial_delay" json:"initial_delay"`
    MaxDelay     time.Duration `yaml:"max_delay" json:"max_delay"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `servers` | `[]MCPServerConfig` | `[]` | — | MCP Server 列表 |
| `servers[].name` | `string` | — | ❌ | Server 唯一标识，**必填** |
| `servers[].command` | `string` | — | ❌ | stdio 启动命令；stdio 时必填 |
| `servers[].args` | `[]string` | `[]` | ❌ | 命令参数 |
| `servers[].env` | `map[string]string` | `{}` | ❌ | 子进程环境变量 |
| `servers[].transport` | `string` | `"stdio"` | ❌ | `stdio` / `sse`（legacy）/ `streamable_http` |
| `servers[].url` | `string` | — | ❌ | sse/streamable_http endpoint；对应 transport 时必填 |
| `servers[].headers` | `map[string]string` | `{}` | ❌ | 远程请求头，值支持 `${VAR}`，读回脱敏 |
| `servers[].tls.ca_file` | `string` | — | ❌ | 自定义 CA bundle |
| `servers[].timeout` | `duration` | `0` | ❌ | 可选 Tool hard cap；0 继承 `timeout.tool` |
| `servers[].auto_start` | `bool` | `true` | ❌ | Runtime 启动时是否连接 |
| `server.enabled` | `bool` | `false` | ❌ | 是否启动 Yaa! MCP Server |
| `server.agent_id` | `string` | — | ❌ | enabled 时必填；作为 Tool Manager 的真实 Agent principal |
| `server.transport` | `string` | `stdio` | ❌ | `stdio` / `sse` / `streamable_http` |
| `server.addr` | `string` | `127.0.0.1:9090` | ❌ | 网络 transport 监听地址 |
| `server.path` | `string` | `/mcp` | ❌ | Streamable HTTP endpoint |
| `server.messages_path` | `string` | `/message` | ❌ | legacy SSE POST endpoint |
| `server.exposed_tools` | `[]string` | `[]` | ❌ | 允许对外暴露的完整 canonical Tool 名称；不得重复 |
| `server.origin_allowlist` | `[]string` | `[]` | ❌ | Streamable HTTP 精确 Origin 白名单 |
| `timeout.connect` | `duration` | `10s` | ❌ | 连接超时 |
| `timeout.init` | `duration` | `15s` | ❌ | initialize 超时 |
| `timeout.tool` | `duration` | `0` | ❌ | 可选 Tool hard cap；0 只使用 Tool Manager/caller deadline |
| `reconnect.enabled` | `bool` | `true` | ❌ | 暂时性断线后是否重连 |
| `reconnect.max_attempts` | `int` | `3` | ❌ | 最大重连次数 |
| `reconnect.initial_delay` | `duration` | `1s` | ❌ | 首次重连等待 |
| `reconnect.max_delay` | `duration` | `60s` | ❌ | 指数退避上限，必须不小于 `initial_delay` |

---

## 6. tools 节点

```go
type ToolsConfig struct {
    DefaultTimeout          time.Duration `yaml:"default_timeout" json:"default_timeout"`
    MaxTimeout              time.Duration `yaml:"max_timeout" json:"max_timeout"`
    MaxConcurrent            int           `yaml:"max_concurrent" json:"max_concurrent"`
    MaxConcurrentPerSession  int           `yaml:"max_concurrent_per_session" json:"max_concurrent_per_session"`
    DefaultMaxRetry          int           `yaml:"default_max_retry" json:"default_max_retry"`
    MaxResultTokens          int           `yaml:"max_result_tokens" json:"max_result_tokens"`
    Builtin                  map[string]ToolConfig `yaml:"builtin" json:"builtin"`
}

type ToolConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    Timeout time.Duration `yaml:"timeout" json:"timeout"`
    Options map[string]any `yaml:"options" json:"options"`
}
```

`ToolConfig.Options` 只作为不同内置 Tool 的解码边界。启动 binding 阶段必须先按完整配置路径分别严格校验 root 与 Agent override，再做顶层 key merge，并解码为对应的 `Effective*Options`；未知 key、错误类型或范围错误都聚合为 `ValidationError`。例如未知的 Agent Shell key 必须报告 `agents[2].tools_config.shell.options.<key>`，不能静默忽略。没有 options 的内置 Tool 只接受空 object。

| 字段 | 默认值 | 校验 |
|------|--------|------|
| `default_timeout` | `30s` | `> 0` 且 `<= max_timeout` |
| `max_timeout` | `300s` | `>= default_timeout` |
| `max_concurrent` | `5` | `> 0` |
| `max_concurrent_per_session` | `3` | `> 0` 且 `<= max_concurrent` |
| `default_max_retry` | `1` | `0..10` |
| `max_result_tokens` | `4000` | `> 0` |
| `builtin` | v1 canonical entries | key 必须是下表中的内置配置键 |

v1 的 `builtin` 配置键固定为：`shell`、`http`、`file`、`config_query`、
`config_reload`、`runtime_status`、`agent_list`、`agent_inspect`、
`session_list`、`session_inspect`、`tool_list`、`skill_list`、
`provider_list` 和 `mcp_list`。其中 `file` 是 `file_read`、`file_write`、
`file_list`、`file_delete` 四个注册 Tool 共享的配置组；其余键与注册 Tool
一一对应。配置组未出现时使用内置默认值；没有 `options` 的键只接受空 object。

### 6.1 tools.builtin.shell

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 Shell Tool |
| `timeout` | `duration` | `30s` | ✅ | 命令执行超时 |
| `options.allowed_commands` | `[]string` | `[]`（全部允许） | ✅ | 允许执行的命令白名单 |
| `options.blocked_commands` | `[]string` | `[]` | ✅ | 禁止执行的命令前缀；优先于 allowlist |
| `options.working_dir` | `string` | `"."` | ✅ | 工作目录；相对主配置文件目录解析 |
| `options.env` | `map[string]string` | `{}` | ✅ | 子进程环境覆盖 |
| `options.max_output_bytes` | `int` | `65536` | ✅ | stdout 与 stderr 合并后的字节上限 |

### 6.2 tools.builtin.http

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 HTTP Tool |
| `timeout` | `duration` | `30s` | ✅ | 请求超时 |
| `options.allowed_hosts` | `[]string` | `[]`（全部允许） | ✅ | 允许的 URL hostname 精确值 |
| `options.blocked_hosts` | `[]string` | `[]` | ✅ | 禁止的 URL hostname 精确值；优先于 allowlist |
| `options.max_redirects` | `int` | `5` | ✅ | 最大重定向次数；每跳重新校验 host |
| `options.max_response_bytes` | `int` | `1048576` (1MB) | ✅ | 最大响应体大小（字节） |

### 6.3 tools.builtin.file

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 File Tool |
| `options.allowed_paths` | `[]string` | `[]` | ✅ | 允许访问的绝对路径前缀；空表示不额外限制 |
| `options.blocked_paths` | `[]string` | `[]` | ✅ | 禁止访问的路径前缀，优先于 allowed_paths |
| `options.max_file_size` | `string` | `"10MB"` | ✅ | 单次读写最大文件大小 |

---

## 7. skills 节点

```go
type SkillsConfig struct {
    Dir      string            `yaml:"dir"       json:"dir"`
    PerSkill map[string]SkillItemConfig `yaml:"per_skill" json:"per_skill"`
}

type SkillItemConfig struct {
    Enabled bool           `yaml:"enabled" json:"enabled"`
    Options map[string]any `yaml:"options"  json:"options"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `dir` | `string` | `"./skills"` | ❌ | Skill 文件扫描目录 |
| `per_skill` | `map[string]SkillItemConfig` | `{}` | — | 按名称对单个 Skill 的配置覆盖 |
| `per_skill.<name>.enabled` | `bool` | `true` | ❌ | 是否启用该 Skill；改变已加载集合需要重启 |
| `per_skill.<name>.options` | `map[string]any` | `{}` | ❌ | 传入 Skill 的自定义配置；v1 在加载时冻结 |

---

## 8. memory 节点

```go
type MemoryConfig struct {
    Enabled         bool                  `yaml:"enabled"          json:"enabled"`
    MaxItems        int                   `yaml:"max_items"        json:"max_items"`
    DefaultTTL      time.Duration         `yaml:"default_ttl"      json:"default_ttl"`
    ExpireInterval  time.Duration         `yaml:"expire_interval"  json:"expire_interval"`
    ExpireBatchSize int                   `yaml:"expire_batch_size" json:"expire_batch_size"`
    EvictionPolicy  string                `yaml:"eviction_policy"  json:"eviction_policy"`
    Storage         MemoryStorageConfig   `yaml:"storage"          json:"storage"`
    Vector          MemoryVectorConfig    `yaml:"vector"           json:"vector"`
    Embedding       MemoryEmbeddingConfig `yaml:"embedding"        json:"embedding"`
}

type MemoryStorageConfig struct {
    Type string `yaml:"type" json:"type"`
    Path string `yaml:"path" json:"path"`
}

type MemoryVectorConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
    TopK int `yaml:"top_k" json:"top_k"`
    FallbackToKeyword bool `yaml:"fallback_to_keyword" json:"fallback_to_keyword"`
}

type MemoryEmbeddingConfig struct {
    Provider string `yaml:"provider" json:"provider"`
    Model string `yaml:"model" json:"model"`
    APIKey string `yaml:"api_key" json:"api_key"`
    BaseURL string `yaml:"base_url" json:"base_url"`
    Dimension int `yaml:"dimension" json:"dimension"`
    Timeout time.Duration `yaml:"timeout" json:"timeout"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 Memory 系统 |
| `max_items` | `int` | `10000` | ✅ | 每个 Agent 最大记忆条目数 |
| `default_ttl` | `duration` | `0` | ✅ | 新 Put 的默认 TTL；0 永不过期 |
| `expire_interval` | `duration` | `5m` | ✅ | 全局过期清理周期 |
| `expire_batch_size` | `int` | `500` | ✅ | 每个清理 batch 上限 |
| `eviction_policy` | `string` | `"fifo"` | ✅ | `fifo` / `ttl` |
| `storage.type` | `string` | `"sqlite"` | ❌ | ContentStore：`sqlite` / `memory` |
| `storage.path` | `string` | `"./data/yaa-memory.db"` | ❌ | SQLite 文件路径 |
| `vector.*` | `MemoryVectorConfig` | 见 Memory 配置 | ❌ | exact cosine 检索策略；改变需重启 |
| `embedding.*` | `MemoryEmbeddingConfig` | 见 Memory 配置 | ❌ | OpenAI-compatible embedding 连接 |

字段范围、Agent pointer override 和合并规则以 [Memory 配置参考](../memory/config-ref.md) 为准；不得在其他配置文档引入同义字段。

---

## 9. Session / Context / Planner / Plugins 节点

这些子系统的完整配置都是 `Config` 根节点，不能放入 `runtime`。Session、Context、Planner 和 Memory 允许在 Agent 条目中使用各自的 pointer override。下表链接是这些委托 DTO 的唯一字段、默认值和合并规则 owner；本文件只固定它们在 `Config` 中的根位置。

| 根节点 | Go 类型 | 权威字段定义 |
|--------|---------|--------------|
| `session` | `SessionConfig` | [Session 配置参考](../session/config-ref.md) |
| `context` | `ContextConfig` | [Context 配置参考](../context/config-ref.md) |
| `planner` | `PlannerConfig` | [Planner 配置参考](../planner/config-ref.md) |
| `plugins` | `PluginsConfig` | [Plugin 配置参考](../plugin/config-ref.md) |

---

## 10. log 节点

```go
type LogConfig struct {
    Level  string `yaml:"level"  json:"level"`
    Format string `yaml:"format" json:"format"`
    Output string `yaml:"output" json:"output"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `level` | `string` | `"info"` | ✅ | 日志级别：`debug` / `info` / `warn` / `error` |
| `format` | `string` | `"text"` | ❌ | 日志格式：`text` / `json` |
| `output` | `string` | `"stderr"` | ❌ | 输出目标：`stderr` / `stdout` / 文件路径 |

---

## 11. 完整配置示例

```yaml
# yaa.yaml — 完整配置示例
config_version: "1.0"

runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
  api:
    http:
      addr: "127.0.0.1:8080"
      read_timeout: 30s
      write_timeout: 30s
      max_header_bytes: 1048576
    ws:
      enabled: true
    sse:
      enabled: true
  auth:
    enabled: true
    tokens:
      - name: "default"
        token: "${YAA_AUTH_TOKEN}"
        roles: ["admin"]

agents:
  - id: "default"
    name: "Default Agent"
    provider: "openai"
    model: "gpt-4o"
    system_prompt: "You are a helpful assistant."
    tools: ["shell", "http", "file_read", "file_write", "file_list", "file_delete"]
    skills: ["weather"]
    max_tokens: 4096
    temperature: 0.7
    memory:
      enabled: true
      max_items: 10000
    session:
      ttl: 8h
      max_lifetime: 168h
    tools_config:
      shell:
        timeout: 60s

providers:
  - id: "openai"
    type: "openai"
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    timeout: 120s
    max_retries: 3
    retry_interval: 1s
    models:
      - id: "gpt-4o"
        name: "GPT-4o"
        context_window: 128000
        max_output: 16384
        supports_tools: true
        supports_vision: true
        supports_streaming: true
  - id: "ollama"
    type: "ollama"
    base_url: "http://localhost:11434"
    timeout: 300s

mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"
  server:
    enabled: false
    agent_id: "default"
    transport: "stdio"
    addr: "127.0.0.1:9090"
    path: "/mcp"
    messages_path: "/message"
    exposed_tools: []

tools:
  builtin:
    shell:
      enabled: true
      timeout: 30s
    http:
      enabled: true
      timeout: 30s
      options:
        max_response_bytes: 1048576
    file:
      enabled: true
      options:
        allowed_paths: ["/srv/yaa/workspace"]
        blocked_paths: []
        max_file_size: "10MB"

skills:
  dir: "./skills"
  per_skill:
    weather:
      enabled: true
      options:
        default_unit: "celsius"

memory:
  enabled: true
  max_items: 10000
  default_ttl: 0
  expire_interval: 5m
  expire_batch_size: 500
  eviction_policy: fifo
  storage:
    type: sqlite
    path: ./data/yaa-memory.db
  vector:
    enabled: false
    similarity_threshold: 0.7
    top_k: 10
    fallback_to_keyword: true
  embedding:
    provider: openai-compatible
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    dimension: 1536
    timeout: 30s

session:
  max_messages: 1000
  max_message_bytes: 10485760
  ttl: 24h
  max_lifetime: 720h
  persist: true
  max_sessions_per_agent: 100
  cleanup_interval: 1m

context:
  max_tokens: 0
  reserved_tokens: 4096
  strategy: "hybrid"
  compression:
    enabled: true
    threshold: 0.85
    target_ratio: 0.60
    min_messages: 6
    preserve_recent: 3
    timeout: 20s

planner:
  type: "llm"
  max_concurrent: 4
  timeout: 30s

plugins:
  paths: ["./plugins"]
  auto_start: true
  entries: []

log:
  level: "info"
  format: "text"
  output: "stderr"
```

---

## 12. 配置路径速查表

| 路径 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `config_version` | string | `"1.0"` | 配置版本号 |
| `runtime.storage.type` | string | `sqlite` | 存储后端 |
| `runtime.storage.path` | string | `./data/yaa.db` | 存储路径 |
| `runtime.api.http.addr` | string | `127.0.0.1:8080` | HTTP 地址 |
| `runtime.api.ws.enabled` | bool | `true` | WS 开关 |
| `runtime.api.sse.enabled` | bool | `true` | SSE 开关 |
| `runtime.auth.enabled` | bool | `false` | 认证开关 |
| `agents[0].model` | string | — | 模型名称 |
| `agents[0].max_tokens` | int | `4096` | 最大 Token |
| `agents[0].temperature` | float | 模型默认 | 温度 |
| `providers[0].type` | string | — | Provider 类型 |
| `providers[0].api_key` | string | — | API 密钥 |
| `providers[0].timeout` | duration | `120s` | 超时 |
| `mcp.servers[0].transport` | string | `stdio` | 传输协议 |
| `tools.builtin.shell.timeout` | duration | `30s` | Shell 超时 |
| `tools.builtin.http.timeout` | duration | `30s` | HTTP 超时 |
| `tools.builtin.file.options.allowed_paths` | []string | `[]` | 文件访问白名单 |
| `skills.dir` | string | `./skills` | Skill 目录 |
| `memory.storage.type` | string | `sqlite` | Memory ContentStore 后端 |
| `memory.max_items` | int | `10000` | 每个 Agent 最大条目 |
| `session.max_messages` | int | `1000` | 单 Session 最大消息数 |
| `session.max_message_bytes` | int | `10485760` | 单条 Provider message 最大 JSON 字节数 |
| `session.ttl` | duration | `24h` | 空闲达到 TTL 后暂停，0 禁用 |
| `session.max_lifetime` | duration | `720h` | 达到后关闭，0 禁用 |
| `session.persist` | bool | `true` | 新 Session 是否持久化 |
| `context.max_tokens` | int | `0` | 0 表示使用目标模型窗口 |
| `planner.type` | string | `llm` | Planner 实现类型 |
| `plugins.paths` | []string | `["./plugins"]` | Plugin 搜索目录 |
| `log.level` | string | `info` | 日志级别 |
| `log.format` | string | `text` | 日志格式 |

---

## 13. 类型说明

| 类型 | Go 类型 | 示例 | 说明 |
|------|---------|------|------|
| `string` | `string` | `"hello"` | 字符串 |
| `bool` | `bool` | `true` | 布尔值 |
| `int` | `int` | `4096` | 整数 |
| `float` | `float64` | `0.7` | 浮点数 |
| `duration` | `time.Duration` | `30s`, `2m`, `1h` | Go 时间 duration 字符串 |
| `[]string` | `[]string` | `["a", "b"]` | 字符串数组 |
| `map[string]any` | `map[string]any` | `{key: value}` | 任意键值对 |
| `*float64` | `*float64` | `0.7` 或 `null` | 可空浮点指针，`null` 表示使用模型默认值 |

---

*最后更新: 2026-07-22*
