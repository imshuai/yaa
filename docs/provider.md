# Provider 层设计

> 本文件描述 Yaa! 的 LLM Provider 层设计。
> Provider 层是 Runtime 的最底层能力，抽象所有 LLM 访问，屏蔽各厂商差异。

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| **统一接口** | 所有 Provider 遵循同一接口，上层无需关心具体厂商 |
| **Provider Independent** | 新增 Provider 不影响 Runtime，仅需实现接口 + 配置注册 |
| **流式优先** | 原生支持流式输出，非流式作为流式的聚合 |
| **自动重试** | 内置重试与故障转移，应对网络波动与限流 |
| **配置驱动** | 通过配置文件注册 Provider，无需修改代码 |
| **零 CGO** | 纯 Go 实现，保证 Windows 7 兼容与交叉编译 |

---

## 2. 核心接口

### 2.1 Provider 接口

```go
// Provider 是所有 LLM 提供商的统一抽象。
// 每个 Provider 实例对应一个具体的 LLM 服务端点。
type Provider interface {
    // ID 返回 Provider 的唯一标识符。
    ID() string

    // Type 返回 Provider 类型（如 "openai"、"ollama"）。
    Type() string

    // Chat 发送非流式对话请求，返回完整响应。
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

    // StreamChat 发送流式对话请求，通过 channel 逐步返回 ChatChunk。
    // 当所有 chunk 发送完毕或发生错误时，channel 关闭。
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)

    // Models 返回该 Provider 支持的模型列表。
    Models() []ModelInfo

    // Close 释放 Provider 持有的资源（如 HTTP 连接池）。
    Close() error
}
```

### 2.2 Provider Manager

```go
// Manager 管理所有已注册的 Provider 实例。
type Manager struct {
    providers map[string]Provider   // key = Provider ID
    mu        sync.RWMutex
}

// NewManager 根据配置创建并初始化所有 Provider。
func NewManager(configs []ProviderConfig) (*Manager, error)

// Register 注册一个 Provider 实例。
func (m *Manager) Register(p Provider) error

// Get 根据 ID 获取 Provider。
func (m *Manager) Get(id string) (Provider, error)

// List 返回所有已注册 Provider 的信息。
func (m *Manager) List() []ProviderInfo

// Unregister 注销一个 Provider。
func (m *Manager) Unregister(id string) error

// Close 关闭所有 Provider。
func (m *Manager) Close() error
```

---

## 3. 请求/响应类型

### 3.1 ChatRequest

```go
// ChatRequest 是发送给 Provider 的统一对话请求。
type ChatRequest struct {
    Model       string         // 模型名称（如 "gpt-4o"）
    Messages    []Message      // 对话消息列表
    Temperature *float64       // 温度参数，nil 表示使用模型默认值
    TopP        *float64       // Top-P 采样，nil 表示使用模型默认值
    MaxTokens   *int           // 最大生成 Token 数，nil 表示不限制
    Stop        []string       // 停止序列
    Stream      bool           // 是否流式（StreamChat 内部自动置 true）
    Tools       []ToolDef      // 可用的 Tool 定义（Function Calling）
    ToolChoice  *ToolChoice    // Tool 选择策略
    ResponseFormat *ResponseFormat // 响应格式控制（如 JSON mode）
    Extra       map[string]any // Provider 特有的扩展参数
}
```

### 3.2 Message

```go
// Message 表示一条对话消息。
type Message struct {
    Role       string         // "system" | "user" | "assistant" | "tool"
    Content    string         // 文本内容
    Name       string         // 发送者名称（可选，用于区分多角色）
    ToolCalls  []ToolCall     // 助手发起的 Tool 调用（Role="assistant" 时）
    ToolCallID string         // 对应的 Tool 调用 ID（Role="tool" 时）
    Refusal    string         // 模型拒绝回答时的说明（可选）
}
```

### 3.3 ToolDef / ToolCall / ToolChoice

```go
// ToolDef 描述一个可供 LLM 调用的 Tool。
type ToolDef struct {
    Type     string         // 固定 "function"
    Function ToolFunction
}

type ToolFunction struct {
    Name        string         // Tool 名称
    Description string         // Tool 描述
    Parameters  json.RawMessage // JSON Schema 参数定义
}

// ToolCall 表示 LLM 决定调用某个 Tool。
type ToolCall struct {
    ID       string         // 调用 ID，用于关联 Tool 结果
    Type     string         // 固定 "function"
    Function ToolCallFunction
}

type ToolCallFunction struct {
    Name      string         // 调用的 Tool 名称
    Arguments string         // JSON 格式的参数字符串
}

// ToolChoice 控制 Tool 调用行为。
type ToolChoice struct {
    Mode  string  // "auto" | "none" | "required" | "specific"
    Tool  string  // 当 Mode="specific" 时指定 Tool 名称
}
```

