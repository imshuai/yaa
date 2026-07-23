# Context Manager

> 文档路径: `docs/context/manager.md`
> 上级: [README.md](README.md)

---

## 1. 公共契约

Context Manager 只有一个公开操作：把完整候选请求变成可安全发送的请求。压缩和截断是内部步骤，不暴露第二套可绕过校验的入口。

```go
type Manager struct{}

type BuildInput struct {
    Provider         provider.Provider
    Model            provider.ModelInfo
    Request          provider.ChatRequest
    Config           config.ContextConfig
    CurrentTurnStart int
}

type BuildOutput struct {
    Request         provider.ChatRequest
    InputTokens     int
    InputBudget     int
    EffectiveWindow int
    Metadata        BuildMetadata
}

type BuildMetadata struct {
    Strategy          string
    OriginalMessages  int
    FinalMessages     int
    CompressedTurns   int
    TruncatedUnits    int
    CompressionFailed bool
    BuildDuration     time.Duration
}

func (m *Manager) Build(ctx context.Context, in BuildInput) (*BuildOutput, error)
```

调用前置条件：

- `Provider` 非 nil，`Request.Model == Model.ID`。
- `Request.MaxTokens` 非 nil且大于 0；它来自 Agent 的 `max_tokens`。
- `CurrentTurnStart` 指向 `Request.Messages` 中最新的 user 消息。
- `Config` 已由根配置和 Agent override 合并。
- `Model.ContextWindow`、`Model.MaxOutput` 已解析为正数。
- `Request` 已由 Agent 的冻结 `ProviderToolProjection` 深拷贝并完成 Tool definitions、历史 Tool name 和 `specific` ToolChoice 的 alias 投影；Context 不接受 canonical 请求后自行补投影。

`Build` 复制 `Request` 后工作，不修改调用方传入的 slice、map 或 Session 历史。

## 2. Provider Token 契约

Provider 必须按自己实际的请求编码规则实现：

```go
type Provider interface {
    // 其他方法见 docs/provider.md。
    EstimateInputTokens(ctx context.Context, req *ChatRequest) (int, error)
}
```

返回值只计算输入，不包含将要生成的 completion；但必须包含消息 framing、`ReasoningContent`、已投影 alias 的 Tool definitions/历史/`ToolChoice`、`ResponseFormat`、`Thinking` 和会进入线上请求的 `Extra`。实现可以返回精确值或保守上界，不得低估。无法估算时返回错误，Context 不做字符数兜底。

## 3. Build 流程

```text
validate input and effective config
  -> resolve effective window and input budget
  -> validate and group message units
  -> estimate the complete candidate request
  -> estimate protected-only request
  -> protected-only request over budget? ErrContextOverflow
  -> apply strategy
  -> estimate final complete request
  -> final tokens over budget? ErrContextOverflow
  -> return request and metadata
```

模型预算由 [config-ref.md](config-ref.md) 的 `ResolveContextBudget` 计算。受保护请求仍保留所有非消息字段和 Tool definitions，只移除可删除历史；因此它能检测 Tool schema 本身过大等情况。

每次 Provider 估算都使用调用方的 `ctx`。摘要调用在其上再派生 `compression.timeout` deadline；取消必须立即向上传递。

## 4. Unit 构造与校验

内部 unit 至少包含以下字段：

```go
type messageUnit struct {
    Messages   []provider.Message
    Protected  bool
    Compressible bool
}
```

构造规则按顺序执行：

1. 开头的 `system` 消息各自成为 `Protected=true` 的 unit。
2. 每个 `user` 开始一个 turn，直到下一个 `user` 之前结束。
3. assistant 消息含 `ToolCalls` 时，紧随其后的 tool 消息必须恰好覆盖所有 call ID；该组不可拆分。
4. `CurrentTurnStart` 所在 turn 以及其后的消息全部受保护。
5. 历史普通 turn 可摘要、可删除；历史 Tool turn 只能整体删除；system/current turn 不可变换。

