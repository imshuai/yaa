# Provider 层设计

> Provider 是 Runtime 对 LLM 的唯一访问边界。本文是请求、响应、错误和重试所有权的权威契约。

---

## 1. 设计目标

- 所有厂商实现同一个 `Provider` 接口；Agent 不依赖厂商 SDK 类型。
- `Chat` 和 `StreamChat` 分别调用厂商的非流式/流式接口，不互相聚合。
- Provider adapter 只负责协议转换、取消和错误分类；重试由 Manager 的一个 decorator 负责。
- 一个配置项只有一个 `base_url`；v1 不做 Provider 间或区域 failover。
- 依赖使用纯 Go 实现，目标 Go 1.20.x、零 CGO。

---

## 2. 核心接口

### 2.1 Provider 接口

```go
type Provider interface {
    ID() string
    Type() string
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
    EstimateInputTokens(ctx context.Context, req *ChatRequest) (int, error)
    Models() []ModelInfo
    Close() error
}
```

`StreamChat` 的同步错误表示请求尚未建立；返回 channel 后的协议错误通过一个 `ChatChunk{Error: err}` 发送并关闭 channel。调用方在拿到 channel 后必须持续读取或取消 `ctx`。

### 2.2 Provider Manager

```go
type Manager struct {
    providers map[string]Provider              // 已包含 retryingProvider
    configs   map[string]config.ProviderConfig // 启动时只读副本
}

type ProviderInfo struct {
    ID     string      `json:"id"`
    Type   string      `json:"type"`
    Models []ModelInfo `json:"models"`
}

func NewManager(configs []config.ProviderConfig) (*Manager, error)
func (m *Manager) Get(id string) (Provider, error)
func (m *Manager) List() []ProviderInfo
func (m *Manager) Close() error
```

`NewManager` 为每个配置执行 `Create(config)` 得到 adapter，再用 `retryingProvider{inner: adapter, maxRetries, retryInterval}` 包装后存入 map。Provider 集合由配置决定，Manager 没有运行时 `Register`/`Unregister`；额外 adapter 必须静态链接并在 `NewManager` 前注册 factory。`List` 按 ID 排序返回副本，`Close` 按 ID 排序并用 `errors.Join` 聚合关闭错误。

---

## 3. 请求/响应类型

### 3.1 ChatRequest

```go
type ChatRequest struct {
    Model          string          `json:"model"`
    Messages       []Message       `json:"messages"`
    Temperature    *float64        `json:"temperature,omitempty"`
    TopP           *float64        `json:"top_p,omitempty"`
    MaxTokens      *int            `json:"max_tokens,omitempty"`
    Stop           []string        `json:"stop,omitempty"`
    Stream         bool            `json:"stream,omitempty"`
    Tools          []ToolDef       `json:"tools,omitempty"`
    ToolChoice     *ToolChoice     `json:"tool_choice,omitempty"`
    ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
    Thinking       *ThinkingConfig `json:"thinking,omitempty"`
    Extra          map[string]any  `json:"extra,omitempty"`
}

type ThinkingConfig struct {
    Enabled bool   `json:"enabled"`
    Effort  string `json:"effort,omitempty"` // low | medium | high | max
    Budget  *int   `json:"budget,omitempty"`
}

type ResponseFormat struct {
    Type       string          `json:"type"` // text | json_object | json_schema
    Name       string          `json:"name,omitempty"`
    JSONSchema json.RawMessage `json:"json_schema,omitempty"`
}
```

`StreamChat` 内部必须把 `Stream` 设为 true；`Chat` 必须使用非流式请求。`Thinking` 和 `ToolChoice` 是 request-level 字段，不出现在 Agent 或 ProviderConfig。

### 3.1.1 Tool name wire boundary

`ChatRequest` 到达 Provider 时已经由 Agent 按 [Provider-safe Tool alias 契约](tool/provider.md)
完成 turn-local 投影：`Tools[].Function.Name`、历史消息中的 Tool call/name 和
`ToolChoice{Mode:"specific"}.Tool` 都是 provider-safe alias。Provider adapter 必须原样
复制这些字符串到厂商请求，不能自行 trim、normalize、hash、追加后缀或尝试恢复
canonical name。`EstimateInputTokens` 也必须按这份已投影的完整请求估算，Context 不会在
Provider 内部再次改名。

### 3.2 Message

```go
type Message struct {
    Role             string     `json:"role"` // system | user | assistant | tool
    Content          string     `json:"content,omitempty"`
    ReasoningContent string     `json:"reasoning_content,omitempty"`
    Name             string     `json:"name,omitempty"`
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID       string     `json:"tool_call_id,omitempty"`
    Refusal          string     `json:"refusal,omitempty"`
}
```