### 3.4 ChatResponse

```go
// ChatResponse 是非流式对话的完整响应。
type ChatResponse struct {
    ID      string         // 响应 ID（由 Provider 生成）
    Model   string         // 实际使用的模型名称
    Content string         // 文本内容
    Role    string         // 固定 "assistant"
    ToolCalls []ToolCall   // 模型发起的 Tool 调用
    FinishReason string     // "stop" | "length" | "tool_calls" | "content_filter"
    Usage   Usage          // Token 使用统计
}

// Usage 统计 Token 使用量。
type Usage struct {
    PromptTokens     int   // 输入 Token 数
    CompletionTokens int   // 输出 Token 数
    TotalTokens      int   // 总 Token 数
}
```

### 3.5 ChatChunk

```go
// ChatChunk 是流式对话中的一个增量片段。
type ChatChunk struct {
    ID           string       // 响应 ID（同一流式响应中保持一致）
    Model        string       // 模型名称
    Delta        Delta        // 增量内容
    FinishReason string       // 结束原因（仅最后一个 chunk 非空）
    Usage        *Usage       // Token 统计（仅最后一个 chunk 携带）
    Error        error        // 流式过程中的错误
}

// Delta 描述本次增量。
type Delta struct {
    Role      string     // 仅第一个 chunk 携带，通常为 "assistant"
    Content   string     // 增量文本
    ToolCalls []ToolCall // 增量 Tool 调用（流式 Tool Calling）
}
```

### 3.6 ModelInfo

```go
// ModelInfo 描述一个可用模型。
type ModelInfo struct {
    ID          string   // 模型 ID（传给 ChatRequest.Model）
    Name        string   // 模型显示名称
    ContextWindow int    // 上下文窗口大小（Token 数）
    MaxOutput     int     // 最大输出 Token 数
    SupportsTools    bool // 是否支持 Function Calling
    SupportsVision   bool // 是否支持视觉输入
    SupportsStreaming bool // 是否支持流式输出
}
```

---

## 4. Provider 配置

### 4.1 配置结构

```yaml
providers:
  - id: "openai"                    # 唯一标识，用于 Agent 引用
    type: "openai"                  # Provider 类型，决定使用哪个实现
    api_key: "${OPENAI_API_KEY}"    # API Key，支持环境变量引用
    base_url: "https://api.openai.com/v1"  # API 基础地址
    timeout: 120s                   # 请求超时
    max_retries: 3                  # 最大重试次数
    retry_interval: 1s              # 重试间隔（指数退避基数）
    models:                          # 可选：显式声明支持的模型
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
    timeout: 300s                    # 本地模型可能较慢
    max_retries: 1

  - id: "deepseek"
    type: "openai"                   # DeepSeek 兼容 OpenAI API
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: "https://api.deepseek.com/v1"
    timeout: 120s
```

### 4.2 ProviderConfig

```go
// ProviderConfig 是配置文件中 Provider 的结构。
type ProviderConfig struct {
    ID            string            `yaml:"id"`
    Type          string            `yaml:"type"`
    APIKey        string            `yaml:"api_key"`
    BaseURL       string            `yaml:"base_url"`
    Timeout       Duration          `yaml:"timeout"`
    MaxRetries    int               `yaml:"max_retries"`
    RetryInterval Duration          `yaml:"retry_interval"`
    Models        []ModelConfig     `yaml:"models"`
    Extra         map[string]any    `yaml:"extra"`  // Provider 特有配置
}

type ModelConfig struct {
    ID               string `yaml:"id"`
    Name             string `yaml:"name"`
    ContextWindow    int    `yaml:"context_window"`
    MaxOutput         int    `yaml:"max_output"`
    SupportsTools    bool   `yaml:"supports_tools"`
    SupportsVision   bool   `yaml:"supports_vision"`
    SupportsStreaming bool   `yaml:"supports_streaming"`
}
```

### 4.3 配置规则

1. **ID 全局唯一**：重复 ID 在启动时报错
2. **Type 决定实现**：Manager 根据 Type 选择对应的 Provider 构造函数
3. **API Key 环境变量**：`${VAR_NAME}` 语法在加载时解析替换
4. **Models 可选**：不配置时由 Provider 运行时动态查询（如 OpenAI `/models` 端点）
5. **Extra 透传**：未识别的配置项通过 `Extra` 传递给 Provider 实现，用于厂商特有参数

