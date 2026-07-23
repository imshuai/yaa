# Context 系统设计

> 文档路径: `docs/context/`
> 依赖: [Provider](../provider.md)、[Session](../session/README.md)、[Tool](../tool/README.md)

---

## 1. 职责边界

Context 是一次 Provider 请求的输入窗口。它从 Agent 已组装的 `provider.ChatRequest` 中压缩或移除旧历史，保证最终请求不超过目标模型的输入预算。

| 模块 | 保存什么 | 是否持久化 | 对窗口的职责 |
|------|----------|------------|----------------|
| Session | 完整消息历史 | 按 Session 配置 | 不截断历史 |
| Memory | 跨 Session 记忆 | 是 | 检索并限制注入量 |
| Tool | Tool 定义与执行结果 | 结果写入 Session | 进入 Context 前限制单条结果 |
| Context | 本次 `ChatRequest` | 否 | 估算、摘要、按原子单元截断 |

Context 不修改 Session，不维护第二份消息模型，也不负责 Tool 输出的单条截断。

## 2. 单一数据流

```text
Agent 解析 Provider/Model 和 Effective Config
  -> 组装 canonical ChatRequest
  -> 冻结并应用 Provider Tool alias 投影
     (history/tool definitions/specific ToolChoice)
  -> Context Manager.Build
  -> Provider.EstimateInputTokens
  -> hybrid | truncate | reject
  -> 再次估算并确认 input_tokens <= input_budget
  -> Provider.Chat 或 Provider.StreamChat
```

Agent 必须先选择目标 Provider 和 Model，再构建 Context。`Session` 不拥有 `SystemPrompt`；System Prompt 来自 Agent 配置并作为 `provider.Message{Role: "system"}` 放入请求。

## 3. 不变量

1. 最终 Token 估算覆盖完整 `ChatRequest`，包括消息 framing、Tool schema、`ToolChoice`、`ResponseFormat`、`Thinking` 和 Provider 扩展字段。
2. 输出预算单独预留：`input_budget = effective_window - reserved_tokens`。
3. System 消息、当前 user 消息和当前 Tool chain 不可删除或摘要；它们单独超限时返回 `ErrContextOverflow`。
4. `assistant(tool_calls)` 与其对应的全部 `tool` 结果是一个原子单元，不得拆开。
5. Context 直接复制 `provider.Message`，因此 `ReasoningContent`、`Name`、`Refusal` 和厂商需要的 Tool 字段不会在转换中丢失。
6. 每次变换后重新调用 Provider 估算；不能通过字符数、字节数或旧的逐消息估算推断最终请求大小。
7. 任何成功返回的请求都满足输入预算。无法证明时返回错误，不能把可能超限的请求发送给 Provider。
8. Context 输入已经完成 [Provider-safe Tool alias 投影](../tool/provider.md)；Context 不持有映射、不自行改名，也不把请求副本回写 canonical Session。

## 4. 策略

| 策略 | 行为 |
|------|------|
| `hybrid` | 达到阈值时同步摘要可压缩的旧普通 turn；失败或仍超限时按完整 unit 截断 |
| `truncate` | 仅在超限时从最旧的可删除完整 unit 开始截断 |
| `reject` | 超限立即返回 `ErrContextOverflow` |

v1 不提供自定义策略注册、异步摘要队列或 Context cache。配置快照在每次 `Build` 开始时读取一次，因此热更新自然从下一次构建生效。

## 5. 消息单元

Context Manager 从 `provider.Message` 序列构造内部 unit：

- 开头的 `system` 消息各自为受保护 unit。
- 一个普通 turn 从 `user` 开始，到下一个 `user` 之前结束。
- 含 `tool_calls` 的 assistant 消息和紧随其后的对应 Tool results 保持在同一 unit。
- 当前 turn 由 `BuildInput.CurrentTurnStart` 指定，整个 unit 受保护。
- 旧 Tool unit 可以整体删除，但不参与摘要，避免破坏 `ReasoningContent` 和 Tool 调用语义。
- orphan Tool result、重复 call ID 或不完整的历史 Tool chain 返回 `ErrInvalidMessageSequence`。

Memory 注入通常使用 system 消息，因此也属于受保护输入。Memory 模块必须先按自己的检索上限控制注入量；如果受保护输入仍超过预算，Context 明确报错。

## 6. 文档索引

| 文件 | 权威内容 |
|------|----------|
| [manager.md](manager.md) | `Build` 输入输出、unit 规则和算法 |
| [config-ref.md](config-ref.md) | 配置类型、默认值、Agent override 和预算校验 |
| [errors.md](errors.md) | 错误类型与降级边界 |
| [observability.md](observability.md) | 日志、指标和事件 |
| [decisions.md](decisions.md) | 已锁定设计决策 |
| [checklist.md](checklist.md) | 实现和验收清单 |

---

*最后更新: 2026-07-23*
