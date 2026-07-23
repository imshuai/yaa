# Memory 可观测性

> 上级: [Memory 系统设计](README.md)
> 错误语义: [errors.md](errors.md)

---

## 1. 日志

使用项目统一的结构化 logger。每次公开操作最多一条完成日志；失败、降级和恢复另写一条。日志可以包含 operation、AgentID、scope、key、version、duration 和 error class，但禁止包含 Content、metadata value、embedding、完整 query、API key 或响应正文。

| operation | 成功日志或 canonical mutation event | 失败/降级 event |
|-----------|--------------------------------------|------------------|
| `put` | event: `memory.added` / `memory.updated` / committed victims 的 `memory.evicted` | `memory.error` / `memory.degraded` |
| `get` | log: `memory.get` | `memory.error` |
| `search` | log: `memory.search` | `memory.error` / `memory.degraded` |
| `delete`/`clear` | event: `memory.deleted` | `memory.error` / `memory.degraded` |
| `promote` | event: `memory.promoted` / committed victims 的 `memory.evicted` | `memory.error` |
| `expire`/`evict` | event: `memory.expired` / `memory.evicted` | `memory.error` |
| `reindex` | log: `memory.reindex.completed` | `memory.degraded` / `memory.error` |

`key` 只作为结构化字段使用，不作为指标 label；需要脱敏的部署可配置 logger 对 key 做 hash。日志 level 由操作结果决定：正常写入 info，后台清理 debug，降级 warn，内容存储/损坏 error。

## 2. Canonical 事件

事件总线直接发布完整名称，不再给名称追加前缀。固定事件为：

```text
memory.added
memory.updated
memory.deleted
memory.promoted
memory.expired
memory.evicted
memory.degraded
memory.error
```

事件 payload：

```go
type Event struct {
    Type      string    `json:"type"`
    AgentID   string    `json:"agent_id"`
    Layer     Layer     `json:"layer"`
    SessionID string    `json:"session_id,omitempty"`
    Key       string    `json:"key"`
    Version   uint64    `json:"version,omitempty"`
    At        time.Time `json:"at"`
    Reason    string    `json:"reason,omitempty"`
    Created   *bool     `json:"created,omitempty"`
}
```

Payload 不包含 Content、metadata、embedding、query、API 凭据或底层错误正文。`memory.degraded` 的 Reason 使用有限枚举（`embedder`、`index_upsert`、`index_delete`、`reindex`）；`memory.error` 只包含稳定 error class。

只有 §2 列出的八个名称进入 event bus；`memory.get`、`memory.search` 和 `memory.reindex.completed` 只是结构化日志名。事件发布失败不回滚已提交内容；Manager 记录 `event_publish` error 并继续。消费者必须把事件当作通知而不是 source of truth，重启后可通过 ContentStore 查询当前状态。

## 3. 指标

指标前缀统一为 `yaa_memory_`，建议最小集合：

| 指标 | 类型 | labels | 说明 |
|------|------|--------|------|
| `yaa_memory_operations_total` | Counter | `operation`, `result` | 操作次数 |
| `yaa_memory_operation_duration_seconds` | Histogram | `operation` | 操作耗时 |
| `yaa_memory_items` | Gauge | `agent_bucket` | 当前未过期条目数；bucket 由部署配置决定 |
| `yaa_memory_errors_total` | Counter | `operation`, `error_class` | 稳定错误分类 |
| `yaa_memory_degraded` | Gauge | `component` | 0/1，`content`, `embedder`, `index` |
| `yaa_memory_expired_total` | Counter | `reason` | 过期清理次数 |
| `yaa_memory_evicted_total` | Counter | `policy` | 容量驱逐次数 |
| `yaa_memory_reindex_total` | Counter | `result` | Reindex 成功/失败 |

禁止将 Content、query、Key、SessionID、ToolID、完整 AgentID、向量值或错误文本作为 label。若需要按 Agent 排查，使用日志 trace 或显式管理端查询，不扩大指标基数。

## 4. 健康报告

```go
type Health struct {
    Status       string     `json:"status"` // healthy | degraded | unhealthy
    StoreOK      bool       `json:"store_ok"`
    EmbedderOK   *bool      `json:"embedder_ok,omitempty"`
    IndexOK      *bool      `json:"index_ok,omitempty"`
    Items        int64      `json:"items"`
    LastErrorAt  *time.Time `json:"last_error_at,omitempty"`
    ErrorClass   string     `json:"error_class,omitempty"`
}
```

判定规则（`IndexOK` 是所有启用 vector 的 Agent 状态的聚合）：

- `unhealthy`：ContentStore Ping/读取失败、schema/row 损坏或 Manager 已关闭。
- `degraded`：内容可读写但 embedder/index 不可用，且至少一个有效 Agent 启用了 vector；任一启用 vector 的 Agent 为 `IndexDegraded` 时 `IndexOK=false`。
- `healthy`：ContentStore 正常，未启用 vector（`IndexOK=nil`）或所有启用 vector 的 Agent 都为 `IndexReady`（`IndexOK=true`）。

健康检查不触发写入、不执行 embedding、不自动 Reindex；它只读取已有状态。Session 状态不影响 Memory 健康。

## 5. Remote 边界

Memory 事件只进入进程内 event bus；v1 没有独立 Memory SSE 路由，Remote API 不转发 `memory.*` 事件。Memory REST 响应直接返回结果，客户端通过已有 REST 读取当前状态，不依赖事件补齐状态。

## 6. 最小观测测试

1. Put/Update/Delete/Promote 各产生一个正确的 canonical event。
2. Index Upsert 失败只产生 degraded，不产生错误的 deleted/rollback event。
3. 任意指标样本都没有 Content、Key、SessionID 或错误正文 label。
4. 健康从 healthy→degraded→healthy 的转换只由 index/embedding 状态触发，Session Close 不改变它；per-Agent 状态只有完整 Reindex 才恢复 ready。

---

*最后更新: 2026-07-22*