Session snapshot 通过其 DTO 直接保存此类型的 canonical 形式；Provider wire alias 不得
进入 Session。tags 是契约的一部分，不能依赖 Go 字段名默认编码。

### 3.3 ToolDef / ToolCall / ToolChoice

```go
type ToolDef struct {
    Type     string       `json:"type"` // function
    Function ToolFunction `json:"function"`
}

type ToolFunction struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
}

type ToolCall struct {
    ID       string           `json:"id"`
    Type     string           `json:"type"` // function
    Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON object encoded as string
}

type ToolChoice struct {
    Mode string `json:"mode"`           // auto | none | required | specific
    Tool string `json:"tool,omitempty"` // Mode=specific 时必填
}
```

### 3.4 ChatResponse

```go
type ChatResponse struct {
    ID               string     `json:"id"`
    Model            string     `json:"model"`
    Content          string     `json:"content,omitempty"`
    ReasoningContent string     `json:"reasoning_content,omitempty"`
    Refusal          string     `json:"refusal,omitempty"`
    Role             string     `json:"role"` // assistant
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
    FinishReason     string     `json:"finish_reason"`
    Usage            Usage      `json:"usage"`
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### 3.5 ChatChunk

```go
type ChatChunk struct {
    ID           string `json:"id"`
    Model        string `json:"model"`
    Delta        Delta  `json:"delta"`
    FinishReason string `json:"finish_reason,omitempty"`
    Usage        *Usage `json:"usage,omitempty"`
    Error        error  `json:"-"`
}

type Delta struct {
    Role             string     `json:"role,omitempty"`
    Content          string     `json:"content,omitempty"`
    ReasoningContent string     `json:"reasoning_content,omitempty"`
    Refusal          string     `json:"refusal,omitempty"`
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}
```

一个 chunk 中可以同时出现 reasoning、content、tool call 或 finish 信息；Agent 不得假定固定顺序。Adapter 若上游按数组 index 发送 Tool call fragment，必须在单次响应内维护 `index -> call ID` 并给每个统一 fragment 补稳定非空 ID；协议没有 ID 时，使用与 direct 相同的 response ID + index 派生规则，不能逐 fragment 随机生成。index/ID 冲突发送协议错误。Adapter 不拼接或反查 alias，Agent 在完整 alias 拼接后统一反查，详见 [Tool alias 契约](tool/provider.md#5-provider-响应反查)。Remote 层将 `Delta.Content` 映射为 `assistant_delta`，不得使用 `content_delta`。

### 3.6 ModelInfo

```go
type ModelInfo struct {
    ID                string   `json:"id"`
    Name              string   `json:"name"`
    ContextWindow     int      `json:"context_window"`
    MaxOutput         int      `json:"max_output"`
    SupportsTools     bool     `json:"supports_tools"`
    SupportsVision    bool     `json:"supports_vision"`
    SupportsStreaming bool     `json:"supports_streaming"`
    SupportsThinking  bool     `json:"supports_thinking"`
    ThinkingEfforts   []string `json:"thinking_efforts"`
    MinThinkingBudget int      `json:"min_thinking_budget"`
}
```

被 Agent 引用的模型必须有正的 `ContextWindow` 和 `MaxOutput`；未知能力不能假设为无限。

---

## 4. Provider 配置

配置结构的唯一来源是 [`config.ProviderConfig`](config/reference.md#4-providers-节点)。Provider 包不再定义第二个同名 DTO。

```yaml
providers:
  - id: openai
    type: openai
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    timeout: 120s
    max_retries: 3
    retry_interval: 1s
    models:
      - id: gpt-4o
        context_window: 128000
        max_output: 16384
        supports_tools: true
        supports_streaming: true
```

`extra` 是唯一厂商扩展 map；`failover`、`endpoints`、`providers[].thinking` 和 Agent 级 thinking 都不是 v1 字段。Provider 的 `api_key` 永不进入 Remote DTO。

---

## 5. Provider 类型

### 5.1 内置配置值

| `type` | API 风格 | 默认 base URL |
|--------|----------|---------------|
| `openai` | OpenAI Chat Completions，含 OpenAI-compatible 服务 | `https://api.openai.com/v1` |
| `claude` | Anthropic Messages | `https://api.anthropic.com` |
| `gemini` | Google Generative AI | `https://generativelanguage.googleapis.com` |
| `ollama` | Ollama REST | `http://localhost:11434` |
| `azure` | Azure OpenAI | 无默认值，`base_url` 必填 |

