# Tool 与 Provider 的衔接

> 文档路径: `docs/tool/provider.md`
> 上级: `docs/tool/README.md` §5

---

## 5. Tool 与 Provider 的衔接

### 5.1 Tool → ToolDef 转换

Tool Manager 将 Tool 转换为 Provider 可识别的 `ToolDef`：

```go
func (m *Manager) ToToolDefs(toolNames []string) ([]provider.ToolDef, error) {
    var defs []provider.ToolDef
    for _, name := range toolNames {
        tool, err := m.Get(name)
        if err != nil {
            return nil, fmt.Errorf("tool %s: %w", name, err)
        }
        defs = append(defs, provider.ToolDef{
            Type: "function",
            Function: provider.ToolFunction{
                Name:        tool.Name(),
                Description: tool.Description(),
                Parameters:  tool.Parameters(),
            },
        })
    }
    return defs, nil
}
```

### 5.2 ToolDef → LLM 请求

Agent 在调用 Provider 前将 `ToolDef` 注入 `ChatRequest.Tools`：

```text
Agent Loop:
  │
  ├─ 1. 获取 Agent 可用的 Tool 列表
  │     └─ ToolManager.ListForAgent(agentID) → ["shell", "http", "file_read"]
  │
  ├─ 2. 转换为 ToolDef
  │     └─ ToolManager.ToToolDefs(["shell", "http", "file_read"])
  │
  ├─ 3. 注入 ChatRequest
  │     └─ request.Tools = toolDefs
  │     └─ request.ToolChoice = "auto" (默认)
  │
  ├─ 4. 调用 Provider
  │     └─ Provider.Chat(ctx, request)
  │
  ├─ 5. 检查响应
  │     ├─ FinishReason = "stop" → 对话结束
  │     └─ FinishReason = "tool_calls" → 进入 Tool 执行
  │
  ├─ 6. 执行 Tool
  │     └─ ToolManager.ExecuteBatch(ctx, agentID, response.ToolCalls)
  │
  ├─ 7. 将结果注入 Context
  │     └─ 每个 ToolCall → 生成 Role="tool" 的 Message
  │     └─ Message.Content = ToolResult.Content
  │     └─ Message.ToolCallID = ToolCall.ID
  │
  └─ 8. 回到步骤 4（下一轮 LLM 调用）
```

### 5.3 ToolChoice 控制

```go
// ToolChoice 控制 LLM 的 Tool 调用行为。
type ToolChoice struct {
    Mode string // "auto" | "none" | "required" | "specific"
    Tool string // 当 Mode="specific" 时指定 Tool 名称
}
```

| Mode | 含义 | 使用场景 |
|------|------|---------|
| `auto` | LLM 自行决定是否调用 Tool | 默认值 |
| `none` | 禁止 LLM 调用 Tool | 纯对话模式 |
| `required` | LLM 必须调用至少一个 Tool | 强制使用 Tool |
| `specific` | LLM 必须调用指定的 Tool | 路由/强制特定操作 |

**Agent 配置示例：**

```yaml
agents:
  - id: "forced-tool-agent"
    tools: ["http", "file_read"]
    tool_choice: "required"     # 强制每轮必须调用 Tool
```

### 5.4 与深度思考模式的配合

当 Provider 启用了深度思考模式（见 `provider.md` §13），Tool Call 的处理需要额外注意：

1. **思维链中的 Tool 规划** — LLM 在 `reasoning_content` 中可能规划了 Tool 调用策略，但实际 Tool Call 在 `content` 中返回
2. **reasoning_content 必须回传** — 有 Tool Call 的轮次，`reasoning_content` 必须在后续请求中回传（DeepSeek 要求）
3. **Agent 层处理** — Agent 收到 `FinishReason="tool_calls"` 时，需同时保存 `reasoning_content` 和 `tool_calls`

```go
// Agent 处理 Tool Call 轮次
func (a *Agent) handleToolCallResponse(resp provider.ChatResponse) {
    // 保存助手消息（含 reasoning_content 和 tool_calls）
    assistantMsg := provider.Message{
        Role:             "assistant",
        Content:          resp.Content,
        ReasoningContent: resp.ReasoningContent, // 深度思考内容
        ToolCalls:        resp.ToolCalls,
    }
    a.context.AppendMessage(assistantMsg)

    // 执行 Tool
    results, err := a.toolManager.ExecuteBatch(ctx, a.id, resp.ToolCalls)

    // 将结果作为 tool 消息加入上下文
    for i, call := range resp.ToolCalls {
        a.context.AppendMessage(provider.Message{
            Role:       "tool",
            Content:    results[i].Content,
            ToolCallID: call.ID,
        })
    }
}
```
