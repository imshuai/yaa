# Memory 实现检查清单

> 依据 [Memory 系统设计](README.md) 和 [配置参考](config-ref.md)。

---

## 模型与 API

- [ ] `MemoryItem` 包含 AgentID、可选 SessionID、Layer、Key、Content、Metadata、时间、ExpiresAt、Version。
- [ ] v1 只接受 `LayerLongTerm`，主键为 `(agent, layer, session, key)`。
- [ ] 调用方 managed fields 必须为零；Manager 单次采样 now，Store 在事务内设置时间/Version。
- [ ] `Put` 是唯一 upsert；Get/Delete 使用完整 Scope；返回值均深拷贝。
- [ ] Search/Clear 的空 SessionID 仅表示 Agent 全范围；Limit 和排序确定。
- [ ] Promote 复制到全局 scope、保留源、重新应用 default TTL；目标只发 promoted，committed victims 仍发 evicted。
- [ ] `Manager.Close(ctx)` 拒绝新操作、停止 worker、等待 in-flight、只关闭 Store 一次，超时后后台关闭可继续。
- [ ] `beginOp` 在 `lifecycleMu` 下完成 closing 检查与 `inFlight.Add(1)`；race test 覆盖并发操作和 Close。

## 内容存储与索引

- [ ] ContentStore 完整实现 CommitPut/Get/Search/List/Delete/Clear/DeleteExpired/Count/Ping/Close。
- [ ] CommitPut 在一个 SQLite 事务/Memory 写锁中提交 target + victims，失败不修改任何 item/event/index。
- [ ] SQLite 使用 pure Go driver，DDL、upsert、事务、时间与 metadata 编码有集成测试。
- [ ] Memory 后端遵守相同主键、排序、过期和深拷贝语义。
- [ ] VectorIndex 使用完整 ItemRef + Version 和 typed Session/global selector；scope 在 threshold/排序前过滤，命中必须回查 ContentStore。
- [ ] VectorIndex 全部方法并发安全；factory 每次返回新的非 nil 空索引并由 Manager 持有。
- [ ] Content 先提交，index 失败不回滚；Delete/Clear/Expire 后 index 失败不复活内容。
- [ ] Reindex 只接收 AgentID，构建全 Agent 临时索引并原子替换，不丢失并发 Put/Delete。
- [ ] vector disabled/初始化/失败/完整 Reindex 成功的状态依次遵循 ready/degraded/degraded/ready，API 全程使用 `IndexStatus`。

## TTL 与容量

- [ ] nil ExpiresAt 使用 default TTL；zero time pointer 永久；过去时间拒绝。
- [ ] Get/Search/List/Count 隐藏已过期 item；cleanup 有稳定顺序、batch 和取消。
- [ ] `max_items` 按 Agent 统计；fifo/ttl victim 和 tie-break 与文档一致。
- [ ] 最终 count 计算覆盖新建、过期 row 恢复和未过期更新；target 排除在 victims 外，热缩后下一次 Put 原子收敛。

## 集成与失败

- [ ] Agent 在 Context Build 前检索；Context 不主动访问 Memory。
- [ ] Memory system message 只存在于请求副本，不写入 Session snapshot。
- [ ] Session Close/Restore/Delete 不隐式摘要、Promote 或删除 Memory。
- [ ] ContentStore 错误不吞掉、不写临时 fallback；向量 fallback 只由配置控制。
- [ ] v1 除 Disabled 和 vector keyword fallback 外没有通用继续策略；其他 Memory 错误阻断 turn。
- [ ] index degraded、Runtime Ready/Not Ready 和 Remote API 状态码符合 errors.md。
- [ ] 锁顺序固定为 mutationGate → Agent keyed lock → index state；Promote 不重入 Agent lock。

## 配置与可观测性

- [ ] 根内容后端唯一 owner 为 `memory.storage.type`，只接受 `sqlite|memory`。
- [ ] Agent 类型为 `*MemoryOverride`，所有 override scalar/vector 字段为 pointer。
- [ ] Agent 不能覆盖 storage/embedding/cleanup，也不能在 root disabled 上启用。
- [ ] hot/restart 字段与 config-ref/hot-reload 完全一致；strict decoder 拒绝未知字段。
- [ ] Agent turn 显式复用同一 policy；Remote 每请求、cleanup 每 tick各捕获一个 Config snapshot。
- [ ] Remote 仅 request deadline 映射 504；client cancel 不写响应，quota/closed 分别映射 429/503。
- [ ] 事件只使用 canonical 8 种名称；指标统一 `yaa_memory_*` 且无高基数/敏感 label。
- [ ] 健康只反映 content/embedder/index，不根据 Session 状态判断。

## 文档门禁

- [ ] Memory 全部文档不存在旧层级、旧写 API、未定义配置或重复 DTO。
- [ ] fenced JSON/YAML 可解析，相对链接存在，Markdown fence 配对。
- [ ] `git diff --check` 通过。

---

*最后更新: 2026-07-22*