---

## 5. Provider 类型

### 5.1 支持的类型

| Type | 说明 | API 风格 |
|------|------|----------|
| `openai` | OpenAI 官方及兼容服务 | OpenAI Chat Completions |
| `claude` | Anthropic Claude | Anthropic Messages API |
| `gemini` | Google Gemini | Google Generative AI API |
| `ollama` | Ollama 本地模型 | Ollama REST API |
| `lmstudio` | LM Studio 本地模型 | OpenAI 兼容 |
| `azure` | Azure OpenAI | Azure OpenAI API |
| `openrouter` | OpenRouter 聚合 | OpenAI 兼容 |
| `qwen` | 阿里通义千问 | OpenAI 兼容 |
| `deepseek` | DeepSeek | OpenAI 兼容 |
| `custom` | 自定义 Provider | 由插件实现 |

### 5.2 OpenAI 兼容策略

许多 Provider（DeepSeek、Qwen、OpenRouter、LM Studio 等）兼容 OpenAI API 格式。

对于这些 Provider，`type` 设为 `openai`，仅修改 `base_url` 和 `api_key` 即可。

```yaml
# DeepSeek 使用 OpenAI 兼容 API
- id: "deepseek"
  type: "openai"
  api_key: "${DEEPSEEK_API_KEY}"
  base_url: "https://api.deepseek.com/v1"
```

这样避免为每个兼容厂商编写重复代码，减少维护成本。

### 5.3 非 OpenAI 兼容的 Provider

Claude 和 Gemini 有各自的 API 格式，需要独立实现，但对外暴露统一的 `Provider` 接口。

内部通过 **Adapter 模式** 转换：

```text
ChatRequest (统一格式)
  → Provider 实现内部转换
  → 厂商 API 请求格式
  → 调用厂商 API
  → 厂商 API 响应格式
  → Provider 实现内部转换
  → ChatResponse / ChatChunk (统一格式)
```

---

## 6. Provider 实现规范

### 6.1 目录结构

```text
internal/provider/
├── provider.go              # Provider 接口定义
├── manager.go               # Provider Manager
├── types.go                 # ChatRequest / ChatResponse / ChatChunk 等
├── errors.go                # Provider 错误定义
├── registry.go              # Provider 类型注册表
├── retry.go                 # 重试逻辑
└── providers/               # 各厂商实现
    ├── openai.go            # OpenAI 及兼容
    ├── claude.go            # Anthropic Claude
    ├── gemini.go            # Google Gemini
    ├── ollama.go            # Ollama
    ├── azure.go             # Azure OpenAI
    └── ...
```

### 6.2 注册机制

```go
// registry.go

// ProviderFactory 是创建 Provider 实例的工厂函数。
type ProviderFactory func(cfg ProviderConfig) (Provider, error)

var registry = map[string]ProviderFactory{}

// Register 注册一个 Provider 类型的工厂函数。
func Register(typeName string, factory ProviderFactory) {
    registry[typeName] = factory
}

// Create 根据 Type 创建 Provider 实例。
func Create(cfg ProviderConfig) (Provider, error) {
    factory, ok := registry[cfg.Type]
    if !ok {
        return nil, fmt.Errorf("unknown provider type: %s", cfg.Type)
    }
    return factory(cfg)
}

// init() 中注册内置 Provider
func init() {
    Register("openai", NewOpenAIProvider)
    Register("claude", NewClaudeProvider)
    Register("gemini", NewGeminiProvider)
    Register("ollama", NewOllamaProvider)
    Register("azure", NewAzureProvider)
}
```

### 6.3 实现检查清单

每个 Provider 实现需要满足：

- [ ] 实现 `Provider` 接口的全部方法
- [ ] 正确处理 `ctx.Done()`，支持请求取消
- [ ] 流式输出通过 channel 返回 `ChatChunk`，结束时关闭 channel
- [ ] 错误通过 `ChatChunk.Error` 传递（流式）或返回值传递（非流式）
- [ ] Token 使用统计尽可能准确填充 `Usage`
- [ ] Tool Calling 正确映射为 `ToolCall` 结构
- [ ] `Models()` 返回模型列表（动态查询或配置静态列表）
- [ ] `Close()` 释放 HTTP 连接等资源

---

## 7. 重试与故障转移

### 7.1 重试策略

