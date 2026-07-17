# 完整配置参考

> Yaa! Yet Another Agent Runtime
> 文档路径: docs/config/reference.md
> 上级: [README.md](README.md)
> 依赖: [overview.md](overview.md) §3, [architecture.md](../architecture.md) §3.12

---

## 1. 顶层结构

`Config` 是 Yaa! 运行时的完整配置根结构，所有子系统从此结构中读取自己的配置。

```go
type Config struct {
    ConfigVersion string         `yaml:"config_version" json:"config_version"`
    Runtime       RuntimeConfig  `yaml:"runtime"       json:"runtime"`
    Agents        []AgentConfig  `yaml:"agents"        json:"agents"`
    Providers     []ProviderConfig `yaml:"providers"   json:"providers"`
    MCP           MCPConfig      `yaml:"mcp"           json:"mcp"`
    Tools         ToolsConfig    `yaml:"tools"         json:"tools"`
    Skills        SkillsConfig   `yaml:"skills"        json:"skills"`
    Memory        MemoryConfig   `yaml:"memory"        json:"memory"`
    Log           LogConfig      `yaml:"log"           json:"log"`
}
```

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `config_version` | `string` | `"1"` | 配置格式版本号，用于自动迁移 |
| `runtime` | `RuntimeConfig` | 见 §2 | 运行时核心配置（存储、API、认证） |
| `agents` | `[]AgentConfig` | `[]` | Agent 列表，至少一个 |
| `providers` | `[]ProviderConfig` | `[]` | LLM Provider 列表，至少一个 |
| `mcp` | `MCPConfig` | 见 §6 | MCP Server 配置 |
| `tools` | `ToolsConfig` | 见 §7 | Tool 系统配置 |
| `skills` | `SkillsConfig` | 见 §8 | Skill 系统配置 |
| `memory` | `MemoryConfig` | 见 §9 | Memory 系统配置 |
| `log` | `LogConfig` | 见 §10 | 日志配置 |

---

## 2. runtime 节点

```go
type RuntimeConfig struct {
    Storage StorageConfig `yaml:"storage" json:"storage"`
    API     APIConfig     `yaml:"api"     json:"api"`
    Auth    AuthConfig    `yaml:"auth"    json:"auth"`
}
```

### 2.1 runtime.storage

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `type` | `string` | `"sqlite"` | ❌ | 存储后端类型：`sqlite` / `bbolt` / `memory` |
| `path` | `string` | `"./data/yaa.db"` | ❌ | 存储文件路径 |

### 2.2 runtime.api

```go
type APIConfig struct {
    HTTP HTTPConfig `yaml:"http" json:"http"`
    WS   WSConfig   `yaml:"ws"   json:"ws"`
    SSE  SSEConfig  `yaml:"sse"  json:"sse"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `http.addr` | `string` | `":8080"` | ❌ | HTTP 监听地址 |
| `http.read_timeout` | `duration` | `30s` | ✅ | HTTP 读超时 |
| `http.write_timeout` | `duration` | `30s` | ✅ | HTTP 写超时 |
| `http.max_header_bytes` | `int` | `1048576` (1MB) | ✅ | HTTP 最大请求头 |
| `ws.enabled` | `bool` | `true` | ❌ | 是否启用 WebSocket |
| `sse.enabled` | `bool` | `true` | ❌ | 是否启用 SSE |

### 2.3 runtime.auth

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `auth.enabled` | `bool` | `true` | ❌ | 是否启用认证 |
| `auth.tokens` | `[]TokenConfig` | `[]` | ❌ | 静态 Token 列表 |
| `auth.tokens[].name` | `string` | — | — | Token 名称（标识用途） |
| `auth.tokens[].token` | `string` | — | — | Token 值，建议 `${VAR_NAME}` 引用 |

---

## 3. agents 节点

数组类型，每个元素定义一个 Agent 实例。

```go
type AgentConfig struct {
    ID           string            `yaml:"id"            json:"id"`
    Name         string            `yaml:"name"          json:"name"`
    Provider     string            `yaml:"provider"      json:"provider"`
    Model        string            `yaml:"model"         json:"model"`
    SystemPrompt string            `yaml:"system_prompt" json:"system_prompt"`
    Tools        []string          `yaml:"tools"         json:"tools"`
    Skills       []string          `yaml:"skills"        json:"skills"`
    MaxTokens    int               `yaml:"max_tokens"    json:"max_tokens"`
    Temperature  *float64          `yaml:"temperature"   json:"temperature"`
    Memory       AgentMemoryConfig `yaml:"memory"        json:"memory"`
    ToolsConfig  map[string]any    `yaml:"tools_config"  json:"tools_config"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `id` | `string` | — | ❌ | Agent 唯一标识，**必填** |