以下输入返回 `ErrInvalidMessageSequence`：未知 role、首个非 system 消息不是 user、orphan tool result、重复或未知 `ToolCallID`、已完成历史中的缺失 Tool result、`CurrentTurnStart` 越界或未指向最新 user。

`provider.Message` 原值整体复制，不能投影为只含 `Role`/`Content` 的本地类型。Tool
name 在输入中已经是 wire alias；Context 只能随完整 message/unit 保留或删除，不得 hash、
canonicalize 或持有 alias map。摘要生成的新 system message 不含 Tool name。

## 5. 策略算法

### 5.1 reject

候选请求在预算内则原样返回；否则返回 `ErrContextOverflow`。不尝试摘要或截断。

### 5.2 truncate

只在超限时执行：

1. 找到最旧的 `Protected=false` unit。
2. 整体删除该 unit。
3. 重新组装完整请求并调用 `EstimateInputTokens`。
4. 重复直到不超限；没有可删除 unit 时返回 `ErrContextOverflow`。

不能按单条消息 Token 和倒序切片，因为 Tool schema 与消息 framing 会使逐项加总不等于最终请求大小。

### 5.3 hybrid

当 `compression.enabled=true` 且当前利用率达到 `compression.threshold` 时，先尝试一次同步摘要：

1. 从旧到新选择普通、完整、未受保护的 turn。
2. 排除最近 `compression.preserve_recent` 个可压缩 turn。
3. 候选消息数少于 `compression.min_messages` 时跳过摘要。
4. 在 `compression.timeout` 内，用同一 Provider/Model 总结候选历史；摘要请求继承调用方取消信号，`MaxTokens` 使用原请求的输出上限。
5. 用一条内部标记为可删除的 system summary 替换候选 turn，再估算完整目标请求。
6. 只有新请求 Token 更少且摘要非空时接受；否则恢复原请求。

`compression.target_ratio` 是摘要后的目标利用率。一次摘要仍高于目标时不递归调用摘要；若实际超出输入预算，再按 `truncate` 算法删除最旧可删除 unit。

摘要失败或超时：

- 原请求仍在预算内：返回原请求，并设置 `Metadata.CompressionFailed=true`。
- 原请求已超限：记录失败后执行 `truncate`；截断成功则 Build 成功。
- 截断也无法满足预算：返回 `ErrContextOverflow`，并在错误链中保留摘要原因。

v1 不异步执行摘要、不缓存结果，也不在 Context 层增加 retry loop。摘要仍调用 Provider Manager 返回的统一 decorator，因此可在首个可见结果前按 Provider policy 重试，但所有尝试共同受 `compression.timeout` 和调用方 context 限制。

## 6. 并发与热更新

`Manager` 无可变 Session 状态，可被并发调用。每次 Build 只使用 `BuildInput.Config` 这一份不可变 Effective Config 快照；构建期间配置热更新不改变当前请求，下一次 Build 使用新快照。

Context 不保存跨请求 cache，因此无需失效锁或 config generation key。Provider 自己的线程安全要求由 Provider 契约保证。

## 7. 最小测试矩阵

| 场景 | 预期 |
|------|------|
| 完整请求刚好等于预算 | 成功且不变换 |
| Tool definitions 使受保护请求超限 | `ErrContextOverflow` |
| 历史 Tool turn 被截断 | call 与全部 results 一起删除 |
| 当前 Tool turn 超限 | 不拆分，返回 `ErrContextOverflow` |
| summary 变大或为空 | 丢弃 summary，按需要截断 |
| Provider 估算失败 | `ErrTokenEstimationFailed`，不发送请求 |
| `ctx` 取消或摘要超时 | 立即停止；按错误契约处理 |
| 热更新发生在 Build 中 | 当前 Build 使用旧快照，下一次使用新快照 |
| safe/hashed Tool alias 与 `specific` ToolChoice | 估算输入与最终 Provider wire 字段完全一致 |

---

*最后更新: 2026-07-23*