```go
// RetryConfig 控制重试行为。
type RetryConfig struct {
    MaxRetries    int           // 最大重试次数（默认 3）
    InitialDelay  time.Duration // 首次重试延迟（默认 1s）
    MaxDelay      time.Duration // 最大重试延迟（默认 30s）
    JitterFactor  float64       // 抖动因子（0~1，默认 0.1）
}
```

**重试触发条件：**

| 条件 | 是否重试 | 说明 |
|------|----------|------|
| 网络超时 | ✅ | 可能是临时网络问题 |
| HTTP 429 (Rate Limit) | ✅ | 读取 `Retry-After` 头 |
| HTTP 500/502/503/504 | ✅ | 服务端临时错误 |
| HTTP 401/403 | ❌ | 认证错误，重试无意义 |
| HTTP 400 | ❌ | 请求格式错误 |
| Context 取消 | ❌ | 客户端主动取消 |
| 其他错误 | ❌ | 未知错误不盲目重试 |

**退避算法：** 指数退避 + 随机抖动

```text
delay = min(InitialDelay * 2^attempt, MaxDelay) * (1 ± JitterFactor)
```

### 7.2 故障转移

当一个 Provider 有多个端点（如 Azure 多区域），可配置备用端点：

```yaml
- id: "azure"
  type: "azure"
  api_key: "${AZURE_API_KEY}"
  endpoints:
    - base_url: "https://eastus.api.cognitive.microsoft.com"
      deployment: "gpt-4o"
    - base_url: "https://westus.api.cognitive.microsoft.com"
      deployment: "gpt-4o"
  failover: true              # 启用故障转移
```

主端点失败且重试耗尽后，自动切换到下一个端点。

---

## 8. 流式协议

### 8.1 流式输出流程

```text
Agent 调用 StreamChat()
  │
  ▼
Provider 实现内部：
  1. 构造厂商 API 请求（含 stream=true）
  2. 发送 HTTP 请求
  3. 读取 SSE 流（或厂商流式格式）
  4. 逐 chunk 解析 → 转换为 ChatChunk → 写入 channel
  5. 流结束 → 发送最后一个 chunk（含 FinishReason + Usage）
  6. 关闭 channel
  │
  ▼
Agent 从 channel 读取 ChatChunk
  │
  ├─ Content 非空 → 累积文本，实时推送给客户端
  ├─ ToolCalls 非空 → 累积 Tool 调用
  ├─ FinishReason 非空 → 流结束
  └─ Error 非空 → 流错误
```

### 8.2 流式 Tool Calling

流式场景下，ToolCall 的参数是增量分片返回的：

```text
chunk 1: ToolCall{ID: "call_1", Function: {Name: "get_weather", Arguments: ""}}
chunk 2: ToolCall{ID: "call_1", Function: {Name: "", Arguments: "{\"loc"}}
chunk 3: ToolCall{ID: "call_1", Function: {Name: "", Arguments: "ation\":\""}}
chunk 4: ToolCall{ID: "call_1", Function: {Name: "", Arguments: "Beijing\"}"}}
chunk 5: FinishReason: "tool_calls"
```

Provider 实现需要将厂商的增量格式正确映射为 `ChatChunk.Delta.ToolCalls`。

Agent 层负责将增量拼接为完整的 ToolCall。

### 8.3 并发与取消

- `StreamChat` 返回的 channel 必须在以下情况关闭：
  - 正常结束（最后一个 chunk 发送后）
  - 发生错误（发送 `ChatChunk{Error: err}` 后关闭）
  - `ctx.Done()` 触发（发送取消错误后关闭）
- Agent 读取 channel 时应检查 `ctx.Done()`，避免阻塞

---

## 9. 错误处理

### 9.1 错误类型

```go
// ProviderError 是 Provider 层的统一错误类型。
type ProviderError struct {
    Code       ErrorCode   // 错误码
    Message    string      // 错误信息
    StatusCode int         // HTTP 状态码（如适用）
    Retryable  bool        // 是否可重试
    ProviderID string     // 来源 Provider
    Cause      error        // 原始错误
}

type ErrorCode int

const (
    ErrCodeUnauthorized   ErrorCode = iota + 1  // 认证失败
    ErrCodeForbidden                              // 无权限
    ErrCodeRateLimit                              // 限流
    ErrCodeServerError                            // 服务端错误
    ErrCodeTimeout                                // 超时
    ErrCodeInvalidRequest                         // 请求格式错误
    ErrCodeModelNotFound                          // 模型不存在
    ErrCodeContextLengthExceeded                  // 上下文超长
    ErrCodeConnectionRefused                      // 连接被拒
    ErrCodeUnknown                                // 未知错误
)
```

### 9.2 错误映射

