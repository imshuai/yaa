# Context 错误处理

> 文档路径: `docs/context/errors.md`
> 上级: `docs/context/README.md` §7

---

## 7. 错误处理

### 7.1 错误分类

| 错误类型 | 说明 | 处理方式 |
|---------|------|---------|
| `ErrContextBuildFailed` | Context 构建失败（段缺失或序列化错误） | 返回给 Agent，终止本轮推理 |
| `ErrTokenBudgetExceeded` | Token 预算超限且无法通过策略降级 | 返回给 LLM，提示减少输入 |
| `ErrTokenEstimationFailed` | Token 估算失败（模型未加载或编码器异常） | 回退到字符数近似估算，记录警告 |
| `ErrCompressionFailed` | 压缩失败（LLM 总结失败或截断异常） | 回退到 `truncate` 策略，保留最近消息 |
| `ErrCompressionTimeout` | 压缩超时（LLM 总结耗时过长） | 取消压缩，改用 `overflow_strategy` |
| `ErrSegmentUnavailable` | 关键段不可用（如系统提示词加载失败） | 返回致命错误，终止会话 |
| `ErrOverflowStrategyUnknown` | 未知的超出策略 | 回退到默认策略 `compress` |
| `ErrContextEmpty` | 构建后 Context 为空（无有效消息） | 返回给 Agent，提示检查输入 |

### 7.2 错误传递

```text
Context 错误 → Context Builder → Agent → LLM
                                    │
                                    ├─ 可降级错误 → 自动回退策略，继续推理
                                    └─ 不可降级错误 → 终止本轮，告知用户
```

**Agent 层错误处理：**

```go
func (a *Agent) buildContext(session *Session) (*Context, error) {
    ctx, err := a.ctxBuilder.Build(session)
    if err != nil {
        switch {
        case errors.Is(err, ErrTokenBudgetExceeded):
            // 尝试强制压缩
            ctx, err = a.ctxBuilder.BuildWithForce(session, ForceCompress)
            if err != nil {
                return nil, fmt.Errorf("context budget exceeded, compression also failed: %w", err)
            }
        case errors.Is(err, ErrCompressionFailed):
            // 回退到截断策略
            a.logger.Warn("compression failed, fallback to truncate",
                "session", session.ID, "error", err)
            ctx, err = a.ctxBuilder.BuildWithStrategy(session, "truncate")
            if err != nil {
                return nil, fmt.Errorf("context build failed after fallback: %w", err)
            }
        case errors.Is(err, ErrSegmentUnavailable):
            // 关键段不可用，终止
            return nil, fmt.Errorf("critical context segment unavailable: %w", err)
        default:
            return nil, fmt.Errorf("context build failed: %w", err)
        }
    }
    return ctx, nil
}
```

### 7.3 降级策略

当 Context 构建或压缩出错时，按以下优先级降级：

```text
1. summarize 压缩    → 失败
2. truncate 截断     → 失败
3. 保留最近 N 条消息  → 最后兜底
```

| 原始策略 | 降级后策略 | 触发条件 |
|---------|-----------|---------|
| `summarize` | `truncate` | LLM 总结失败或超时 |
| `truncate` | 保留 `preserve_recent` 条 | 截断后仍超限 |
| `compress` | `reject` | 所有降级均失败 |
| `reject` | — | 直接返回错误给 LLM |

### 7.4 重试策略

| 操作 | 重试条件 | 重试次数 |
|------|---------|---------|
| Token 估算 | 编码器异常 | 1 次（回退近似估算） |
| 压缩（summarize） | LLM 返回空或格式错误 | 2 次（指数退避） |
| 压缩（truncate） | 截断后仍超限 | 不重试，直接降级 |
| 段加载 | 临时 I/O 错误 | 3 次（1s 间隔） |
| 整体构建 | 预算超限 | 1 次（强制压缩） |

### 7.5 错误日志

```go
// Context 相关日志事件
builder.logger.Error("context build failed",
    "session", sessionID,
    "agent", agentID,
    "total_tokens", estimated,
    "max_tokens", cfg.MaxTokens,
    "error", err,
)

builder.logger.Warn("token budget exceeded, triggering compression",
    "session", sessionID,
    "current_tokens", current,
    "threshold", threshold,
)

builder.logger.Error("compression failed, fallback to truncate",
    "session", sessionID,
    "method", "summarize",
    "error", err,
)
```
