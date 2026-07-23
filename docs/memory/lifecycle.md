# Memory 生命周期

> 上级: [Memory 系统设计](README.md)
> 存储提交语义: [storage.md](storage.md)

---

## 1. 输入所有权与固定限制

Manager 对输入做深拷贝，调用方之后修改原 slice/map 不得改变已保存内容。v1 使用固定上限，避免为安全边界再增加配置项：

| 输入 | 上限 | 计算方式 |
|------|------|----------|
| `AgentID` | 128 bytes | UTF-8 字节数 |
| `SessionID` | 128 bytes | UTF-8 字节数；可为空 |
| `Key` | 256 bytes | UTF-8 字节数；不可为空 |
| `Content` | 65536 bytes | UTF-8 字节数；不可为空 |
| `Metadata` | 16384 bytes | 标准 JSON 编码后的字节数 |
| Search `Limit` | 100 | 0 使用 effective `vector.top_k`，负数或大于 100 拒绝 |

Metadata 必须能编码为 JSON object；不接受 NaN、Infinity、函数、channel 或循环引用。顶层 metadata filter 只做 key 存在且 JSON 值深度相等匹配，不做范围、正则或嵌套路径查询。

调用方必须把 `CreatedAt`、`UpdatedAt` 和 `Version` 留为零值；非零输入返回 `ErrMemoryManagedField`。Manager 从唯一 Clock 采样一次 `now`，ContentStore 只使用这个值在事务内设置时间和 Version，防止伪造或同一次提交出现多套时间。

## 2. Put

`Put` 是唯一创建/更新入口：

```go
func (m *Manager) Put(ctx context.Context, policy config.MemoryPolicy, item MemoryItem) (PutResult, error)
```

步骤：

1. 检查 ctx、Manager 状态、传入 policy、Layer 和固定输入上限；从 Clock 采样一次 `now`。
2. 深拷贝 Content/Metadata，并把所有时间归一化为 UTC。
3. 解析 `ExpiresAt`：
   - nil：`default_ttl>0` 时设为 `now+default_ttl`，否则保存为永不过期；
   - 指向 zero time：明确永不过期；
   - 非零且 `<=now`：返回 `ErrMemoryExpiredInput`；
   - 其他值：使用调用方给定的绝对时间。
4. 按 `mutationGate.RLock -> Agent keyed lock` 获取锁，读取当前未过期 count 和目标 row。未过期目标的 delta=0；目标不存在或已过期的 delta=1。
5. 计算 `victimCount=max(0, liveCount+delta-policy.MaxItems)`，按 policy 选择并排除目标完整主键；不足时返回 `ErrMemoryQuota`，不修改内容。
6. 调用 `ContentStore.CommitPut(item, victimRefs, now)`。更新完整替换 Content、Metadata 和 ExpiresAt，不做 merge；nil ExpiresAt 在每次 Put 都重新应用传入 policy 的 default TTL。victim 删除与 target upsert 是唯一 commit point。
7. Store 返回最终 item、Version、created 和实际 evicted items；提交后发布 `memory.added` 或 `memory.updated`，并为每个 victim 发布 `memory.evicted`。所有事件的 At 都使用同一个 `now`。
8. 提交后删除 victims 的 index refs，再为 target 生成 embedding 并 Upsert index。任一步失败仅把向量能力标记 `IndexDegraded`，Content commit 仍返回成功；`PutResult.IndexStatus` 返回该 Agent 操作后的 typed 状态。

同一个已过期物理 row 在 Put 时仍按同一物理主键更新：保留 `CreatedAt`、递增 `Version`，并更新内容、`UpdatedAt` 和 TTL。它不是 Version 1 的新 row；事件 reason 为 `replaced_expired` 时仍可用于观测。

## 3. Get、Search 和可见性

### 3.1 Get

`Get` 要求完整 Scope。空 SessionID 表示 Agent 全局 item，而不是通配符。ContentStore 未命中或 item 已过期都返回 `ErrMemoryNotFound`；过期内容可由后台 worker 稍后物理删除。

### 3.2 Search

Search/Clear 中空 SessionID 才表示 Agent 所有来源；非空表示只查该 Session 来源。Layer 始终为 `long_term`。

```go
type SearchRequest struct {
    Scope         Scope
    Query         string
    Limit         int
    Metadata      map[string]any
    IncludeGlobal bool
}
```

关键词路径对 `Key` 和 `Content` 做 Unicode lowercase 后的 substring 匹配；Query 为空表示不做文本过滤。`IncludeGlobal` 只允许在 Scope.SessionID 非空时为 true，此时结果是该 Session 来源与空 SessionID 全局 items 的并集。先应用 scope、TTL 和 metadata filter，再按 `UpdatedAt DESC, SessionID ASC, Key ASC` 排序。关键词 `SearchResult.Score` 固定为 0。

向量只在 vector enabled 且 Query 非空时使用：

- `similarity_threshold` 只过滤向量 score，不影响关键词结果。
- exact index 先按 Agent/Layer 以及 Session/global union scope 过滤，再按 threshold、score 降序和 SessionID/Key 升序返回全部候选；其他 Session 的高分结果不能挤占候选。
- Manager 依次从 ContentStore 回查每个 hit，校验 scope、Version、TTL 和 metadata，收集到最终 limit 后停止；后置过滤不会导致可用结果下溢。
- embed/index 失败且 `fallback_to_keyword=true` 时执行完整关键词路径；否则返回对应稳定错误。
- fallback 是一次性选择，不重试、不把 limit 扩大为无界结果。

## 4. Delete 和 Clear

```go
func (m *Manager) Delete(ctx context.Context, policy config.MemoryPolicy, scope Scope, key string) error
func (m *Manager) Clear(ctx context.Context, policy config.MemoryPolicy, scope Scope) (int, error)
```

