# Memory 架构

> 上级: [Memory 系统设计](README.md)
> 相关: [存储](storage.md)、[生命周期](lifecycle.md)、[集成](integration.md)

---

## 1. v1 边界

Memory v1 只保存 Agent 的长期记忆。Session 保存完整对话历史；Context 只接收 Agent 已经检索并格式化好的 Memory 输入。Memory 不复制 Session 消息、不生成摘要，也不在 Session Close/Delete 时自动写入或删除内容。

```text
Remote API / Agent
        |
        v
+---------------- Memory Manager ----------------+
| validate scope and policy                       |
| Put/Get/Search/Delete/Clear/Promote/Reindex     |
| TTL cleanup, capacity eviction, events, health |
+-------------------+-----------------------------+
                    |
          +---------+----------+
          v                    v
  ContentStore             Embedder
  source of truth              |
  sqlite | memory              v
                         VectorIndex
                         derived cache
```

内容存储提交成功后，Memory item 就已经成功写入。Embedding 和向量索引都是可删除、可重建的派生数据，不能成为读取单条 item 的真实来源。

## 2. 进程内组件

Runtime 创建一个共享 `Manager`。Manager 只持有根基础设施；每个 Agent turn、Remote request 或 cleanup tick 在入口捕获一个 Config snapshot，解析出 `config.MemoryPolicy` 后显式传给该次 Memory 操作。Manager 不在操作中重新读取配置，因此同一 turn 不会混用两代 policy。

```go
type Manager struct {
    store        ContentStore
    embedder     Embedder // vector 未启用时为 nil
    indexFactory VectorIndexFactory
    indexes      map[string]*agentIndexState
    indexMu      sync.RWMutex
    mutationGate sync.RWMutex
    agentLocks   keyedMutex
    clock        Clock
    workerCancel context.CancelFunc
    workerDone   chan struct{}
    lifecycleMu  sync.Mutex
    closing      bool
    inFlight     sync.WaitGroup
    closeOnce    sync.Once
    closeDone    chan struct{}
    closeErr     error
}

type agentIndexState struct {
    mu     sync.RWMutex
    index  VectorIndex
    status IndexStatus
}

type VectorIndexFactory func() VectorIndex
```

除纯内存 `IndexStatus` 外，每个公开操作在访问 Store/Index 前调用 `beginOp`，并 `defer m.inFlight.Done()`。`lifecycleMu` 使“检查 closing + Add”成为一个临界区；Close 先在同一锁内设置 closing，再启动 Wait，因此不存在 `WaitGroup.Add` 与 `Wait` 竞态：

```go
func (m *Manager) beginOp() error {
    m.lifecycleMu.Lock()
    defer m.lifecycleMu.Unlock()
    if m.closing {
        return ErrMemoryClosed
    }
    m.inFlight.Add(1)
    return nil
}
```

启动时为每个启用 vector 的 Agent 创建 `agentIndexState`；初始状态为 `IndexDegraded`，直到完整 Reindex 成功。vector 未启用的 Agent 不需要 index state，`IndexStatus` 固定返回 `IndexReady`。`indexFactory` 必须每次返回新的非 nil 空索引，不得复用当前索引或跨 Agent 共享；完整 Reindex 成功后才把新 pointer 和 `IndexReady` 一起发布。任何 embedding、Upsert、Delete、Search 或 Reindex 失败都把该 Agent 标记为 `IndexDegraded`；普通操作成功不会清除历史 degraded，只有完整 Reindex 能恢复 ready。

Manager 对同一 Agent 的内容变更使用 keyed mutex 串行化，以保证容量检查、Put、Delete、Clear、Promote 和 Reindex 的组合语义。唯一锁顺序是 `mutationGate -> Agent keyed lock -> index state lock`；所有普通 mutation 和 Reindex 持 `mutationGate.RLock`，全局 `DeleteExpired` 持 `mutationGate.Lock` 且不再获取 Agent keyed lock，因此 cleanup 不会与其他 mutation 交错。不同 Agent 的普通 mutation 可以并行；Get 依赖 ContentStore 的并发保证，Search 还依赖并发安全的 VectorIndex。

## 3. ContentStore

`ContentStore` 是 Memory 内部接口，不等同于根 `storage.Storage` KV 接口。它需要理解复合主键、过期时间和确定性查询。

