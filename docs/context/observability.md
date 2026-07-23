# Context 可观测性

> 文档路径: `docs/context/observability.md`
> 上级: [README.md](README.md)

---

## 1. 日志

每次 Build 最多写一条完成日志；压缩、截断或失败时再写对应事件。不得记录完整 prompt、Memory、Tool result、API key 或摘要正文。

| 事件 | 级别 | 必需字段 |
|------|------|----------|
| `context.built` | debug | request_id, session_id, agent_id, provider_id, model, strategy, input_tokens, input_budget, original_messages, final_messages, duration_ms |
| `context.compressed` | info | request_id, before_tokens, after_tokens, compressed_turns, duration_ms |
| `context.compression_failed` | warn | request_id, reason, fallback, duration_ms |
| `context.truncated` | info | request_id, before_tokens, after_tokens, dropped_units |
| `context.overflow` | warn | request_id, input_tokens, input_budget, protected_tokens, strategy |
| `context.error` | error | request_id, error_type, error |

`session_id`、`agent_id` 只进入结构化日志和 trace，不作为 Prometheus 标签。

## 2. 指标

所有 duration 使用秒，名称带 `_seconds`。标签只使用有界枚举或已配置的稳定 ID。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `context_build_total` | Counter | provider, model, strategy, result | Build 次数 |
| `context_build_duration_seconds` | Histogram | provider, model, strategy | Build 耗时 |
| `context_input_tokens` | Histogram | provider, model | 成功请求输入 Token |
| `context_input_budget` | Gauge | agent, provider, model | 当前 Effective Config 的输入预算 |
| `context_utilization_ratio` | Histogram | provider, model | `input_tokens/input_budget` |
| `context_compression_total` | Counter | provider, model, result | 摘要尝试次数 |
| `context_compression_duration_seconds` | Histogram | provider, model, result | 摘要耗时 |
| `context_truncation_total` | Counter | provider, model | 发生截断的 Build 数 |
| `context_dropped_units_total` | Counter | provider, model | 被整体删除的 unit 数 |
| `context_overflow_total` | Counter | provider, model, strategy, reason | 无法满足预算次数 |
| `context_token_estimation_failed_total` | Counter | provider, model | Provider 估算失败次数 |

不得声明 Context cache hit/miss 指标；v1 没有 Context cache。

## 3. Trace

```text
context.build
  context.estimate
  context.compress       # 仅 hybrid 触发时存在
    provider.chat
  context.truncate       # 仅需要时存在
  context.estimate
```

Span 属性使用与指标相同的稳定字段，可额外包含 request/session/agent ID。错误 span 记录错误类型，不附加原始消息。

## 4. Remote 边界

Context 只写上述日志、指标和 trace；v1 没有 Context Remote 事件或独立 SSE 路由。Context 不注册 `context_status`、`context_history` 或 `context_compress` 内置 Tool。需要管理能力时使用已有 Session Remote API；新增 Tool 必须先在 Tool registry、权限模型和 Remote API 中同时定义。

---

*最后更新: 2026-07-22*
