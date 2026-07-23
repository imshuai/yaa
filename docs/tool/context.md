# Tool 与 Context 的交互

> 文档路径: docs/tool/context.md
> 上级: README.md 8

---

## 8. Tool 与 Context 的交互

本文件中的 Tool name 都表示 Agent/Session 侧 canonical name。实际请求在进入 Context
之前已经按 [Provider-safe Tool alias 契约](provider.md) 深拷贝投影；Context 只看到
wire alias，但不能持有映射或把投影结果写回 Session。

### 8.1 Tool 结果在 Context 中的表示

Tool 执行结果以 `role="tool"` 的 Message 加入 Context：

```go
// Tool 结果 → Context Message
func toolResultToMessage(callID, canonicalName string, result ToolResult) provider.Message {
    return provider.Message{
        Role:       "tool",
        Name:       canonicalName,
        Content:    result.Content,
        ToolCallID: callID,
    }
}
```

**Context 中的完整 Tool 交互序列：**

```text
messages: [
  {role: "user",      content: "北京天气怎么样？"},
  {role: "assistant", content: "", tool_calls: [
      {id: "call_1", function: {name: "http", arguments: "{\"url\":\"...\"}"}}
  ]},
  {role: "tool", name: "http", content: "{\"temp\": 25, \"weather\": \"晴\"}", tool_call_id: "call_1"},
  {role: "assistant", content: "北京现在 25°C，晴天。"}
]
```

### 8.2 多轮 Tool 调用

LLM 可能在一轮中调用 Tool，根据结果决定是否再次调用：

```text
Turn 1: User → "帮我查北京和上海的天气"
  │
  ├─ LLM → tool_calls: [get_weather("北京"), get_weather("上海")]
  │  └─ 并发执行 → 两个 tool 消息加入 Context
  │
  ├─ LLM → content: "北京 25°C 晴，上海 28°C 多云"
  │  └─ FinishReason = "stop" → 对话结束
```

### 8.3 Context 截断中的 Tool 消息

Context Manager 在截断历史消息时，需保持 Tool 消息的完整性：

**规则：**
- `assistant` 消息中的 `tool_calls` 和紧随其后的 `tool` 消息是一个**原子单元**
- 不能只截断 `tool` 消息而保留 `tool_calls`，反之亦然
- 截断以原子单元为单位

```go
// 原子单元：assistant(tool_calls) + 后续所有 tool 消息
type ContextUnit struct {
    AssistantMessage provider.Message  // 含 tool_calls
    ToolResults     []provider.Message  // role="tool" 的结果消息
}
```

### 8.4 深度思考模式下的 Context 处理

当启用深度思考模式时，Tool 交互的 Context 需要额外处理 `reasoning_content`：

```text
messages: [
  {role: "user",      content: "计算 9.11 和 9.8 哪个大"},
  {role: "assistant", reasoning_content: "让我分析...", content: "",
   tool_calls: [{id: "call_1", function: {name: "calculate", arguments: "{...}"}}]},
  {role: "tool",      content: "9.11", tool_call_id: "call_1"},
  {role: "assistant", reasoning_content: "根据计算结果...", content: "9.11 比 9.8 大"}
]
```

**关键规则（DeepSeek）：**
- 有 Tool Call 的 assistant 消息的 `reasoning_content` **必须保留**
- Context Manager 截断时不能丢弃这些 `reasoning_content`
- 无 Tool Call 的轮次可以丢弃 `reasoning_content`

---
