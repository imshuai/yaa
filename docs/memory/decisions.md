# Memory 设计决策

> 上级: [Memory 系统设计](README.md)

---

## MM-001：v1 只有 Agent-scoped long-term

Session 保存完整消息和短期状态；Memory 不复制历史、不维护独立摘要层。`SessionID` 仅作为 item 来源和过滤 scope。

## MM-002：Put 是唯一 upsert 写契约

公开 `Put(ctx,policy,item)` 按完整复合主键 upsert，避免创建/更新两套语义。内部唯一 commit 是 `ContentStore.CommitPut(item,victims,now)`，target 与容量 victims 同成同败。Promote 是显式的 scope 复制操作，最终仍使用同一提交规则。

## MM-003：ContentStore 是真实来源

SQLite/Memory ContentStore 先提交 item；embedding 和 vector index 可重建。索引失败不回滚已提交内容，健康状态标记 degraded。

## MM-004：v1 向量索引是进程内 exact cosine

向量默认关闭。启用时使用纯 Go slice 和 exact cosine，不增加本地扩展或独立索引服务。默认 `max_items=10000` 是这个简单实现的明确上限；实测延迟或内存不满足后再引入新版本后端。

## MM-005：纯 Go SQLite

本地内容存储使用 `modernc.org/sqlite`，零 CGO，保持目标平台可交叉编译。Memory SQLite 文件独立于根 KV Storage，因为查询和版本契约不同。

## MM-006：TTL 与驱逐由 Manager 控制

Default TTL 只在 Put 时解析；已有 ExpiresAt 不因配置 reload 改变。过期使用有限 batch 删除，驱逐只支持 FIFO 或最早 TTL，不依赖未持久化访问时间。Manager 在 Agent lock 内计算 victims，ContentStore 在同一事务/写锁内完成 victims 删除与 target upsert，提交后才发布事件。

## MM-007：严格 scope 与严格配置

Get/Delete 必须提供完整 Scope；空 SessionID 在 Search/Clear 表示 Agent 全范围，Manager 的 Reindex 通过 `List` 使用同一全范围语义但只接收 `agentID`。未知配置字段、未知 Layer 和 Agent 越权启用一律拒绝。

## MM-008：事件不泄露内容

事件只包含 scope、key、Version、时间和有限 reason；日志/指标不记录 Content、metadata value、embedding、query 或凭据，高基数 ID 不进入 metric label。

## MM-009：确定性优先

关键词结果、向量并列、TTL batch 和 eviction victim 都有稳定 tie-break。相同内容与相同时钟输入必须产生相同顺序，便于测试和重放。

## MM-010：没有隐式内容 fallback

ContentStore 失败向上传递，不将写入转存临时 map 后返回成功。只有向量检索可按显式 `fallback_to_keyword` 降级；Content commit 后的索引失败由 degraded + Reindex 修复。

---

*最后更新: 2026-07-22*