```go
type ContentStore interface {
    CommitPut(ctx context.Context, item MemoryItem, victims []ItemRef, now time.Time) (CommitPutResult, error)
    Get(ctx context.Context, scope Scope, key string) (MemoryItem, error)
    Search(ctx context.Context, req SearchRequest, now time.Time) ([]MemoryItem, error)
    List(ctx context.Context, scope Scope, now time.Time) ([]MemoryItem, error)
    Delete(ctx context.Context, scope Scope, key string) (MemoryItem, error)
    Clear(ctx context.Context, scope Scope) ([]MemoryItem, error)
    DeleteExpired(ctx context.Context, before time.Time, limit int) ([]MemoryItem, error)
    Count(ctx context.Context, agentID string, now time.Time) (int, error)
    Ping(ctx context.Context) error
    Close() error
}

type CommitPutResult struct {
    Stored  MemoryItem
    Created bool
    Evicted []MemoryItem
}
```

契约：

- `CommitPut` 在一个事务内校验 victims、删除 victims 并按完整主键 upsert；任一步失败都回滚全部内容。
- `CommitPut` 使用调用方传入的单次 `now` 设置 `CreatedAt`/`UpdatedAt` 和 Version；Store 不自行取时钟。
- `Get` 返回物理记录，包括已经过期但尚未清理的记录；Manager 负责将其映射为 `ErrMemoryNotFound`。
- `Search`、`List` 和 `Count` 必须排除 `ExpiresAt <= now` 的记录。
- `Search` 只执行关键词检索和 metadata 精确过滤；结果按 `UpdatedAt DESC, SessionID ASC, Key ASC` 排序。
- `SearchRequest.IncludeGlobal` 为 true 时，非空 SessionID 与空 SessionID 的全局 items 做并集；false 只查指定来源，空 SessionID 仍表示全 Agent 范围。Reindex 通过 `List` 使用同样的空 SessionID 全范围语义；除显式 policy 外，它的唯一选择器是 `agentID`。
- `Delete` 未命中返回 `ErrMemoryNotFound`；`Clear` 未命中返回空切片。
- 返回的 item、metadata 和 slice 不得引用实现内部的可变内存。

`List` 最多返回一个 Agent 的当前 item。`max_items` 默认 10000，因此 v1 不增加 cursor 接口；容量上限发生实测问题后再引入分页。

## 4. Embedder 和 VectorIndex

```go
type Embedder interface {
    Embed(ctx context.Context, inputs []string) ([][]float32, error)
    Dimension() int
}

type ItemRef struct {
    AgentID   string
    SessionID string
    Layer     Layer
    Key       string
    Version   uint64
}

type VectorHit struct {
    Ref   ItemRef
    Score float64
}

type VectorSearchRequest struct {
    AgentID       string
    Layer         Layer
    SessionID     string
    IncludeGlobal bool
    Query         []float32
    Threshold     float64
}

type VectorIndex interface {
    Upsert(ctx context.Context, ref ItemRef, vector []float32) error
    Delete(ctx context.Context, ref ItemRef) error
    Search(ctx context.Context, req VectorSearchRequest) ([]VectorHit, error)
}
```

`VectorIndex` 的 `Upsert`、`Delete` 和 `Search` 必须并发安全；v1 exact 实现用内部 `sync.RWMutex` 保护向量 slice。Search 必须先按 `AgentID + Layer + (SessionID 或 SessionID 与空值并集)` 过滤，再应用 threshold 和排序；不会让其他 Session 的高分结果占用候选。v1 exact index 返回全部符合 scope/threshold 的有序候选，不在索引层截断 limit，Manager 回查 ContentStore 的 Version/TTL/metadata 后再截最终 limit，因此允许的结果不会因后置过滤而下溢。`VectorIndexFactory` 每次返回一个空的 exact index；它是 Reindex 的临时索引构造器，不能复用当前 index。每个 Agent 的 `agentIndexState.index` 在短暂 `state.mu` 临界区交换；Search 先复制 pointer 后再调用，因而不会读到半张索引。

v1 只有一个 `VectorIndex` 实现：进程内精确余弦检索。它使用 Go slice 保存向量，按 score 降序，再按 `(SessionID, Key)` 升序打破并列。索引不持久化；启用向量的 Runtime 在 Ready 前对每个 Agent 用初始 policy 执行全量 `Reindex(policy, agentID)`。这个实现的上限由 `max_items` 控制，暂不引入额外索引依赖。