DeepSeek、Qwen、OpenRouter、LM Studio 等 OpenAI-compatible 服务使用 `type: openai` 和各自 `base_url`，不新增别名。进程外 Plugin 不注册 Provider type；静态链接扩展可在 Runtime 构造 Provider Manager 前调用 `Register`，并必须参与同一次启动 binding 校验。

### 5.2 Adapter

每个内置 adapter 只做以下工作：统一请求/响应映射、HTTP/协议取消、ProviderError 分类、ModelInfo 提供。Adapter 不 sleep、不循环重试、不选择其他 Provider。

```go
type ProviderFactory func(config.ProviderConfig) (Provider, error)

func Register(typeName string, factory ProviderFactory) // 仅启动前调用
func Create(cfg config.ProviderConfig) (Provider, error)
```

---

## 6. 重试与故障转移

### 6.1 唯一 owner

重试只存在于 Manager 创建的 `retryingProvider` decorator。Adapter 返回一次分类后的 `ProviderError`，Agent、Planner、Context 和 Tool 都不得再次包 retry loop。

```go
type retryingProvider struct {
    inner         Provider
    timeout       time.Duration // 一次逻辑调用的总 deadline
    maxRetries    int           // 重试次数，不含首次请求
    retryInterval time.Duration // 首次退避基数
}
```

`retryingProvider` 是 `ProviderConfig.timeout` 的唯一 owner。每次 `Chat` 或 `StreamChat` 入口只派生一次 `context.WithTimeout(callerCtx, timeout)`；这个总 deadline 覆盖首次 attempt、全部重试 attempt、退避等待，以及流式 channel 关闭前的完整消费期。caller 已有更早 deadline 时自然优先。Adapter 不再另建配置 timeout；只把该 context 传给 HTTP 请求。`StreamChat` 的派生 cancel 必须由消费 goroutine在外部 channel 关闭时调用，不能在方法返回时提前 cancel。

总尝试次数最多为 `1 + maxRetries`：首次请求的 attempt index 为 0，最后一次可为 `maxRetries`。只有 `ProviderError.Retryable == true` 且逻辑调用 context 未取消时重试。第 `retryIndex` 次重试（从 0 开始）的本地退避为 `min(retryInterval*2^retryIndex, 30s)`；若错误携带正数 `RetryAfter`，实际等待取二者较大值。等待全程 select `ctx.Done()`，不做 failover，单个 Provider 只有一个 `base_url`。总 deadline 到期后返回包装 `context.DeadlineExceeded` 的 timeout 分类，不再启动新 attempt。

Decorator 的 `ID`、`Type`、`EstimateInputTokens`、`Models` 和 `Close` 都恰好委托 inner 一次，不执行重试、缓存或字段改写；只有 `Chat` 和首个可见 chunk 前的 `StreamChat` 使用上述重试状态机。

### 6.2 Chat

`retryingProvider.Chat` 在收到完整 `ChatResponse` 前可以重试；任何响应都只向调用方返回一次。Adapter 已发送请求但返回可重试错误时，Manager 可重新发起完整请求，因为 Chat 没有可见增量。

### 6.3 StreamChat

Decorator 必须在内部消费每次 attempt 的 channel，并先缓冲到第一个可见、无错误 chunk：

1. 同步建立错误，或 channel 在第一个可见 chunk 前发送可重试错误：丢弃该 attempt，按退避重新建立。
2. 第一个可见 chunk 可以只有 role、reasoning、content、tool delta、finish 或 usage；它一旦转发给调用方，重试窗口立即关闭。
3. 首个可见 chunk 后的任何错误都直接转发为终态 `ChatChunk{Error: err}`，不得重放，避免重复文本或 Tool call。
4. 所有 attempt 最终都关闭内部 channel；外部 channel 只关闭一次。

Provider adapter 不应在首 chunk 后自行重连。`ctx.Done()` 不重试；外部 channel 关闭，调用方从自己的 context 得到取消原因。

---

## 7. 流式协议

### 7.1 Adapter channel

```text
StreamChat(ctx, req)
  -> synchronous validation/connection error, or channel
  -> zero or more ChatChunk
  -> optional one ChatChunk{Error: err}
  -> close(channel)
```

正常结束时最后一个 chunk 可携带 `FinishReason` 和 `Usage`，也可在没有增量时单独发送。非 context 错误必须先发送 error chunk 再关闭；context 取消直接关闭，调用方读取 `context.Cause(ctx)`。

### 7.2 Tool call 增量

