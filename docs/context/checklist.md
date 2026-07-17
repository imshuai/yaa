# Context 实现检查清单

> 文档路径: `docs/context/checklist.md`
> 上级: `docs/context/README.md` §11

---

## 11. 实现检查清单

### Context 定义与数据结构

- [ ] `Context` 结构体定义（Messages, TokenCount, Budget, SessionID, BuildAt）
- [ ] `Message` 结构体定义（Role, Content, ToolCallID, ToolCalls, Metadata）
- [ ] `MessagePriority` 枚举（System, High, Medium, Low）
- [ ] `ContextOption` 函数选项定义（WithMaxTokens, WithStrategy, WithProvider）
- [ ] `ContextBudget` 结构体定义（MaxInput, Reserved, Usable）
- [ ] Message 优先级标记逻辑（System=不可截断, User=高, Tool=中, Assistant=低）

### Context Manager

- [ ] `Manager` 结构体定义（strategies, providers, memory, skillMgr, cache, logger, mu）
- [ ] `Build()` — 构建完整 Context（聚合多源 + 策略执行）
- [ ] `Compress()` — 触发异步摘要压缩
- [ ] `Truncate()` — 执行截断至指定 Token 数
- [ ] `EstimateTokens()` — 委托 Provider 估算 Token 数
- [ ] `GetCache()` / `SetCache()` — Context 缓存读写
- [ ] `InvalidateCache()` — 消息变更时使缓存失效
- [ ] `GetStats()` — 获取 Context 构建统计

### 策略实现

- [ ] `Strategy` 接口定义（Name, Apply, Priority）
- [ ] `SlidingWindow` 策略 — 滑动窗口截断
  - [ ] 从尾部保留最近 N 条消息
  - [ ] System Prompt 始终保留
  - [ ] 跨消息对的完整性保证（Tool Call + Result 不拆分）
- [ ] `SummaryCompress` 策略 — 摘要压缩
  - [ ] 异步调用 LLM 生成早期消息摘要
  - [ ] 摘要消息以 System 角色注入
  - [ ] 压缩进行中降级为截断
  - [ ] 压缩结果缓存
- [ ] 策略链式执行（按优先级顺序，前一个结果作为后一个输入）
- [ ] 自定义策略注册接口

### 集成与交互

- [ ] 与 Session Manager 集成（Build 时接收 Session 消息历史）
- [ ] 与 Provider 集成（Token 估算、预算查询、异步压缩调用）
- [ ] 与 Memory 集成（长期摘要注入 Context 头部）
- [ ] 与 Skill Manager 集成（已激活 Skill 的 Prompt 注入）
- [ ] 与 Agent Loop 集成（Tool Call 后重建 Context）
- [ ] 与 Planner 集成（Planner 可请求精简 Context 做规划）

### 配置

- [ ] 全局 Context 配置（`context.*` in config.yaml）
- [ ] `context.default_strategy` — 默认策略（sliding_window / summary_compress / auto）
- [ ] `context.max_messages` — 最大保留消息条数
- [ ] `context.compression_threshold` — 触发压缩的 Token 占比阈值
- [ ] `context.compression_model` — 压缩使用的 Provider/Model
- [ ] Provider 级别 `context_budget` 配置（预留输出 Token）
- [ ] Agent 级别策略覆盖
- [ ] 配置校验与默认值合并

### 错误处理

- [ ] `ErrContextBuildFailed` — Context 构建失败
- [ ] `ErrContextOverflow` — 超出 Token 预算且无法截断（System Prompt 本身超限）
- [ ] `ErrCompressionFailed` — 摘要压缩调用失败
- [ ] `ErrCompressionTimeout` — 压缩超时
- [ ] `ErrStrategyNotFound` — 指定策略未注册
- [ ] `ErrProviderNoTokenizer` — Provider 不支持 Token 估算
- [ ] 压缩失败降级处理（记录错误 + 回退截断策略）
- [ ] Token 估算不可用时降级处理（按字符数近似估算）

### 可观测性

- [ ] Context 构建日志（Session ID、消息数、Token 数、命中缓存）
- [ ] 截断日志（截断前 Token 数、截断后 Token 数、移除消息数）
- [ ] 压缩日志（压缩触发、压缩完成、压缩耗时、压缩前后 Token 数）
- [ ] 指标: `context_build_total` (Counter)
- [ ] 指标: `context_build_duration` (Histogram)
- [ ] 指标: `context_token_count` (Gauge — 每次构建后的 Token 数)
- [ ] 指标: `context_cache_hit_total` / `context_cache_miss_total` (Counter)
- [ ] 指标: `context_truncate_total` (Counter)
- [ ] 指标: `context_compress_total` / `context_compress_failed_total` (Counter)
- [ ] 指标: `context_compress_duration` (Histogram)
- [ ] SSE 事件: `context.built`
- [ ] SSE 事件: `context.truncated`
- [ ] SSE 事件: `context.compressed`
- [ ] SSE 事件: `context.cache_hit` / `context.cache_miss`
- [ ] SSE 事件: `context.error`
- [ ] 调用链追踪（span: context.build, context.truncate, context.compress）
- [ ] Token 使用趋势监控（按 Agent / Session 维度聚合）

---

*最后更新: 2025-07-17*
