# Context 可观测性

> 文档路径: `docs/context/observability.md`
> 上级: `docs/context/README.md` §8

---

## 8. 可观测性

### 8.1 日志

```go
// Context 相关日志事件
builder.logger.Info("context built",
    "session", sessionID,
    "agent", agentID,
    "total_tokens", totalTokens,
    "segment_tokens", segmentTokens, // map[string]int
    "build_duration", duration,
    "strategy", strategy,
)

builder.logger.Info("context compressed",
    "session", sessionID,
    "method", "summarize",
    "before_tokens", beforeTokens,
    "after_tokens", afterTokens,
    "ratio", ratio,
    "duration", duration,
    "preserved_messages", preserved,
)

builder.logger.Warn("token budget near limit",
    "session", sessionID,
    "used_tokens", used,
    "max_tokens", maxTokens,
    "utilization", utilization,
)
```

### 8.2 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `context_build_total` | Counter | agent, strategy | Context 构建次数 |
| `context_build_duration` | Histogram | agent, strategy | 构建耗时（ms） |
| `context_total_tokens` | Gauge | agent, session | 当前 Context 总 Token 数 |
| `context_segment_tokens` | Gauge | agent, session, segment | 各段 Token 占用 |
| `context_token_utilization` | Gauge | agent, session | Token 利用率（0-1） |
| `context_compression_total` | Counter | agent, method | 压缩触发次数 |
| `context_compression_duration` | Histogram | agent, method | 压缩耗时（ms） |
| `context_compression_ratio` | Histogram | agent, method | 压缩比（after/before） |
| `context_compression_failed_total` | Counter | agent, method, reason | 压缩失败次数 |
| `context_overflow_total` | Counter | agent, strategy | 溢出策略触发次数 |
| `context_budget_exceeded_total` | Counter | agent | 预算超限次数 |

### 8.3 压缩前后对比

每次压缩记录前后 Token 对比，用于分析和调优：

```go
// recordCompression 记录压缩指标。
func (m *Metrics) recordCompression(sessionID, method string, before, after int, duration time.Duration) {
    ratio := float64(after) / float64(before)

    m.compressionTotal.WithLabelValues(m.agentID, method).Inc()
    m.compressionDuration.WithLabelValues(m.agentID, method).Observe(duration.Seconds() * 1000)
    m.compressionRatio.WithLabelValues(m.agentID, method).Observe(ratio)

    m.logger.Info("compression result",
        "session", sessionID,
        "method", method,
        "before", before,
        "after", after,
        "saved", before-after,
        "ratio", fmt.Sprintf("%.2f%%", ratio*100),
        "duration", duration,
    )
}
```

**压缩报告示例：**

| 会话 | 方法 | 压缩前 | 压缩后 | 节省 | 压缩比 | 耗时 |
|------|------|--------|--------|------|--------|------|
| sess-001 | summarize | 118000 | 68000 | 50000 | 57.6% | 2.3s |
| sess-002 | truncate | 126000 | 72000 | 54000 | 57.1% | 0.01s |
| sess-003 | summarize | 132000 | 79000 | 53000 | 59.8% | 3.1s |

### 8.4 Remote API 事件

Context 状态变化通过 Remote API SSE 推送：

| 事件 | 触发时机 | Payload |
|------|---------|---------|
| `context.built` | Context 构建完成 | session_id, total_tokens, segments |
| `context.compressed` | 压缩完成 | session_id, method, before, after, ratio |
| `context.overflow` | 溢出策略触发 | session_id, strategy, dropped_tokens |
| `context.error` | Context 发生错误 | session_id, error, type |

**SSE 示例：**

```json
{
  "event": "context.compressed",
  "data": {
    "session_id": "sess-001",
    "method": "summarize",
    "before_tokens": 118000,
    "after_tokens": 68000,
    "ratio": 0.576,
    "duration_ms": 2300,
    "timestamp": "2026-07-16T15:30:00Z"
  }
}
```

### 8.5 内置 Tool 集成

Context 系统与 Tool 系统的内视工具集成：

| Tool | 说明 |
|------|------|
| `context_status` | 查看当前 Context 各段 Token 占用与利用率 |
| `context_history` | 查看构建历史（最近 N 次构建的指标摘要） |
| `context_compress` | 手动触发压缩 |

详见 `docs/tool/introspection.md` §6.5.8。