Delete 未命中返回 `ErrMemoryNotFound`；Clear 未命中成功返回 0。两者按 `mutationGate.RLock -> Agent keyed lock` 串行，先提交 ContentStore 删除，再删除 index ref 并发布 `memory.deleted`。Index 删除失败不会改变成功的内容删除，而是发布 `memory.degraded`；后续 Reindex 清理陈旧 ref。

Clear 的空 SessionID 删除该 Agent 的全部 long-term items；非空只删除该 Session 来源的 items。删除 Memory 不修改 Session，删除 Session 也不修改 Memory。

## 5. TTL 清理

Runtime 只有一个 expiration worker。每个 tick 重复调用：

```go
func (m *Manager) DeleteExpired(ctx context.Context, before time.Time, limit int) (int, error)
```

`before` 转为 UTC；`limit` 必须在 1..10000。Manager 持有 `mutationGate.Lock`，不再获取任何 Agent keyed lock；ContentStore 按过期时间和完整主键稳定排序，一次事务删除至多 limit 个 `ExpiresAt <= before` 的 item，并返回被删 items。Manager 清理对应 index ref，逐条发布 `memory.expired`。

若返回数等于 limit，worker 在同一个 tick 继续下一批，但每批重新检查 ctx；任一批失败即停止本 tick，下个 tick 重试。配置 reload 只改变下一次 tick 周期和 batch size，不重写已有 ExpiresAt。

## 6. 容量驱逐

`max_items` 按 Agent 统计所有 SessionID 下未过期的 long-term items。Put 新 key 前若无空位，Manager 在 Agent write lock 内选择 victim：

| policy | 排序 |
|--------|------|
| `fifo` | `CreatedAt ASC, SessionID ASC, Key ASC` |
| `ttl` | 有期限的 item 按 `ExpiresAt ASC`，永不过期排最后；再按 CreatedAt/SessionID/Key |

未过期更新的 count delta 为 0，但若 `max_items` 热更新后变小，仍会驱逐直到最终 count 不超过新上限；已过期 row 恢复可见的 delta 为 1。victim 必须排除 target。`CommitPut` 在一个事务/写锁内删除全部 victims 并 upsert target；任何失败整体回滚，提交成功后才发布 `memory.evicted`。

## 7. Promote

```go
func (m *Manager) Promote(ctx context.Context, policy config.MemoryPolicy, source Scope, key string) (PutResult, error)
```

source 必须带非空 SessionID。Promote 按 `mutationGate.RLock -> Agent keyed lock` 获取一次锁，读取源 item，复制 Content 和 Metadata，以 `SessionID=""` 和同一个 Key 调用不重复加锁的 `putLocked`：

- 源 item 保留，不是 move。
- 目标已存在时按 Put upsert；目标 Version 按自己的历史递增。
- Manager 单次采样 commit time，ContentStore 用它设置目标的 CreatedAt/UpdatedAt。
- 目标 `ExpiresAt=nil`，因此重新应用当前 Agent `default_ttl`，不继承源的绝对过期时间。
- 成功只为目标发布一次 `memory.promoted`，payload 指明 source SessionID、目标 Version 和 created 标记；不再重复发布 added/updated。同一 CommitPut 驱逐的每个 victim 仍在提交后发布一次 `memory.evicted`。

Session Close、Pause、Restore 或 Delete 不调用 Promote。只有显式应用逻辑或 Remote API 请求可以触发它。

## 8. Reindex

```go
func (m *Manager) Reindex(ctx context.Context, policy config.MemoryPolicy, agentID string) (int, error)
```

Reindex 要求 vector enabled 且 `agentID` 非空；它只重建该 Agent 的全部 Session 来源和全局 item。实现按 `mutationGate.RLock -> Agent keyed lock`，执行 List -> Embed -> `indexFactory()` 临时 exact index -> pointer/status swap；期间 Put/Delete/Clear/Promote 被阻塞，Search 可读取旧 pointer。ctx 取消、dimension 错误或任一 embedding/index 失败时保留旧 index，标记 `IndexDegraded`，并返回同时包装 `ErrMemoryReindexFailed` 和底层错误的结果；不得留下半张新索引或重放变更。只有完整 swap 成功才设为 `IndexReady`。

## 9. 最小测试矩阵

| 场景 | 预期 |
|------|------|
| 新建、更新同一复合主键 | Version 1→2，CreatedAt 不变，Metadata 完整替换 |
| nil/zero/绝对 ExpiresAt | 分别应用 default、永久、指定时间 |
| 已过期 key 再 Put | 保留 CreatedAt，Version 递增 |
| Put 与 victim 删除中任一步失败 | target/victims/事件/index 均保持提交前状态 |
| max_items 热缩后更新现存 key | target 保留且按新上限原子驱逐其他 item |
| 同 Key、不同 SessionID | 两个独立 item |
| fifo/ttl 并列 | 按固定 tie-break 确定 victim |
| vector stale Version hit | 丢弃，不返回旧内容 |
| 当前 Session + global 向量搜索 | 其他 Session 的高分 hit 不占候选；metadata/TTL 后仍填满可用 limit |
| index Upsert/Delete 失败 | 内容结果不回滚，健康为 degraded |
| vector disabled / 初始 Reindex 失败 / 成功 Reindex | 状态分别为 ready / degraded / ready |
| Promote 覆盖并触发驱逐 | 源保留，目标 Version 递增；目标只发 promoted，victim 发 evicted |
| ctx 在 cleanup/Reindex 中取消 | 尽快停止，无半提交批次/半张索引 |

---

*最后更新: 2026-07-22*