| 厂商错误 | 映射到 | Retryable |
|----------|--------|-----------|
| HTTP 401 | `ErrCodeUnauthorized` | ❌ |
| HTTP 403 | `ErrCodeForbidden` | ❌ |
| HTTP 429 | `ErrCodeRateLimit` | ✅ |
| HTTP 500/502/503/504 | `ErrCodeServerError` | ✅ |
| HTTP 400 | `ErrCodeInvalidRequest` | ❌ |
| Context deadline | `ErrCodeTimeout` | ✅ |
| Connection refused | `ErrCodeConnectionRefused` | ✅ |
| Model not found | `ErrCodeModelNotFound` | ❌ |
| Context length exceeded | `ErrCodeContextLengthExceeded` | ❌ |

### 9.3 错误传播

```text
Provider 实现
  → 包装为 ProviderError（含 Code、Retryable、StatusCode）
  → Manager 透传
  → Agent / API 层根据 Retryable 决定是否重试
  → API 层映射为 HTTP 状态码返回给客户端
```

---

## 10. 与其他模块的关系

```text
┌─────────────────────────────┐
│         Agent               │
│   选择 Provider ID + Model   │
└──────────┬──────────────────┘
           │
           ▼
┌─────────────────────────────┐
│     Provider Manager        │
│   Get(id) → Provider 实例    │
└──────────┬──────────────────┘
           │
           ▼
┌─────────────────────────────┐
│      Provider 实现           │
│  Chat() / StreamChat()      │
│  统一格式 ←→ 厂商格式        │
└──────────┬──────────────────┘
           │
           ▼
┌─────────────────────────────┐
│      LLM API (远程/本地)      │
│  OpenAI / Claude / Ollama   │
└─────────────────────────────┘
```

**依赖关系：**

| 方向 | 说明 |
|------|------|
| Agent → Provider Manager | Agent 通过 Provider ID 获取 Provider 实例 |
| Agent → ChatRequest | Agent 构造请求，包含 Messages、Tools 等 |
| Provider → ChatResponse/ChatChunk | Provider 返回统一格式响应 |
| Provider Manager ← Config | 启动时根据配置创建所有 Provider |
| Remote API → Provider Manager | `/providers` 端点查询 Provider 和模型列表 |

**Provider 层不依赖的模块：**
- Agent（Provider 不知道谁在调用它）
- Session / Context（Provider 只接收 Messages 列表）
- Tool / Skill（Provider 只接收 ToolDef 定义，不执行 Tool）
- Memory（Provider 无状态）

---

## 11. 扩展自定义 Provider

### 11.1 内置注册

在 `internal/provider/providers/` 下新建文件，实现 `Provider` 接口，并在 `init()` 中注册：

```go
package providers

import "github.com/imshuai/yaa/internal/provider"

func init() {
    provider.Register("myprovider", NewMyProvider)
}

func NewMyProvider(cfg provider.ProviderConfig) (provider.Provider, error) {
    // 解析配置，创建 HTTP client
    // 返回 Provider 实例
}
```

### 11.2 插件注册（未来）

未来通过 Plugin 系统，支持在不修改源码的情况下注册第三方 Provider：

```go
// 插件通过 init() 或显式调用注册
func RegisterProvider(typeName string, factory ProviderFactory)
```

---

## 12. 设计决策

### PV-001: 统一接口而非多接口

- **决策**：所有 Provider 实现同一个 `Provider` 接口
- **理由**：上层调用方无需关心具体厂商，切换 Provider 零成本

### PV-002: OpenAI 兼容 Provider 复用同一实现

- **决策**：兼容 OpenAI API 的厂商（DeepSeek、Qwen 等）使用 `type: "openai"` + 不同 `base_url`
- **理由**：避免重复代码，减少维护成本，新增兼容厂商只需配置

### PV-003: 流式通过 channel 而非回调

- **决策**：`StreamChat` 返回 `<-chan ChatChunk`
- **理由**：channel 与 Go 并发模型一致，支持 select + ctx 取消，比回调更自然

### PV-004: 非流式不作为流式的简单包装

- **决策**：`Chat` 独立实现，而非 `StreamChat` 的聚合
- **理由**：部分厂商非流式 API 有额外优化（如批量 Token 统计），且非流式更简单高效

### PV-005: Models 动态查询 + 静态配置双模式

- **决策**：`Models()` 优先返回配置中声明的模型列表，未配置时动态查询
- **理由**：本地模型（Ollama）适合动态查询，云服务（OpenAI）适合静态声明以减少 API 调用

### PV-006: 重试在 Provider 层而非 Agent 层