Adapter 将厂商的 Tool call 分片映射到 `Delta.ToolCalls`，保留 Provider-safe alias 原值；Agent 按稳定调用 ID 拼接 `Function.Name` 与 `Arguments`，在 `FinishReason=tool_calls` 后按冻结 turn map 一次性反查 canonical name，校验全部 calls 后才交给 Tool Manager。Provider 不执行 Tool，也不负责 alias 生成或反查。unknown/非法 alias 由 Agent 返回 `ErrAgentProviderProtocol`，不产生 Tool 执行或 partial Session unit。

---

## 8. 错误处理

```go
type ErrorCode string

const (
    ErrCodeUnauthorized   ErrorCode = "unauthorized"
    ErrCodeForbidden      ErrorCode = "forbidden"
    ErrCodeRateLimit      ErrorCode = "rate_limit"
    ErrCodeServer         ErrorCode = "server"
    ErrCodeTimeout        ErrorCode = "timeout"
    ErrCodeInvalidRequest ErrorCode = "invalid_request"
    ErrCodeModelNotFound  ErrorCode = "model_not_found"
    ErrCodeContextLength  ErrorCode = "context_length"
    ErrCodeConnection     ErrorCode = "connection"
    ErrCodeUnknown        ErrorCode = "unknown"
)

type ProviderError struct {
    Code       ErrorCode
    Message    string
    StatusCode int
    Retryable  bool
    RetryAfter time.Duration
    ProviderID string
    Cause      error
}

func (e *ProviderError) Error() string
func (e *ProviderError) Unwrap() error { return e.Cause }
```

Adapter 分类规则：

| 来源 | Code | Retryable |
|------|------|:---------:|
| HTTP 401 | unauthorized | 否 |
| HTTP 403 | forbidden | 否 |
| HTTP 429 | rate_limit | 是，保留 Retry-After |
| HTTP 500/502/503/504 | server | 是 |
| HTTP 400 | invalid_request | 否 |
| model not found | model_not_found | 否 |
| context canceled/deadline | timeout，Cause 保留 `context.Cause(ctx)` | 否 |
| connection refused/temporary network | connection | 是 |

Manager 读取 `Retryable` 和正数 `RetryAfter`；它不解析错误字符串或 HTTP body。Remote/API 层先用 `errors.Is(context.Cause(requestCtx), ...)` 检查 request context：`context.Canceled` 时通常不再写响应，`context.DeadlineExceeded` 映射 HTTP 504 / `50401`。只有 request context 仍有效的真实 Provider 上游失败才映射 HTTP 502 / `50202`；内部错误 code 和 retry 分类只写日志。Agent Stop、Runtime shutdown 等自定义 cause 必须继续交给上层稳定错误映射，不能在 Provider 层压成 `context.Canceled`。

---

## 9. 与其他模块的关系

```text
Agent -> Provider Manager.Get -> retryingProvider -> adapter -> upstream API
Context -> EstimateInputTokens
Agent -> Chat / StreamChat
Remote -> Manager.List / Models (只读)
```

Provider 不依赖 Agent、Session、Context、Tool 或 Memory；它只接收完整、已投影的 `ChatRequest`。Agent 负责 alias 投影/反查、Tool loop、Session 提交和 Remote frame。Provider 不持久化 alias map。

---

## 10. Thinking / Reasoning

`ThinkingConfig` 是请求级字段。Adapter 将 `Enabled`、`Effort`、`Budget` 映射到厂商格式，并把响应/流式 reasoning 字段统一为 `ReasoningContent`。不支持 Thinking 的 Model 必须在 `ModelInfo.SupportsThinking=false`，Agent 在构造请求前拒绝不兼容的设置。

Reasoning 增量进入 Remote `reasoning_delta`；最终文本进入 `assistant_delta`。Provider 文档和实现不得引入 `content_delta` 别名。

---

## 11. 设计决策

- **PV-001：** 一个 `Provider` 接口，`Chat` 与 `StreamChat` 独立实现。
- **PV-002：** OpenAI-compatible 服务统一使用 `type: openai`。
- **PV-003：** Provider Manager 是唯一重试 owner；Adapter 不重试。
- **PV-004：** 流式首个可见 chunk 后禁止重试或重放。
- **PV-005：** 一个 Provider 一个 `base_url`，不做内部 failover。
- **PV-006：** `reasoning_content` 是统一 DTO 一等字段，Remote 名称固定 `reasoning_delta` / `assistant_delta`。
- **PV-007：** Provider adapter 不自行生成或修改 Tool alias；Agent/Tool 边界维护唯一 turn-local 映射，Context estimator 看到最终 wire 请求。