`VectorIndex` 不暴露会先破坏旧状态的 `Reset`；Reindex 在同一 Agent write lock 内完成 List -> Embed -> 临时 exact index -> pointer swap，期间 Put/Delete/Clear/Promote 被阻塞，Search 可继续读取旧 pointer。任一失败保留旧 pointer、标记该 Agent `degraded`，并返回同时可由 `errors.Is` 判断的 `ErrMemoryReindexFailed` 与底层分类错误。

每个向量携带 item Version。Search 从 ContentStore 重新读取命中的 item，并丢弃不存在、过期或 Version 不匹配的 hit，因此旧索引不会返回陈旧内容。

## 5. 核心数据流

### 5.1 Put

```text
validate item and effective policy
  -> acquire Agent write lock
  -> normalize TTL and check whether key is new
  -> make capacity room when needed
  -> ContentStore.CommitPut(item, victims, now) (single commit point)
  -> emit added/updated and each evicted event
  -> vector enabled? embed and index Upsert
  -> index failure: mark degraded, emit memory.degraded, keep Put successful
```

任何 CommitPut 错误都在 single commit point 前向调用方返回；victim、target、index 和事件都不应出现部分提交。事件发布失败不回滚已提交内容。

### 5.2 Search

```text
validate scope/query/limit
  -> vector enabled and query non-empty?
       yes: embed query -> index Search -> verify hits in ContentStore
       no:  ContentStore.Search
  -> vector path failed or index degraded, and fallback_to_keyword=true?
       yes: ContentStore.Search
       no:  return stable vector/embed/index-degraded error
  -> return at most limit deep-copied results
```

向量结果按 score 排序。关键词结果的 `Score` 固定为 0，并使用 ContentStore 的确定性顺序。两个路径都应用相同的 scope、过期和 metadata 过滤。

### 5.3 Delete、Clear 和过期清理

ContentStore 删除是 commit point。之后的 index Delete 失败只会让索引进入 degraded；Search 的 Version/存在性校验仍会阻止已删除内容泄漏。`Reindex` 通过新建 exact index 并交换 pointer 清理全部陈旧引用。

## 6. 启动、健康和关闭

启动顺序：

```text
load and validate Config
  -> open ContentStore and run schema migration
  -> build effective Agent policies
  -> create optional Embedder and per-Agent VectorIndex state from indexFactory
  -> Reindex every vector-enabled Agent (each call covers all sources)
  -> start one expiration worker
  -> Runtime Ready
```

若 ContentStore 无法打开或 schema 不兼容，Runtime Not Ready。初始 Reindex 失败时，`fallback_to_keyword=true` 的 Agent 可以 degraded Ready；否则 Runtime Not Ready。Reindex 失败永远保留旧 index；所有启用 vector 的 Agent 完整 Reindex 成功后才把各自状态设为 ready。

`Close(ctx)` 是 Manager 唯一关闭入口且幂等。构造器把 `workerDone` 初始化为非 nil channel；若启动在 worker 建立前失败，rollback 先关闭它。第一次调用的 `closeOnce.Do` 在 `lifecycleMu` 下设置 closing 并取消 expiration worker，然后启动一个后台 closer：先等待 `workerDone`，再 `inFlight.Wait()`，最后只调用一次 `ContentStore.Close`、保存 `closeErr` 并关闭 `closeDone`。设置 closing 后 `beginOp` 不再 Add，而 Wait 只在释放同一锁后开始。每次 `Close` 都 select `closeDone` 与自己的 `ctx.Done()`；ctx 到期返回 `context.Cause(ctx)`，后台关闭继续，后续调用可等待同一个最终结果。进程内索引无需持久化。关闭后的新 I/O 操作返回 `ErrMemoryClosed`；`IndexStatus` 仍可读取最终快照。Runtime 不由 Agent 关闭共享 Memory。

## 7. 不变量

1. 唯一内容主键始终是 `(AgentID, Layer, SessionID, Key)`。
2. v1 只接受 `LayerLongTerm`。
3. 对外可见内容一定来自 ContentStore，而不是 vector hit 本身。
4. 已过期内容不会被 Get/Search/List 返回。
5. 同一 Agent 的未过期 item 数不高于其有效 `max_items`；每次 Put 都按最终可见数量收敛，且 CommitPut 原子包含 victims。
6. Session 生命周期不触发 Memory 写入、晋升或删除。
7. 日志、指标和事件不包含 Content、embedding 或凭据。

---

*最后更新: 2026-07-22*