- **决策**：重试逻辑内置在 Provider 实现 / Manager 中
- **理由**：重试与具体厂商的限流策略相关，Provider 层最了解如何正确重试

### PV-007: reasoning_content 作为一等公民

- **决策**：统一数据模型中原生支持 `reasoning_content`（思维链/深度思考内容）
- **理由**：DeepSeek、GLM、MiniMax 等主流国产模型均已支持深度思考模式，思维链内容是 Agent 决策的重要上下文，不能丢弃或当作普通文本处理

---

## 13. 深度思考模式（Thinking / Reasoning）

### 13.1 背景

近年来，主流 LLM 厂商纷纷推出深度思考（Thinking / Reasoning）模式。模型在输出最终回答前，先生成一段思维链（Chain-of-Thought），帮助提升推理质量。

这些厂商虽然大多兼容 OpenAI API 格式，但在思维链相关字段上有各自的处理方式，Yaa! 必须统一抽象。

### 13.2 各厂商特殊字段对比

| 厂商 | 思考开关 | 思考力度 | 响应字段 | 流式增量字段 | 兼容 OpenAI |
|------|----------|----------|----------|-------------|-------------|
| **DeepSeek** | `thinking: {"type": "enabled\|disabled"}`（默认 enabled） | `reasoning_effort: "high\|max"`（默认 high） | `reasoning_content`（与 `content` 同级） | `delta.reasoning_content` | ✅ |
| **GLM (智谱)** | `thinking: {"type": "enabled\|disabled"}`（默认 enabled） | `reasoning_effort: "max\|high"`（默认 max） | `reasoning_content`（与 `content` 同级） | `delta.reasoning_content` | ✅ |
| **MiniMax** | `thinking: {"type": "enabled\|disabled"}` | `reasoning_effort` | `reasoning_content`（与 `content` 同级） | `delta.reasoning_content` | ✅ |
| **Qwen (通义千问)** | `enable_thinking: true\|false` | `thinking_budget: int` | `reasoning_content` | `delta.reasoning_content` | ✅ |
| **OpenAI (o 系列)** | 默认启用 | `reasoning_effort: "low\|medium\|high"` | `reasoning`（结构化对象） | 不流式暴露思维链 | 原生 |
| **Claude (Anthropic)** | `thinking: {"type": "enabled", "budget_tokens": N}` | `budget_tokens` | `thinking` block（content 数组元素） | `thinking_delta` event | ❌ 独立 API |

### 13.3 统一数据模型

在统一类型中新增 `reasoning_content` 字段，使思维链成为一等公民：

```go
// Message 扩展（新增字段）
type Message struct {
    Role             string       // "system" | "user" | "assistant" | "tool"
    Content          string       // 最终回答文本
    ReasoningContent string       // 思维链内容（深度思考模式）
    Name             string       // 发送者名称
    ToolCalls        []ToolCall   // 助手发起的 Tool 调用
    ToolCallID       string       // 对应的 Tool 调用 ID
    Refusal          string       // 拒绝说明
}

// ChatResponse 扩展（新增字段）
type ChatResponse struct {
    ID               string
    Model            string
    Content          string       // 最终回答
    ReasoningContent string       // 思维链内容
    Role             string
    ToolCalls        []ToolCall
    FinishReason     string
    Usage            Usage
}

// ChatChunk 的 Delta 扩展（新增字段）
type Delta struct {
    Role             string
    Content          string       // 最终回答增量
    ReasoningContent string       // 思维链增量
    ToolCalls        []ToolCall
}
```

### 13.4 统一请求参数

在 `ChatRequest` 中新增思维链控制参数：

```go
type ChatRequest struct {
    // ... 原有字段 ...

    // Thinking 控制深度思考模式。
    Thinking *ThinkingConfig
}

// ThinkingConfig 统一控制深度思考行为。
type ThinkingConfig struct {
    Enabled  bool    // 是否启用深度思考
    Effort   string  // 思考力度："low" | "medium" | "high" | "max"
                    // 不同厂商支持档位不同，Provider 实现负责映射
    Budget   *int    // 思考 Token 预算（Claude 的 budget_tokens / Qwen 的 thinking_budget）
                    // nil 表示不限制
}
```

### 13.5 Provider 实现的映射规则

各 Provider 实现负责将统一的 `ThinkingConfig` 转换为厂商特有的请求格式，并将厂商的响应字段映射回统一的 `reasoning_content`：

#### DeepSeek 映射

