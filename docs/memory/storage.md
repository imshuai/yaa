# Memory 存储

> 上级: [Memory 系统设计](README.md)
> 接口与组件: [架构](architecture.md)

---

## 1. ContentStore 契约

Memory 内容使用专用 `ContentStore`，因为根 `storage.Storage` 只有 KV 读写，不能表达 Memory 的复合主键、查询排序和原子版本更新。两者可以使用同一个进程，但不是同一个接口。

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
```

`scope.Layer` 必须是 `long_term`。在 `Get`/`Delete` 中空 `SessionID` 是一个真实的全局主键值，不表示“全部 Session”；全范围语义对 `Search`、`Clear` 和 `List` 开放。Manager 的全量 Reindex 调用 `List` 时传入空 `SessionID`，但公开 Reindex API 只接收 `agentID`。

`CommitPut` 是 target upsert 和容量驱逐的唯一存储事务：

1. 校验 target 完整主键、JSON metadata、`now` 和每个 victim `ItemRef`；victim 不得等于 target，Version 必须仍匹配。
2. 删除全部 victims；任一 ref 不存在或 Version 不匹配时返回 `ErrMemoryQuota` 并回滚。
3. 找到 target 时保留 `CreatedAt` 并将 Version 加一；否则以 Version 1 创建。
4. 使用 Manager 传入的同一个 UTC `now` 设置新建的 CreatedAt 和 target UpdatedAt；Store 不读取另一只时钟。
5. 同一事务替换完整 Content 和 Metadata，不做字段级 merge，并返回 stored target、created 和实际 evicted items 的深拷贝。

Manager 传入的 `CreatedAt`、`UpdatedAt` 和 `Version` 不具有写权限；恢复数据使用受校验的存储快照导入路径，而不是调用公开 `Put`。

## 2. SQLite 实现

默认后端使用 `modernc.org/sqlite` 的纯 Go 驱动，不依赖 CGO。Memory SQLite 文件由 `memory.storage.path` 指定；目录不存在时创建目录，无法创建或迁移失败则启动失败。

最小 schema 如下，时间以 RFC3339Nano UTC 文本保存：

```sql
CREATE TABLE memory_items (
    agent_id   TEXT NOT NULL,
    layer      TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    item_key   TEXT NOT NULL,
    content    TEXT NOT NULL,
    metadata   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    expires_at TEXT,
    version    INTEGER NOT NULL,
    PRIMARY KEY (agent_id, layer, session_id, item_key)
);

CREATE INDEX memory_items_agent_updated
    ON memory_items (agent_id, layer, updated_at DESC, session_id, item_key);

CREATE INDEX memory_items_expiry
    ON memory_items (expires_at)
    WHERE expires_at IS NOT NULL;
```

关键词 Search 不依赖全文索引扩展：读取当前 Agent 的候选 rows，在 Go 中按大小写折叠后的 `item_key`/`content` 做 substring 匹配，再应用 metadata 顶层精确过滤和确定性排序。`max_items` 默认 10000，候选集有明确上限；单个 item 也有固定字节上限，避免无界扫描。

SQL 操作要求：

- CommitPut 使用一个事务完成带 Version 条件的 victim 删除和 `INSERT ... ON CONFLICT ... DO UPDATE`，并在同一事务内计算 target Version；错误时全部回滚。
- Delete/Clear/DeleteExpired 使用事务；返回实际删除的完整 item，供 Manager 清理派生索引。
- `expires_at <= now` 的 rows 不由 Search/List/Count 返回；DeleteExpired 按 `expires_at ASC, agent_id, session_id, item_key` 扫描。
- 数据库错误原样包装为 `ErrMemoryStoreUnavailable`，不转换为“空结果”。JSON 解码错误返回 `ErrMemoryCorrupt`，不能跳过坏 row。

## 3. Memory 实现

`memory.storage.type=memory` 使用进程内 `map[PrimaryKey]MemoryItem` 和 `sync.RWMutex`。它遵守与 SQLite 相同的主键、版本、TTL、排序和拷贝规则；进程退出后数据丢失。该后端只适合测试或明确接受丢失的临时运行。

```go
type PrimaryKey struct {
    AgentID   string
    Layer     Layer
    SessionID string
    Key       string
}
```

Memory 后端的 `CommitPut` 在一个写锁临界区校验/删除 victims 并完成 upsert，失败时不得留下部分 map 修改；可先在副本计算再交换，或验证全部条件后执行不会失败的 mutation。`List` 返回副本并按同一排序规则输出。它不使用 timer 为每个 item 建立 goroutine；统一 expiration worker 调用 `DeleteExpired`。

## 4. 过期、容量和索引的一致性

ContentStore 负责 target 与 victims 的原子内容提交。Manager 负责：

- 在 Put 前将 nil `ExpiresAt` 解析为有效 policy 的 `default_ttl`；显式指向 zero time 表示永不过期。
- 在同一 Agent 写锁内计算最终 count 和 victim refs，再通过一次 CommitPut 提交，保持 `max_items` 上限。
- Content commit 后调用并发安全的 `VectorIndex.Upsert`；失败不回滚 content，而是记录 `IndexDegraded` 状态。
- Delete/Clear/DeleteExpired 后调用 `VectorIndex.Delete`；索引失败不复活已删除内容。

索引引用必须包含完整 `ItemRef`（AgentID、SessionID、Layer、Key、Version）。ContentStore 更新 Version 后，旧引用自然失效；Reindex 从当前 ContentStore items 生成一个新的 exact index，全部成功后在 Agent write lock 下交换 pointer 和 status，不先破坏旧索引。

## 5. 可重建向量索引

v1 的索引实现是内存 exact cosine index。它不写入 ContentStore，不改变 ContentStore 的成功/失败语义：

```go
func cosine(a, b []float32) (float64, error) {
    if len(a) == 0 || len(a) != len(b) {
        return 0, ErrMemoryEmbeddingDimension
    }
    var dot, na, nb float64
    for i := range a {
        x, y := float64(a[i]), float64(b[i])
        dot += x * y
        na += x * x
        nb += y * y
    }
    if na == 0 || nb == 0 {
        return 0, ErrMemoryEmbeddingZero
    }
    return dot / (math.Sqrt(na) * math.Sqrt(nb)), nil
}
```

Index `Search` 只返回超过 threshold 的 hit，按 score 降序、`SessionID`/`Key` 升序打破并列。Manager 必须重新从 ContentStore 读取并校验 Version 后再暴露结果。

启用 vector 时，Runtime 必须提供 `Embedder`。内置 HTTP embedder 使用 `POST {base_url}/embeddings`，请求字段为 `model`、`input`，响应必须提供与配置 dimension 相同的浮点数组；非 2xx、超时、格式错误或 dimension 不匹配都返回 embedding 错误。响应正文和输入内容不得写入日志。

## 6. 迁移与备份

SQLite schema 使用单调 `schema_version` 表。启动只执行已知的向前迁移；未知版本、主键不完整或无法解码的 row 使 Memory Runtime Not Ready。迁移前由部署流程备份原文件；文档不定义自动降级或跳过记录。

备份和恢复必须保留完整复合主键、Version、时间和 metadata。向量索引不在备份中，恢复后由 `Reindex` 重建。

---

*最后更新: 2026-07-22*
