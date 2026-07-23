# Memory 系统设计

> 依赖: [Context](../context/README.md)、[Provider](../provider.md)、[Session](../session/README.md)

---

## 1. 职责与边界

Memory v1 是 Agent-scoped 的长期记忆内容库。它保存应用明确写入的事实、偏好和知识，并按需检索后交给 Agent 注入 Context。

- Session 负责完整、有序的短期对话历史和短期状态；Memory 不复制 Session 消息，不维护摘要层。
- `SessionID` 只是可选来源/过滤 scope，不改变 Memory 的 Agent 归属，也不随 Session Close/Delete 自动删除。
- ContentStore 是 source of truth；向量索引只是可重建的检索加速器。
- Memory 不调用 Provider 生成摘要，不决定何时把对话“记住”；显式 `Put` 或 `Promote` 才写入。

## 2. 权威模型

```go
type Layer string

const LayerLongTerm Layer = "long_term"

type Scope struct {
    AgentID  string
    SessionID string // 空值在 Search/Clear 表示 Agent 全范围；Get/Delete 中表示全局主键
    Layer    Layer
}

type MemoryItem struct {
    AgentID   string
    SessionID string
    Layer     Layer
    Key       string
    Content   string
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
    ExpiresAt *time.Time // nil 使用 default_ttl；zero time 指向值表示明确永不过期
    Version   uint64
}

type SearchRequest struct {
    Scope         Scope
    Query         string
    Limit         int
    Metadata      map[string]any // 顶层 JSON 值精确匹配
    IncludeGlobal bool           // Scope.SessionID 非空时同时检索全局 items
}

type SearchResult struct {
    Item  MemoryItem
    Score float64 // 关键词路径固定为 0；向量路径为 cosine score
}

type IndexStatus string

const (
    IndexReady    IndexStatus = "ready"
    IndexDegraded IndexStatus = "degraded"
)
```

唯一内容主键是 `(AgentID, Layer, SessionID, Key)`。v1 只接受 `LayerLongTerm`；未知 layer、空 AgentID/Key/Content 或超过固定字节上限的输入拒绝。所有时间使用 UTC。调用方必须把 managed fields 留为零；Manager 每次 mutation 只采样一次 `now`，并把它传给 ContentStore，由同一事务设置时间和 Version。

## 3. Manager API

```go
type Manager struct { /* ContentStore, optional Embedder/VectorIndex, lifecycle */ }

type PutResult struct {
    Item        MemoryItem
    Created     bool
    IndexStatus IndexStatus
}

func (m *Manager) Put(ctx context.Context, policy config.MemoryPolicy, item MemoryItem) (PutResult, error)
func (m *Manager) Get(ctx context.Context, policy config.MemoryPolicy, scope Scope, key string) (MemoryItem, error)
func (m *Manager) Search(ctx context.Context, policy config.MemoryPolicy, req SearchRequest) ([]SearchResult, error)
func (m *Manager) IndexStatus(agentID string) IndexStatus
func (m *Manager) Delete(ctx context.Context, policy config.MemoryPolicy, scope Scope, key string) error
func (m *Manager) Clear(ctx context.Context, policy config.MemoryPolicy, scope Scope) (int, error)
func (m *Manager) DeleteExpired(ctx context.Context, before time.Time, limit int) (int, error)
func (m *Manager) Promote(ctx context.Context, policy config.MemoryPolicy, source Scope, key string) (PutResult, error)
func (m *Manager) Reindex(ctx context.Context, policy config.MemoryPolicy, agentID string) (int, error)
func (m *Manager) Close(ctx context.Context) error
```