```text
请求映射:
  Thinking.Enabled = true  → extra_body.thinking = {"type": "enabled"}
  Thinking.Enabled = false → extra_body.thinking = {"type": "disabled"}
  Thinking.Effort           → reasoning_effort (支持 "high", "max")
  注: 思考模式下 temperature/top_p 等参数无效，实现需自动忽略

响应映射:
  message.reasoning_content → Message.ReasoningContent
  delta.reasoning_content   → ChatChunk.Delta.ReasoningContent
```

#### GLM 映射

```text
请求映射:
  Thinking.Enabled = true  → extra_body.thinking = {"type": "enabled"}
  Thinking.Enabled = false → extra_body.thinking = {"type": "disabled"}
  Thinking.Effort           → reasoning_effort (支持 "max", "high")
  注: 默认 max，不设置或设为非 high 值时均为 max

响应映射:
  message.reasoning_content → Message.ReasoningContent
  delta.reasoning_content   → ChatChunk.Delta.ReasoningContent
```

#### MiniMax 映射

```text
请求映射:
  Thinking.Enabled         → extra_body.thinking = {"type": "enabled"/"disabled"}
  Thinking.Effort          → reasoning_effort
  Thinking.Budget          → 思考 Token 预算（如厂商支持）

响应映射:
  message.reasoning_content → Message.ReasoningContent
  delta.reasoning_content   → ChatChunk.Delta.ReasoningContent
```

#### Qwen 映射

```text
请求映射:
  Thinking.Enabled = true  → enable_thinking = true
  Thinking.Enabled = false → enable_thinking = false
  Thinking.Budget          → thinking_budget (int, 思考 Token 数)

响应映射:
  message.reasoning_content → Message.ReasoningContent
  delta.reasoning_content   → ChatChunk.Delta.ReasoningContent
```

#### OpenAI (o 系列) 映射

```text
请求映射:
  Thinking.Effort  → reasoning_effort (支持 "low", "medium", "high")
  注: o 系列不支持关闭思考

响应映射:
  message.reasoning → Message.ReasoningContent (提取结构化对象中的文本)
  注: o 系列不通过流式暴露思维链增量，仅在最终响应中返回
```

#### Claude 映射

```text
请求映射:
  Thinking.Enabled = true  → thinking = {"type": "enabled", "budget_tokens": N}
  Thinking.Budget          → thinking.budget_tokens (必填)
  注: budget_tokens 最小值 1024

响应映射:
  content 数组中 type="thinking" 的元素 → Message.ReasoningContent
  流式 thinking_delta event → ChatChunk.Delta.ReasoningContent
```

### 13.6 Effort 档位映射表

不同厂商支持的思考力度档位不同，`ThinkingConfig.Effort` 统一使用以下档位，Provider 实现负责降级映射：

| 统一 Effort | DeepSeek | GLM | MiniMax | Qwen | OpenAI o 系列 | Claude |
|-------------|----------|-----|---------|------|--------------|--------|
| `low` | → `high` (不支持 low) | → `high` | 支持 | 支持 | `low` | N/A (用 Budget) |
| `medium` | → `high` | → `high` | 支持 | 支持 | `medium` | N/A |
| `high` | `high` | `high` | 支持 | 支持 | `high` | N/A |
| `max` | `max` | `max` | 支持 | 支持 | → `high` (不支持 max) | N/A |

**降级规则：**
- 厂商不支持的档位 → 降级到最接近的可用档位
- Claude 不使用 Effort，使用 `Budget`（Token 数）控制思考深度
- 映射规则在 Provider 实现中硬编码，可通过配置覆盖

### 13.7 多轮对话中的 ReasoningContent 处理

**关键问题：** 部分厂商（如 DeepSeek）在多轮对话中，对 `reasoning_content` 的传递有严格规则。

**DeepSeek 规则：**

| 场景 | reasoning_content 处理 |
|------|----------------------|
| 无 Tool Call 的历史轮次 | 不需要回传，传入也会被忽略 |
| 有 Tool Call 的历史轮次 | **必须**完整回传，否则返回 400 错误 |

**统一处理策略：**

Yaa! 采用保守策略 —— **始终保留并回传 reasoning_content**，由 Provider 实现决定是否在请求中包含：

```go
// Provider 实现在构造请求时的处理逻辑
func (p *DeepSeekProvider) buildMessages(messages []Message) []map[string]any {
    var result []map[string]any
    for _, msg := range messages {
        m := map[string]any{
            "role":    msg.Role,
            "content": msg.Content,
        }
        // 如果有 reasoning_content，始终保留
        if msg.ReasoningContent != "" {
            m["reasoning_content"] = msg.ReasoningContent
        }
        // Tool 相关字段...
        result = append(result, m)
    }
    return result
}
```

