# Context 实现检查清单

> 文档路径: `docs/context/checklist.md`
> 上级: [README.md](README.md)

---

## 配置

- [x] `ContextConfig` 默认值与 [config-ref.md](config-ref.md) 完全一致
- [x] `AgentConfig.Context` 使用 `*ContextOverride`
- [x] pointer merge 保留显式 `false` 和 `0`
- [x] 加载阶段校验枚举、数值范围和 compression 跨字段关系
- [x] Agent 创建及 reload 发布前校验模型窗口、输出上限和 reserve
- [x] Provider 窗口未知时拒绝 Agent，不使用无限窗口兜底
- [x] 单条 Tool result 只读取 `tools.max_result_tokens`

## 数据模型与接口

- [x] `Build(ctx, BuildInput)` 是唯一公开构建入口
- [x] Context 直接保存完整 `provider.Message`
- [x] Provider 实现完整请求的 `EstimateInputTokens`
- [x] 估算包含 Tool schema、response format、framing 和 Provider extra
- [x] `Build` 不修改输入 Request、Session 历史或已发布 Config 快照
- [x] `Request.MaxTokens` 来自 Agent 输出上限且不大于 reserve/model max output
- [x] `BuildInput.Request` 已完成 Tool definitions、历史和 `specific` ToolChoice alias 投影
- [x] Context 不持有 alias map、不自行改名，Provider estimator 看见最终 wire 请求

## Unit 与策略

- [x] system、当前 user 和当前 Tool chain 受保护
- [x] Tool call 与全部 results 组成不可拆分 unit
- [x] orphan/重复/缺失 Tool result 返回 `ErrInvalidMessageSequence`
- [x] 旧普通 turn 可摘要或整体删除
- [x] 旧 Tool turn 只可整体删除，不能摘要后丢失 `ReasoningContent`
- [x] `reject` 超限直接返回 `ErrContextOverflow`
- [x] `truncate` 每次删除最旧可删除 unit 后重新估算
- [x] `hybrid` 按 threshold、target ratio、min messages、preserve recent 工作
- [x] 摘要继承请求取消并应用 `compression.timeout`
- [x] 摘要为空、不减 Token、失败或超时时恢复原请求并按需截断
- [x] 成功 Build 的最终完整请求不超过输入预算

## 错误与并发

- [x] 所有 sentinel 与 [errors.md](errors.md) 一致并保留 wrapped cause
- [x] Token 估算失败不使用字符数近似兜底
- [x] 受保护请求本身超限返回明确错误
- [x] `ctx.Done()` 在估算、摘要和循环截断中及时生效
- [x] 同一次 Build 只使用一个 Effective Config 快照
- [x] Manager 无跨 Session 可变状态并通过 race detector
- [x] v1 不实现异步摘要队列、自定义策略注册或 Context cache

## 可观测性与测试

- [x] 日志不含 prompt、Memory、Tool result、摘要正文或 Secret
- [x] 指标名称和标签与 [observability.md](observability.md) 一致
- [x] 指标不使用 session/request ID 高基数标签
- [x] 预算边界、受保护超限、完整 Tool unit、摘要 fallback 有单元测试
- [x] Provider 估算失败、取消、timeout 和 config reload 有集成测试
- [x] 使用多语言/UTF-8、Tool schema 和 ReasoningContent 请求做回归测试
- [x] safe/hashed alias 与 `specific` ToolChoice 的估算值覆盖真实 Provider wire

---

*最后更新: 2026-07-23*