| `name` | `string` | — | ❌ | Agent 显示名称，**必填** |
| `provider` | `string` | — | ❌ | 绑定的 Provider ID，**必填** |
| `model` | `string` | — | ✅ | 使用的模型名称，如 `gpt-4o` |
| `system_prompt` | `string` | `""` | ✅ | 系统提示词 |
| `tools` | `[]string` | `[]` | ❌ | 可用 Tool 名称列表 |
| `skills` | `[]string` | `[]` | ❌ | 可用 Skill 名称列表 |
| `max_tokens` | `int` | `4096` | ✅ | 单次响应最大 Token 数 |
| `temperature` | `*float64` | `nil`（用模型默认） | ✅ | 采样温度，范围 0.0–2.0 |
| `memory.enabled` | `bool` | `true` | ❌ | 是否为该 Agent 启用记忆 |
| `memory.max_size` | `int` | `1000` | ✅ | 最大记忆条目数 |
| `tools_config` | `map[string]any` | `{}` | ✅ | Agent 级 Tool 配置覆盖，覆盖全局 `tools.builtin.*` |

---

## 4. providers 节点

数组类型，每个元素定义一个 LLM Provider。

```go
type ProviderConfig struct {
    ID      string        `yaml:"id"       json:"id"`
    Type    string        `yaml:"type"     json:"type"`
    APIKey  string        `yaml:"api_key"  json:"api_key"`
    BaseURL string        `yaml:"base_url" json:"base_url"`
    Timeout duration      `yaml:"timeout"  json:"timeout"`
    Models  []string      `yaml:"models"   json:"models"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `id` | `string` | — | ❌ | Provider 唯一标识，**必填** |
| `type` | `string` | — | ❌ | Provider 类型：`openai` / `claude` / `gemini` / `ollama` 等，**必填** |
| `api_key` | `string` | `""` | ✅ | API 密钥，建议 `${VAR_NAME}` 引用 |
| `base_url` | `string` | 类型相关 | ✅ | API 基础 URL |
| `timeout` | `duration` | `120s` | ✅ | 请求超时时间 |
| `models` | `[]string` | `[]` | ✅ | 可用模型列表（空则自动获取） |

**各类型默认 `base_url`：**

| Type | 默认 base_url | 需要 api_key |
|------|---------------|:------------:|
| `openai` | `https://api.openai.com/v1` | ✅ |
| `claude` | `https://api.anthropic.com` | ✅ |
| `gemini` | `https://generativelanguage.googleapis.com` | ✅ |
| `ollama` | `http://localhost:11434` | ❌ |

---

## 5. mcp 节点

```go
type MCPConfig struct {
    Servers []MCPServerConfig `yaml:"servers" json:"servers"`
}

type MCPServerConfig struct {
    Name      string            `yaml:"name"      json:"name"`
    Command   string            `yaml:"command"   json:"command"`
    Args      []string          `yaml:"args"      json:"args"`
    Env       map[string]string `yaml:"env"       json:"env"`
    Transport string            `yaml:"transport" json:"transport"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `servers` | `[]MCPServerConfig` | `[]` | — | MCP Server 列表 |
| `servers[].name` | `string` | — | ❌ | Server 唯一标识，**必填** |
| `servers[].command` | `string` | — | ❌ | 启动命令（如 `npx`），**必填** |
| `servers[].args` | `[]string` | `[]` | ❌ | 命令参数 |
| `servers[].env` | `map[string]string` | `{}` | ❌ | 子进程环境变量 |
| `servers[].transport` | `string` | `"stdio"` | ❌ | 传输协议：`stdio` / `sse` / `websocket` |

---

## 6. tools 节点

```go
type ToolsConfig struct {
    Builtin BuiltinToolsConfig `yaml:"builtin" json:"builtin"`
}

type BuiltinToolsConfig struct {
    Shell ShellToolConfig `yaml:"shell" json:"shell"`
    HTTP  HTTPToolConfig  `yaml:"http"  json:"http"`
    File  FileToolConfig  `yaml:"file"  json:"file"`
}
```

### 6.1 tools.builtin.shell

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 Shell Tool |
| `timeout` | `duration` | `30s` | ✅ | 命令执行超时 |
| `allowed_commands` | `[]string` | `[]`（全部允许） | ✅ | 允许执行的命令白名单 |

### 6.2 tools.builtin.http

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 HTTP Tool |
| `timeout` | `duration` | `30s` | ✅ | 请求超时 |
| `max_response_size` | `int` | `1048576` (1MB) | ✅ | 最大响应体大小（字节） |