**Context Manager 配合：**
- Context Manager 在截断/压缩上下文时，可以丢弃历史轮次的 `reasoning_content`（无 Tool Call 的轮次）
- 但有 Tool Call 的轮次的 `reasoning_content` 必须保留
- 这需要 Context Manager 理解消息的 Tool Call 语义

### 13.8 流式输出的 ReasoningContent 处理

流式场景下，思维链增量通常在最终回答增量之前输出：

```text
chunk 1: Delta.ReasoningContent = "让我分析一下这个问题..."
chunk 2: Delta.ReasoningContent = "首先需要比较两个数字..."
chunk 3: Delta.ReasoningContent = "9.11 的整数部分是 9..."
chunk 4: Delta.ReasoningContent = ""           // 思维链结束
chunk 5: Delta.Content = "9.11"                // 最终回答开始
chunk 6: Delta.Content = " 比 9.8"              
chunk 7: Delta.Content = " 更大。"             
chunk 8: FinishReason = "stop", Usage = {...}  // 结束
```

**Agent 层处理：**
- `Delta.ReasoningContent` 非空 → 累积思维链，可实时推送给客户端（标记为思考内容）
- `Delta.Content` 非空 → 累积最终回答，推送给客户端
- 两者通常不同时出现（先思考后回答），但实现不应假设顺序

**Remote API 透传：**
- SSE / WS 事件中需要区分思维链和最终回答
- 事件类型设计：`reasoning_delta` vs `content_delta`（详见 Remote API 对话文档）

### 13.9 配置示例

```yaml
# Provider 级别配置默认思考行为
providers:
  - id: "deepseek"
    type: "openai"
    api_key: "${DEEPSEEK_API_KEY}"
    base_url: "https://api.deepseek.com/v1"
    thinking:
      default_enabled: true      # 默认启用思考
      default_effort: "high"     # 默认思考力度
      # 注: 思考模式下 temperature/top_p 无效

  - id: "glm"
    type: "openai"
    api_key: "${GLM_API_KEY}"
    base_url: "https://open.bigmodel.cn/api/paas/v4"
    thinking:
      default_enabled: true
      default_effort: "max"      # GLM 默认 max

  - id: "minimax"
    type: "openai"
    api_key: "${MINIMAX_API_KEY}"
    base_url: "https://api.minimax.chat/v1"
    thinking:
      default_enabled: true
      default_effort: "high"

  - id: "qwen"
    type: "openai"
    api_key: "${QWEN_API_KEY}"
    base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
    thinking:
      default_enabled: false     # 默认关闭，按需启用
      default_budget: 4096        # 思考 Token 预算

  - id: "claude"
    type: "claude"
    api_key: "${CLAUDE_API_KEY}"
    base_url: "https://api.anthropic.com"
    thinking:
      default_enabled: true
      default_budget: 10000       # budget_tokens (最小 1024)

# Agent 级别可覆盖 Provider 默认值
agents:
  - id: "reasoning-agent"
    provider: "deepseek"
    model: "deepseek-v4-pro"
    thinking:
      enabled: true
      effort: "max"               # 覆盖 Provider 默认值
```

### 13.10 ModelInfo 扩展

```go
type ModelInfo struct {
    // ... 原有字段 ...

    SupportsThinking    bool     // 是否支持深度思考模式
    ThinkingEfforts     []string // 支持的思考力度档位（如 ["high", "max"]）
    MinThinkingBudget   int      // 最小思考 Token 预算（Claude: 1024）
}
```

Agent 在选择模型时，可通过 `ModelInfo.SupportsThinking` 判断是否支持深度思考，避免对不支持的模型设置 Thinking 参数。

### 13.11 实现检查清单（Thinking 扩展）

Provider 实现在支持深度思考模式时需额外满足：

- [ ] 将 `ThinkingConfig` 正确映射为厂商请求参数
- [ ] 将厂商响应中的思维链字段映射为 `reasoning_content`
- [ ] 流式输出时正确解析 `delta.reasoning_content` 增量
- [ ] 多轮对话中正确处理 `reasoning_content` 的保留/丢弃规则
- [ ] Effort 档位降级映射正确
- [ ] 思考模式下自动忽略不支持的参数（如 temperature）
- [ ] `ModelInfo` 中正确声明 `SupportsThinking` 和 `ThinkingEfforts`
- [ ] 未启用思考模式时不发送相关参数（避免厂商报错）

---

*最后更新: 2025-07-17*
