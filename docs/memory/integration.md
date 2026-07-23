# Memory 集成

> 上级: [Memory 系统设计](README.md)
> 相关: [Session](../session/README.md)、[Context](../context/README.md)、[Architecture](../architecture.md)

---

## 1. 依赖关系

```text
Config -> ContentStore -> Memory Manager -> Agent
                                      |
                                      +-> Context Build
                                      +-> Remote API
                                      +-> events/metrics
Session ------------------------------+
```

Memory 依赖 Agent ID 和有效 policy；它不拥有 Session，也不从 Session 自动提取内容。Session 只在 Agent loop 中提供 `SessionID` 和当前用户问题。

## 2. Agent turn 顺序

同一 Session 的完整 turn 已由 Session Manager 的 FIFO gate 串行化。Agent callback 内按以下顺序执行：

1. 读取一次 `ReloadManager.Current()`，从该不可变 Config snapshot 解析本轮 `config.MemoryPolicy`；从 Session `Snapshot()` 取得本轮输入和历史。
2. 把同一个 policy 显式传给本轮全部 Memory 调用。用本轮 user content 组成 `SearchRequest`，scope 为该 Agent + 当前 Session + `LayerLongTerm`，并设置 `IncludeGlobal=true`；因此只读取当前来源和 Agent 全局 Memory，不读其他 Session 来源。
3. 将返回的 `SearchResult.Item` 转为受控的 system message 文本，限制注入数量和字节数；Memory Content 不可直接改变 role、ToolCalls 或系统配置。
4. 把 system prompt、受控 Memory system message、Session 消息和 Tool definitions 组装成完整 `provider.ChatRequest`。
5. 调用唯一的 `context.Manager.Build`；Context 决定预算、压缩和截断，不主动访问 Memory。
6. 执行 Provider/Tool loop；assistant Tool call 与全部 tool result 作为一个 Session 原子单元追加。
7. 将最终 assistant 消息提交到 Session 后再返回远端客户端。
8. 只有业务代码明确识别出应长期保存的事实时，才调用 Memory `Put` 或 `Promote`。

```go
func (a *Agent) RunTurn(ctx context.Context, sessionID string, user provider.Message) error {
    snapshot := a.Config.Current()
    policy, err := a.resolveMemoryPolicy(snapshot)
    if err != nil {
        return err
    }
    results, err := a.Memory.Search(ctx, policy, memory.SearchRequest{
        Scope: memory.Scope{AgentID: a.ID, SessionID: sessionID, Layer: memory.LayerLongTerm},
        Query: user.Content,
        Limit: 0, // Manager 从该 Agent 的 effective vector.top_k 解析默认值
        IncludeGlobal: true,
    })
    if err != nil && !errors.Is(err, memory.ErrMemoryDisabled) {
        return fmt.Errorf("recall memory: %w", err)
    }

    request := buildRequest(a.SystemPrompt, formatMemoryResults(results), user)
    built, err := a.Context.Build(ctx, contextwindow.BuildInput{
        Provider: a.Provider,
        Model:    a.Model,
        Request:  request,
        Config:   a.ContextConfig,
        CurrentTurnStart: len(request.Messages) - 1,
    })
    if err != nil {
        return err
    }
    return a.executeAndCommit(ctx, sessionID, built.Request)
}
```

示例只表达调用顺序；实际 Agent 必须使用 Session 的 `RunTurn` callback，不能绕过 FIFO gate。除 `ErrMemoryDisabled` 外，Memory 读取失败一律阻断当前 turn，不能伪装为空结果；v1 没有通用“继续对话”配置，只有明确的 `fallback_to_keyword` 负责向量到关键词降级。

## 3. Memory 注入格式

Context 文档规定 Memory 通常作为受保护 system message。格式化器必须：

- 只读取 `SearchResult.Item.Content` 和必要的非敏感 metadata；不把 Score 当作 MemoryItem 字段。
- 固定转义换行和控制字符，避免内容伪造 role 或 Tool protocol。
- 按 Search 返回顺序输出，不重新按模型生成结果排序。
- 应用 Agent 的注入字节上限；超过上限时丢弃最末结果并记录计数，不修改 Session。
- 不把 Memory item 写回 Session system message；它只存在于本次 candidate request。