每个公开操作都接收调用方从同一 Config snapshot 解析出的 `config.MemoryPolicy`；Manager 不在操作中重新读取或缓存另一代 policy。`policy.Enabled=false` 直接返回 `ErrMemoryDisabled`。`Put` 是唯一写入/更新契约：同一主键 upsert，保留 CreatedAt，更新 Content/Metadata、UpdatedAt 和 Version；返回存储最终值和 created/index 状态。Get/Delete 必须提供完整 scope；Search/Clear 的空 SessionID 表示 Agent 全部来源，Get/Delete 的空值仍是全局主键。Search 的非空 SessionID 默认只查该来源，`IncludeGlobal=true` 时联合空 SessionID 的全局 items。`Limit=0` 使用传入 policy 的 `vector.top_k`，其他值必须在 1..100。

`Promote` 从带 SessionID 的源 item 复制到同 Agent 的全局 item（SessionID 置空），源不删除，目标按 Put 规则提交并重新应用 default TTL。`Reindex` 只接受 `agentID`，每次重建该 Agent 的全部来源，不提供按 SessionID 的局部重建；这样临时索引交换不会丢失同 Agent 的其他来源。`IndexStatus` 是唯一不调用 `beginOp` 的纯内存观察方法：它只在 `indexMu` 下读取快照，关闭后仍可读；vector 未启用或未知 Agent 返回 `IndexReady`，不读取或修改 MemoryItem。

## 4. TTL、容量和检索

- `ExpiresAt == nil` 时由 `default_ttl` 计算；default 为 0 表示永不过期；指向 zero time 的 pointer 明确表示永不过期。
- 已过期 item 对 Get/Search 不可见；物理删除由 `DeleteExpired` 批量完成。
- `max_items` 按 Agent 统计未过期 item。每次 Put 都按最终可见数量计算容量：新建或恢复已过期 row 的 delta 为 1，未过期更新的 delta 为 0；即使只是更新，也会在热缩后驱逐到新上限。victim 按 `fifo` 或 `ttl` 确定性选择并排除目标 key，和 upsert 在同一提交中完成。
- v1 固定上限：AgentID/SessionID 128 bytes、Key 256 bytes、Content 65536 bytes、JSON Metadata 16384 bytes。
- 向量默认关闭。启用后使用纯 Go 的进程内 exact cosine index；先写 ContentStore，再生成 embedding 并 Upsert。索引失败不回滚内容，标记 degraded 并由 Reindex 修复。
- Search 向量失败时仅在 `fallback_to_keyword=true` 下执行确定性的关键词匹配；否则返回稳定向量错误。

## 5. 事件与健康边界

Canonical 事件：`memory.added`、`memory.updated`、`memory.deleted`、`memory.promoted`、`memory.expired`、`memory.evicted`、`memory.degraded`、`memory.error`。只有 `CommitPut` 成功后才发布 mutation 事件；Promote 只发布 `memory.promoted`，但同一提交驱逐的每个 victim 仍发布 `memory.evicted`。payload 只含 scope、key、Version、时间和有限 reason，不含 Content、metadata value、embedding 或凭据。

指标统一 `yaa_memory_*`，不使用 Content、Key、SessionID 或 query 作为 labels。健康状态反映 ContentStore、Embedder 或 VectorIndex；Session 的 Created/Closed 状态与 Memory 健康无关。

## 6. 文档索引

| 文件 | 内容 |
|------|------|
| [config-ref.md](config-ref.md) | 根配置、Agent override、默认值和校验 |
| [storage.md](storage.md) | ContentStore、SQLite 和内存后端 |
| [lifecycle.md](lifecycle.md) | Put、更新、TTL、驱逐、过期、Promote、Reindex |
| [integration.md](integration.md) | Agent/Context/Session 集成和失败语义 |
| [architecture.md](architecture.md) | 组件边界、并发和启动顺序 |
| [errors.md](errors.md) | 稳定错误与降级策略 |
| [observability.md](observability.md) | 日志、指标、事件和健康 |
| [decisions.md](decisions.md) | 已确定的设计决策 |
| [checklist.md](checklist.md) | 实现与静态门禁 |

---

*最后更新: 2026-07-22*
