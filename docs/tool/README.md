# Tool 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/tool/` (原 `docs/tool.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.7, `docs/provider.md` §3.3

---

## 1. 设计目标

Tool 是 Agent 可调用的原子能力。Tool 系统的设计目标：

| 目标 | 说明 |
|------|------|
| **统一接口** | 所有 Tool 遵循同一 Go interface，不关心实现细节 |
| **JSON Schema 描述** | 参数用 JSON Schema 声明，自动传递给 LLM 的 Function Calling |
| **启动期注册** | 内置 Tool、Plugin Proxy 和 MCP Proxy 按 Runtime 启动顺序注册 |
| **权限控制** | `agents[].tools` 作为 Agent 级精确 allowlist；空数组表示全部 |
| **超时与取消** | 所有 Tool 执行受 context 控制，支持超时和取消 |
| **并发执行** | 同一轮多个 Tool Call 可并行执行 |
| **可观测** | 每次调用有完整日志、耗时、Token 统计 |
| **安全隔离** | 危险 Tool（如 Shell）支持沙箱限制 |

---

## 2. 核心接口

### 2.1 Tool Interface

```go
// Tool 是所有工具的统一接口。
type Tool interface {
    // Name 返回工具的唯一标识符。
    // 返回 Runtime 内唯一的 canonical Tool name。
    Name() string

    // Description 返回工具的人类可读描述。
    // 这段描述会传递给 LLM，影响 LLM 的调用决策。
    Description() string

    // Parameters 返回工具参数的 JSON Schema。
    // LLM 依据此 Schema 生成调用参数。
    Parameters() json.RawMessage

    // Execute 执行工具调用。
    // scope 是 Runtime 解析出的调用身份；params 已通过 JSON Schema 校验。
    // IsError 表示已处理的业务失败；error 表示 Manager 需要分类的硬错误。
    Execute(ctx context.Context, scope ExecutionScope, params map[string]any) (ToolResult, error)
}
```

canonical Tool name 必须是合法 UTF-8、1..256 bytes 且无 Unicode 控制字符；builtin 通常使用小写蛇形，但 MCP 等来源可以使用点分命名空间。Provider 的 function-name 限制不反向收窄 canonical name，完整转换见 [Provider-safe Tool alias 契约](provider.md)。

`ExecutionScope.AgentID` 永远非空；Agent turn 还必须携带真实 `SessionID`。实现不得从 params、Tool 名或 HTTP identity 推导 principal。即使某个 builtin 不使用 scope，也必须显式接收它，保证 Plugin/MCP/内视 Tool 没有旁路接口。

### 2.2 ToolResult

```go
// ToolResult 是 Tool 执行的返回值。
type ToolResult struct {
    // Content 是返回给 LLM 的文本内容。
    // 这是 LLM 在后续轮次中看到的 Tool 结果。
    Content string

    // IsError 标记这是否是一个"软错误"。
    // true 时 LLM 可据此调整策略（如换一种方式重试）。
    // ExecuteBatch 会把非 caller-cancel 的硬错误投影为脱敏 ToolResult，保持 Tool unit 完整。
    IsError bool

    // Meta 是可选的元数据，不传递给 LLM。
    // 用于日志、监控、审计等场景。
    Meta map[string]any
}
```

**Content 格式约定：**

| 场景 | Content 格式 |
|------|-------------|
| 文本结果 | 直接返回纯文本 |
| JSON 结构化结果 | 返回 JSON 字符串 |
| 文件内容 | 返回文件内容文本（注意截断） |
| 二进制数据 | 返回 Base64 编码或文件路径 |
| 错误信息 | `IsError=true`，Content 包含错误描述 |

**Content 截断策略：**

Tool 结果可能很大（如读取大文件），Yaa! 在将结果注入 Session/Context 前应用 `tools.max_result_tokens`。这是单条结果的唯一配置 owner；Context 只负责最终完整请求的窗口预算。

```go
func (m *Manager) limitResult(
    ctx context.Context,
    agentID string,
    content string,
    maxTokens int,
) (string, error)
```

`limitResult` 使用该 Agent 目标 Provider/Model 的 Token 估算器；超过上限时保留 UTF-8 完整的头尾并计入截断标记，迭代缩短直到估算值不大于上限。估算失败时返回错误，不注入未受限结果。最终 `ChatRequest` 仍由 Context Manager 重新做完整请求估算。

### 2.3 ToolSchema

```go
// ToolSchema 是 Tool 参数的 JSON Schema 封装。
// 本质上是 json.RawMessage，内容为标准 JSON Schema (draft 2020-12)。
type ToolSchema = json.RawMessage
```

示例 Schema：

```json
{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The shell command to execute"
    },
    "timeout": {
      "type": "integer",
      "description": "Timeout in seconds",
      "default": 30
    }
  },
  "required": ["command"]
}
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [manager.md](manager.md) | Tool Manager — 注册、发现、权限、执行引擎 |
| [provider.md](provider.md) | canonical name 与 Provider-safe alias 的权威契约 |
| [builtin.md](builtin.md) | 内置 Tool 总览 + 通用执行类（Shell / HTTP / File） |
| [config-tools.md](config-tools.md) | Config 系列工具（query / reload） |
| [introspection.md](introspection.md) | 固定只读内视工具（runtime / agent / session / skill / provider / mcp） |
| [custom.md](custom.md) | 自定义 Tool（实现、注册方式、插件接口） |
| [context.md](context.md) | Tool 与 Context 的交互（结果表示、多轮调用、截断保护、深度思考） |
| [errors.md](errors.md) | 错误处理（分类、传递、重试策略） |
| [observability.md](observability.md) | 可观测性（日志、指标、Remote API 事件） |
| [config-ref.md](config-ref.md) | 配置参考（完整配置、Agent 级别覆盖） |
| [decisions.md](decisions.md) | 设计决策（TD-001 ~ TD-013） |
| [checklist.md](checklist.md) | 实现检查清单 |