v1 固定 Memory 注入上限为 32 KiB（UTF-8 编码后的完整 system message）；该上限不是 `MemoryConfig` 字段。`Limit=0` 由 Manager 使用 effective `vector.top_k`，格式化器仍必须执行 32 KiB 总字节上限。

## 4. 显式写入与晋升

业务代码自行决定何时写入：

```go
item := memory.MemoryItem{
    AgentID:   agentID,
    SessionID: sessionID,
    Layer:     memory.LayerLongTerm,
    Key:       "preference.answer_style",
    Content:   "用户偏好简洁回答",
    Metadata:  map[string]any{"source": "user"},
}
if _, err := mgr.Put(ctx, policy, item); err != nil {
    return err
}

// 需要将同一事实提升为 Agent 全局记忆时：
_, err := mgr.Promote(ctx, policy,
    memory.Scope{AgentID: agentID, SessionID: sessionID, Layer: memory.LayerLongTerm},
    item.Key,
)
```

Put 是 upsert；调用方不先调用 Add/Update，也不自行拼接 Version。Promote 保留 Session 来源 item，只覆盖或创建空 SessionID 的全局 item。Memory 不在 assistant 回复完成、Session Pause/Close 或 Runtime shutdown 时自动摘要或 Promote。

## 5. Session 和 Storage 边界

- Session 的完整 snapshot 由 Session Manager 持久化到根 `storage.Storage` 的 `session:<id>` key。
- Memory item 由 `memory.storage.type` 选择的 ContentStore 保存；不能把 item 拆成临时层、摘要记录或单独 message key。
- Session Delete/Clear 不触碰 Memory；Memory Clear/Delete 不触碰 Session。
- `persist=false` 只影响 Session；Memory 是否持久化由其自己的 `memory.storage.type` 决定。
- Runtime 恢复 Session 时不读取、不生成 Memory 摘要。Memory SQLite 恢复只校验自身 schema 和 rows。

## 6. 热更新和快照

Agent turn 开始时捕获一次 Config snapshot并解析有效 Memory policy。该 turn 内的 Search、Put、Promote 等调用都显式接收同一个 policy；reload 不改变已经计算出的 ExpiresAt，下一 turn 才使用新 policy。Remote handler 每个请求各捕获一次 snapshot；expiration worker 每个 tick 各捕获一次根 cleanup 配置，cleanup 不属于 Agent turn。

根 `default_ttl`、`max_items` 和 `eviction_policy` 可热更新；`storage.*`、embedding 连接、vector 开关和 dimension 需要重启。Agent override 的可热字段由 [config-ref.md](config-ref.md) 明确列出。

## 7. 故障语义

| 阶段 | 结果 |
|------|------|
| ContentStore 读取/写入失败 | 返回稳定错误；不返回伪造空结果，不写临时内存副本 |
| 向量 query embedding/index 失败，fallback 开启 | 同一次 Search 改走关键词，并发布 degraded |
| 向量 query embedding/index 失败，fallback 关闭 | Search 返回对应错误并阻断当前 turn |
| Content Put 成功、index Upsert 失败 | Put 仍成功；健康 degraded，Reindex 可修复 |
| Session 关闭 | 不触发 Memory 操作 |

## 8. 最小集成测试

1. 只写 Session 后调用 Search，确认没有隐式 Memory item。
2. Put 一个 Session-scoped item 后，当前 Session 能检索，另一个 Session 只能在 Agent 全范围查询时看到它。
3. Context Build 返回的 Request 含 Memory system message，但 Session snapshot 不增加该 message。
4. Promote 后源 item 仍存在，全局 scope 能读取目标 item。
5. ContentStore 停止时 Put 返回错误且没有内存 fallback。
6. index 故障时 Put 成功、事件为 degraded，Reindex 恢复后向量 Search 可用。
7. 在同一 turn 的 Search 与 Put 之间 reload policy，确认两次操作仍使用同一代 policy；下一 turn 使用新 policy。

Runtime 关闭时先停止 Remote 接入并 drain Agent/Session turn，再调用一次共享 `Memory.Manager.Close(ctx)`；Agent 不关闭共享 Memory、ContentStore 或 Embedder。

---

*最后更新: 2026-07-22*