### 6.3 tools.builtin.file

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `enabled` | `bool` | `true` | ❌ | 是否启用 File Tool |
| `root` | `string` | `"./"` | ✅ | 文件操作根目录（沙箱限制） |
| `max_size` | `int` | `10485760` (10MB) | ✅ | 单次读写最大文件大小（字节） |

---

## 7. skills 节点

```go
type SkillsConfig struct {
    Dir      string            `yaml:"dir"       json:"dir"`
    PerSkill map[string]SkillItemConfig `yaml:"per_skill" json:"per_skill"`
}

type SkillItemConfig struct {
    Enabled bool           `yaml:"enabled" json:"enabled"`
    Config  map[string]any `yaml:"config"  json:"config"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `dir` | `string` | `"./skills"` | ❌ | Skill 文件扫描目录 |
| `per_skill` | `map[string]SkillItemConfig` | `{}` | — | 按名称对单个 Skill 的配置覆盖 |
| `per_skill.<name>.enabled` | `bool` | `true` | ✅ | 是否启用该 Skill |
| `per_skill.<name>.config` | `map[string]any` | `{}` | ✅ | 传入 Skill 的自定义配置 |

---

## 8. memory 节点

```go
type MemoryConfig struct {
    Backend string `yaml:"backend"  json:"backend"`
    MaxSize int    `yaml:"max_size" json:"max_size"`
}
```

| 字段 | 类型 | 默认值 | 热更新 | 说明 |
|------|------|--------|:------:|------|
| `backend` | `string` | `"sqlite"` | ❌ | 记忆后端：`sqlite` / `bbolt` / `memory` |
| `max_size` | `int` | `1000` | ✅ | 最大记忆条目数（超出时淘汰最旧条目） |

> **注意：** Agent 级 `memory` 配置（§3）覆盖此全局 `memory` 配置。

---

## 9. log 节点

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
| `format` | `string` | `"text"` | ✅ | 日志格式：`text` / `json` |
| `output` | `string` | `"stderr"` | ✅ | 输出目标：`stderr` / `stdout` / 文件路径 |

---

## 10. 完整配置示例

```yaml
# yaa.yaml — 完整配置示例
config_version: "1"

runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
  api:
    http:
      addr: ":8080"
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

agents:
  - id: "default"
    name: "Default Agent"
    provider: "openai"
    model: "gpt-4o"
    system_prompt: "You are a helpful assistant."
    tools: ["shell", "http", "file"]
    skills: ["weather"]
    max_tokens: 4096
    temperature: 0.7
    memory:
      enabled: true
      max_size: 1000
    tools_config:
      shell:
        timeout: 60s

providers:
  - id: "openai"
    type: "openai"
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
    timeout: 120s
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

tools:
  builtin:
    shell:
      enabled: true
      timeout: 30s
    http:
      enabled: true
      timeout: 30s
      max_response_size: 1048576
    file:
      enabled: true
      root: "./"
      max_size: 10485760

skills:
  dir: "./skills"
  per_skill:
    weather:
      enabled: true
      config:
        default_unit: "celsius"

memory:
  backend: "sqlite"
  max_size: 1000

log:
  level: "info"
  format: "text"
  output: "stderr"
```

---

## 11. 配置路径速查表

| 路径 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `config_version` | string | `"1"` | 配置版本号 |
| `runtime.storage.type` | string | `sqlite` | 存储后端 |
| `runtime.storage.path` | string | `./data/yaa.db` | 存储路径 |
| `runtime.api.http.addr` | string | `:8080` | HTTP 地址 |
| `runtime.api.ws.enabled` | bool | `true` | WS 开关 |
| `runtime.api.sse.enabled` | bool | `true` | SSE 开关 |
| `runtime.auth.enabled` | bool | `true` | 认证开关 |
| `agents[0].model` | string | — | 模型名称 |
| `agents[0].max_tokens` | int | `4096` | 最大 Token |
| `agents[0].temperature` | float | 模型默认 | 温度 |
| `providers[0].type` | string | — | Provider 类型 |
| `providers[0].api_key` | string | — | API 密钥 |
| `providers[0].timeout` | duration | `120s` | 超时 |
| `mcp.servers[0].transport` | string | `stdio` | 传输协议 |
| `tools.builtin.shell.timeout` | duration | `30s` | Shell 超时 |
| `tools.builtin.http.timeout` | duration | `30s` | HTTP 超时 |
| `tools.builtin.file.root` | string | `./` | 文件根目录 |
| `skills.dir` | string | `./skills` | Skill 目录 |
| `memory.backend` | string | `sqlite` | 记忆后端 |
| `memory.max_size` | int | `1000` | 最大条目 |
| `log.level` | string | `info` | 日志级别 |
| `log.format` | string | `text` | 日志格式 |

---

## 12. 类型说明

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

*最后更新: 2025-07-17*
