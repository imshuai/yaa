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
| **自动注册与发现** | 内置 Tool 启动即注册，自定义 Tool 通过配置/插件注册 |
| **权限控制** | Agent 级别控制可用 Tool 白名单/黑名单 |
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
    // 命名规范: 小写蛇形，如 "shell", "http_request", "file_read"。
    Name() string

    // Description 返回工具的人类可读描述。
    // 这段描述会传递给 LLM，影响 LLM 的调用决策。
    Description() string

    // Parameters 返回工具参数的 JSON Schema。
    // LLM 依据此 Schema 生成调用参数。
    Parameters() json.RawMessage

    // Execute 执行工具调用。
    // params 是 LLM 生成的参数（已通过 JSON Schema 校验）。
    // 返回 ToolResult 和 error。error 表示执行失败，ToolResult 中的内容会回传给 LLM。
    Execute(ctx context.Context, params map[string]any) (ToolResult, error)
}
```

### 2.2 ToolResult

```go
// ToolResult 是 Tool 执行的返回值。
type ToolResult struct {
    // Content 是返回给 LLM 的文本内容。
    // 这是 LLM 在后续轮次中看到的 Tool 结果。
    Content string

    // IsError 标记这是否是一个"软错误"。
    // true 时 LLM 可据此调整策略（如换一种方式重试）。
    // 与 Execute 返回 error 不同：error 是硬错误，会中断 Tool Loop。
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

Tool 结果可能很大（如读取大文件），Yaa! 在将结果注入 Context 前进行截断：

```go
const MaxToolResultTokens = 4000 // 默认上限

// 截断策略
func truncateResult(content string, maxTokens int) string {
    tokens := estimateTokens(content)
    if tokens <= maxTokens {
        return content
    }
    // 保留头部 + 尾部，中间用省略标记
    headLen := maxTokens * 6 / 10  // 60% 头部
    tailLen := maxTokens * 3 / 10  // 30% 尾部
    return content[:headLen] +
        "\n\n[... truncated for brevity ...]\n\n" +
        content[len(content)-tailLen:]
}
```

截断阈值可按 Tool / Agent / Session 级别配置。

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
| [provider.md](provider.md) | Tool 与 Provider 衔接（ToolDef 转换、ToolChoice、深度思考配合） |
| [builtin.md](builtin.md) | 内置 Tool 总览 + 通用执行类（Shell / HTTP / File） |
| [config-tools.md](config-tools.md) | Config 系列工具（query / set / reload / scheme / save / diff） |
| [introspection.md](introspection.md) | 内视与管理系列工具（runtime / agent / session / skill / provider / mcp / log / metric / 管理） |
| [custom.md](custom.md) | 自定义 Tool（实现、注册方式、插件接口） |
| [context.md](context.md) | Tool 与 Context 的交互（结果表示、多轮调用、截断保护、深度思考） |
| [errors.md](errors.md) | 错误处理（分类、传递、重试策略） |
| [observability.md](observability.md) | 可观测性（日志、指标、Remote API 事件） |
| [config-ref.md](config-ref.md) | 配置参考（完整配置、Agent 级别覆盖） |
| [decisions.md](decisions.md) | 设计决策（TD-001 ~ TD-013） |
| [checklist.md](checklist.md) | 实现检查清单 |
