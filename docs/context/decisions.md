# Context 设计决策

> 文档路径: `docs/context/decisions.md`
> 上级: [README.md](README.md)

---

## CX-001: Context 独立于 Session

**决策：** Session 保存完整历史；Context Manager 只生成一次 Provider 请求，不回写或裁剪 Session。

**原因：** 持久历史和临时窗口有不同生命周期。把截断放入 Session 会造成不可逆的数据丢失。

## CX-002: Provider 估算完整请求

**决策：** `provider.Provider.EstimateInputTokens(ctx, req)` 是唯一 Token 估算入口，输入是完整 `ChatRequest`。

**原因：** 模型 tokenizer、消息 framing、Tool schema 和 Provider 扩展都因厂商而异。Context 自建 tokenizer 或字符数近似无法保证不低估。

## CX-003: 只保留三种内建策略

**决策：** v1 只支持 `hybrid`、`truncate`、`reject`，不提供策略注册接口。

**原因：** 三种行为已经覆盖“摘要后兜底”“只删除旧历史”“严格拒绝”。在没有第二个真实实现前，自定义接口只增加配置、生命周期和测试面。

## CX-004: 以完整 unit 作为变换边界

**决策：** system、当前 turn 和当前 Tool chain 受保护；Tool call 与全部 results 是原子单元。旧普通 turn 可摘要或删除，旧 Tool turn只能整体删除。

**原因：** 按消息或优先级混合裁剪会留下 orphan Tool result，或丢失 Provider 要求回传的 `ReasoningContent`。

## CX-005: 输出额度显式预留

**决策：** 模型窗口来自 `ModelInfo.ContextWindow`；正的 `context.max_tokens` 只能进一步收紧它，随后减去 `context.reserved_tokens` 得到输入预算。Agent 的 `max_tokens` 必须不大于 reserve 和 `ModelInfo.MaxOutput`。

**原因：** 输入和输出共享模型窗口。仅检查输入上限会产生 Provider 可预测拒绝的请求。

## CX-006: 摘要同步执行并有 deadline

**决策：** `hybrid` 在一次 Build 内同步调用摘要模型，继承请求取消并叠加 `compression.timeout`；失败时按同一次 Build 的截断路径降级。

**原因：** 异步队列需要任务持久化、去重、过期和结果发布协议。v1 没有这些契约，同步调用更容易证明结果对应当前请求。

## CX-007: v1 不缓存 Context

**决策：** 每次 Build 从不可变配置快照和当前完整请求重新计算，不保存跨请求缓存。

**原因：** 正确 cache key 必须包含消息、Provider/Model、Tool schema、Memory、Skill 和配置 generation。仅使用 `lastMessageID` 会在 Tool 结果或热更新后返回旧请求。先保证正确，再用测量结果决定是否缓存。

## CX-008: Tool 结果限额归 Tool Manager

**决策：** 单条 Tool 结果只由 `tools.max_result_tokens` 限制。Context 只做最终窗口裁剪，不定义第二个同义字段。

**原因：** 两个 owner 会产生不同默认值和不可解释的覆盖顺序。Tool Manager 是结果进入 Session/Context 前的统一边界。

## CX-009: 直接保留 Provider Message

**决策：** Context 不复制定义简化版 Message；变换过程保存完整 `provider.Message`。

**原因：** `ReasoningContent`、`Name`、`Refusal` 和后续 Provider 字段必须端到端保留。内部 unit 只包装消息 slice 和变换元数据。

## CX-010: Tool alias 投影先于 Context Build

**决策：** Agent 先按 turn-local 映射深拷贝并投影完整 Provider 请求，再交给 Context；Context 不拥有 alias 算法、映射或反查。

**原因：** Provider estimator 必须看见实际 wire definition、历史 name 和 `specific` ToolChoice。若 Build 后才改名，Token 结果不再描述已验证的请求；若 Context 持有映射，则会复制 Tool/Agent 的权限和生命周期 owner。

## 模块依赖

```text
internal/config <- context
provider        <- context
session/memory/tool/skill -> agent alias projection -> context.Build -> provider
```

Agent 负责选择 Provider/Model、读取同一 Effective Config 快照、组装 canonical `ChatRequest` 并应用 [Provider-safe Tool alias 投影](../tool/provider.md)。Context 不直接依赖 Session Manager、Memory Manager、Skill Manager 或 Tool Manager，避免在 Builder 内形成隐藏 I/O 和循环依赖。

---

*最后更新: 2026-07-23*
