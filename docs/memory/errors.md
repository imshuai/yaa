# Memory 错误与降级

> 上级: [Memory 系统设计](README.md)
> 可观测性: [observability.md](observability.md)

---

## 1. 稳定错误

Memory 包导出以下可用 `errors.Is` 判断的 sentinel。底层错误必须用 `%w` 包装，不能把它们转换成空 slice 或 nil。

```go
var (
    ErrMemoryDisabled          = errors.New("memory: disabled")
    ErrMemoryClosed            = errors.New("memory: closed")
    ErrMemoryNotFound          = errors.New("memory: item not found")
    ErrMemoryInvalidScope      = errors.New("memory: invalid scope")
    ErrMemoryInvalidItem       = errors.New("memory: invalid item")
    ErrMemoryManagedField      = errors.New("memory: managed field is not writable")
    ErrMemoryUnsupportedLayer  = errors.New("memory: unsupported layer")
    ErrMemoryExpiredInput      = errors.New("memory: expiration is in the past")
    ErrMemoryQuota             = errors.New("memory: capacity could not be satisfied")
    ErrMemoryStoreUnavailable  = errors.New("memory: content store unavailable")
    ErrMemoryCorrupt           = errors.New("memory: corrupt content")
    ErrMemoryEmbeddingFailed   = errors.New("memory: embedding failed")
    ErrMemoryEmbeddingDimension = errors.New("memory: embedding dimension mismatch")
    ErrMemoryEmbeddingZero     = errors.New("memory: zero vector")
    ErrMemoryIndexUnavailable  = errors.New("memory: vector index unavailable")
    ErrMemoryIndexDegraded     = errors.New("memory: vector index degraded")
    ErrMemoryReindexFailed     = errors.New("memory: reindex failed")
}
```

参数错误（scope、layer、长度、metadata、limit）在 ContentStore 写入前返回。`ErrMemoryNotFound` 同时表示不存在和已过期；调用方不应通过错误内容区分两者。

## 2. 操作语义

| 操作 | ContentStore 失败 | embedding/index 失败 |
|------|-------------------|----------------------|
| Put/容量驱逐 | CommitPut 整体回滚 target 与 victims | 内容已提交，Put 成功，状态 degraded |
| Get | 返回错误 | 不访问 index |
| 关键词 Search | 返回错误 | 不适用 |
| 向量 Search，fallback=true | 不适用 | 同一次请求改走关键词，返回成功并发布 degraded |
| 向量 Search，fallback=false | 不适用 | 返回 `ErrMemoryEmbeddingFailed`、`ErrMemoryIndexUnavailable` 或 `ErrMemoryIndexDegraded` |
| Delete/Clear/Expire/Evict | 返回错误，删除事务回滚 | 内容删除成功，索引状态 degraded |
| Reindex | 保留旧索引，返回错误 | 保留旧索引，返回错误 |

“Put 成功但 index degraded”是有意的 partial success：Manager 返回 `PutResult{IndexStatus: IndexDegraded}` 和 nil error。ContentStore 是唯一真实来源，调用方不应因为 degraded 自动重复 Put。事件和健康报告提供修复信号，`Reindex` 是唯一修复入口。vector Search 在 `fallback_to_keyword=false` 且状态已 degraded 时返回 `ErrMemoryIndexDegraded`。

## 3. Agent 层传播

Agent 不得捕获所有错误后返回 nil。推荐规则：

```go
results, err := mgr.Search(ctx, policy, req)
switch {
case err == nil:
    // 注入 results
case errors.Is(err, ErrMemoryDisabled):
    // 明确关闭时不注入
default:
    // v1 除 Disabled 外没有继续策略；所有错误向上传递
    return fmt.Errorf("recall memory: %w", err)
}
```

Agent 对除 `ErrMemoryDisabled` 外的 Memory 错误一律阻断当前 turn，因为静默跳过会让用户误以为记忆已保存或已检索。v1 配置没有通用“继续对话”字段；唯一降级是已定义的 `vector.fallback_to_keyword`。

## 4. 不允许的降级

- ContentStore 故障时不得写入只存在于进程内的临时副本。
- 不得把坏 row 跳过后报告 Search 成功；返回 `ErrMemoryCorrupt` 并将 Runtime 标为 Not Ready/Unhealthy。
- 不得无限重试 Put、embedding 或 Reindex。Manager 不内置重试循环；调用方如需重试必须受 ctx deadline 和业务次数限制。
- 不得因为 quota 失败自动删除任意“低优先级” item；victim 只能按 `fifo|ttl` 的确定性排序选择。
- 不得把 vector threshold 应用于关键词结果，也不得在 fallback 时扩大结果数量。

## 5. 错误包装和取消

```go
item, err := store.Get(ctx, scope, key)
if err != nil {
    if errors.Is(err, ErrMemoryNotFound) {
        return MemoryItem{}, ErrMemoryNotFound
    }
    return MemoryItem{}, fmt.Errorf("memory get: %w: %w", ErrMemoryStoreUnavailable, err)
}
```

真实底层错误应作为 wrapped cause 保留给日志和 tracing，但对外稳定分类使用上面的 sentinel。Reindex 失败必须包装 `ErrMemoryReindexFailed`，并继续包装 embedding/index/store 原因；`context.Canceled` 和 `context.DeadlineExceeded` 保留原错误链，不能改成“not found”。

## 6. 启动和健康

- ContentStore 打开、迁移或 row 校验失败：Memory Runtime Not Ready。
- vector enabled 且 `fallback_to_keyword=false` 时，初始 Reindex 失败：Runtime Not Ready。
- vector enabled 且 `fallback_to_keyword=true` 时，初始 Reindex 失败：Runtime 可 Ready，但为 degraded，向量 Search 会按配置 fallback。
- index 单次 Upsert/Delete 失败：不影响已提交内容，健康为 degraded，发布 `memory.degraded`。

## 7. 对 Remote API 的映射

API 层统一使用 envelope；建议状态码：

| 错误 | HTTP |
|------|------|
| invalid scope/item/limit | 400 / `40001` |
| not found | 404 / `40401` |
| disabled | 409 / `40901` |
| quota | 429 / `42901` |
| closed | 503 / `50301` |
| store unavailable/corrupt | 503 / `50301` |
| vector unavailable/degraded 且未启用 fallback | 503 / `50301` |
| Reindex failure（非请求取消/超时） | 503 / `50301` |
| request deadline exceeded | 504 / `50401` |
| 已提交但 index degraded | 2xx，并在 data/index_status 中标记 `degraded` |

Handler 必须先检查自己的 request context：`context.Canceled` 表示客户端已断开，通常不再写响应；只有 request context 的 `DeadlineExceeded` 映射 `50401`。内部 embedding/store/index 子操作的 timeout 在 request context 尚未到期时映射 `50301`。API 不暴露数据库路径、embedding 响应、完整 ContentStore 错误文本或凭据。

---

*最后更新: 2026-07-22*
