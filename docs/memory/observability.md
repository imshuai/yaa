# Memory 可观测性

> 文档路径: `docs/memory/observability.md`
> 上级: `docs/memory/README.md` §可观测性
> 依赖: `docs/architecture.md` §3.6, `docs/memory/errors.md`

---

## 1. 概述

Memory 系统的可观测性覆盖四个维度：**日志**、**指标**、**SSE 事件** 和 **健康检查**。所有可观测数据按 Agent 隔离，支持通过 Remote API 实时查询。

---

## 2. 日志事件

Memory 系统使用 `slog` 结构化日志，所有日志携带 `agent_id`、`layer`、`operation` 字段。

| 事件 | 级别 | 触发时机 | 关键字段 |
|------|------|----------|----------|
| `memory.add` | INFO | 写入记忆 | `key`, `layer`, `agent_id` |
| `memory.get` | DEBUG | 按 key 读取 | `key`, `found` |
| `memory.search` | INFO | 执行检索 | `query`, `limit`, `result_count`, `latency_ms` |
| `memory.delete` | INFO | 删除记忆 | `key` |
| `memory.clear` | WARN | 清空作用域记忆 | `scope`, `cleared_count` |
| `memory.promote` | INFO | 短期晋升长期 | `key`, `from_layer`, `to_layer` |
| `memory.expire` | DEBUG | 过期清理 | `expired_count` |
| `memory.search.fallback` | WARN | 向量搜索降级关键词 | `reason` |
| `memory.error` | ERROR | 操作异常 | `operation`, `error` |

```go
// 结构化日志示例
m.logger.Info("memory.add",
    slog.String("agent_id", agentID),
    slog.String("key", key),
    slog.String("layer", layer.String()),
)
```

---

## 3. 指标

Memory Manager 通过内置指标注册表暴露 Prometheus 兼容指标。

| 指标 | 类型 | 说明 |
|------|------|------|
| `yaa_memory_ops_total` | Counter | 记忆操作总数（按 `operation` label） |
| `yaa_memory_ops_duration_seconds` | Histogram | 操作耗时分布 |
| `yaa_memory_items_count` | Gauge | 当前记忆条数（按 `agent_id`、`layer` label） |
| `yaa_memory_search_results` | Histogram | 检索返回结果数分布 |
| `yaa_memory_search_score` | Histogram | 检索相关性分数分布 |
| `yaa_memory_errors_total` | Counter | 错误总数（按 `operation`、`error_type` label） |
| `yaa_memory_storage_size_bytes` | Gauge | 存储占用大小 |

```go
// 指标注册示例
var (
    opsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "yaa_memory_ops_total",
        },
        []string{"operation", "layer"},
    )
    opsDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "yaa_memory_ops_duration_seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"operation"},
    )
)
```

---

## 4. SSE 事件

Memory 相关的 Remote API 事件通过 SSE 推送给订阅客户端。

| 事件类型 | 方向 | 说明 |
|----------|------|------|
| `memory.added` | Server → Client | 新记忆写入 |
| `memory.updated` | Server → Client | 记忆内容更新 |
| `memory.deleted` | Server → Client | 记忆被删除 |
| `memory.search_completed` | Server → Client | 检索完成，携带结果摘要 |
| `memory.promoted` | Server → Client | 短期记忆晋升长期 |
| `memory.expired` | Server → Client | 记忆过期清理 |

```go
// SSE 事件推�示例
type MemoryEvent struct {
    Type     string         `json:"type"`      // 事件类型
    AgentID  string         `json:"agent_id"`
    Key      string         `json:"key"`
    Layer    MemoryLayer    `json:"layer"`
    Metadata map[string]any `json:"metadata,omitempty"`
    Timestamp time.Time     `json:"timestamp"`
}

func (m *MemoryManager) emitEvent(event MemoryEvent) {
    m.eventBus.Publish("memory."+event.Type, event)
}
```

---

## 5. 健康检查

```go
// MemoryHealthReport 是 Memory 系统的健康报告。
type MemoryHealthReport struct {
    Status     string             // "healthy" | "degraded" | "unhealthy"
    Backend    string             // 存储后端名称
    StoreOK    bool               // 存储连接正常
    EmbedderOK bool               // 向量嵌入器可用
    ItemsTotal int64             // 总记忆条数
    Details    []ComponentHealth  // 各组件详情
}
```

| 检查项 | 判定逻辑 |
|--------|----------|
| 存储连接 | 对存储后端执行 ping，超时 2s |
| 向量嵌入 | 对 Embedder 执行一次小规模嵌入测试 |
| 磁盘空间 | 存储占用超过阈值（默认 90%）时 degraded |
| 记忆数量 | 单 Agent 超过 `max_items`（默认 100000）时告警 |

```go
// HealthCheck 检查 Memory 系统健康状态。
func (m *MemoryManager) HealthCheck() MemoryHealthReport {
    report := MemoryHealthReport{Backend: m.config.Backend}
    if err := m.store.Ping(m.ctx); err != nil {
        report.StoreOK = false
        report.Status = "unhealthy"
    } else {
        report.StoreOK = true
        report.Status = "healthy"
    }
    if m.embedder != nil {
        report.EmbedderOK = m.embedder.Healthy()
        if !report.EmbedderOK && report.Status == "healthy" {
            report.Status = "degraded" // 向量降级，仍可服务
        }
    }
    return report
}
```

---

*最后更新: 2026-07-17*
