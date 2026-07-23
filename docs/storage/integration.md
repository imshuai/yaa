# Storage 模块集成

> 上级: [Storage 系统设计](README.md)
> Session 契约: [Session 持久化](../session/persistence.md)
> Memory 契约: [Memory 存储](../memory/storage.md)

---

## 1. 所有权

根 `storage.Storage` 是简单 KV 基础设施。v1 只有 Session 使用它保存持久业务状态；其他模块不能仅因“可能需要缓存”就自行增加 key。

| 模块 | 根 Storage | 说明 |
|------|:----------:|------|
| Session | 是 | 每个 Session 一个完整 snapshot |
| Memory | 否 | 使用专用 ContentStore，支持复合主键和查询 |
| Config | 否 | 当前配置快照在内存，source of truth 是配置文件 |
| Auth | 否 | 静态/JWT 凭据来自当前 Config；v1 不持久化验证缓存 |
| Agent/Provider/Tool/Skill/MCP | 否 | 状态由各自 Manager 或外部进程拥有 |

新增 Storage owner 必须先在对应模块文档中定义 key、value schema、TTL、恢复和损坏语义，不能只在集成代码里出现。

## 2. Key 命名

v1 唯一业务 key：

```text
session:<session-id> -> schema_version=1 的完整 JSON snapshot
```

规则：

- key 使用 ASCII，模块前缀后用冒号分隔；Session ID 格式为 `ses_<ULID>`。
- 不为 Session message、metadata、状态或 policy 建立子 key。
- 不在根 Storage 中建立 Memory item、摘要或向量 key。
- `Keys("session:")` 只用于启动 Restore；返回结果必须按 key 升序，便于确定性校验。

## 3. Session 提交协议

```text
read current immutable snapshot
  -> deep-copy and validate candidate
  -> JSON encode candidate
  -> persist=true: Storage.Set("session:"+id, bytes)
  -> publish candidate to in-memory indexes
  -> emit one Session event
```

Storage 写失败时内存状态和事件保持不变。Close 写入 Closed snapshot，不删除 key；只有 Session Delete 调用 Storage.Delete。Delete 对缺失 key 幂等，但 Session Manager 仍需先确认 Session 存在。

Session snapshot 不传 Storage TTL。`session.ttl`/`max_lifetime` 只驱动 Session 状态机；让 KV 自动删除会绕过 Closed 状态、事件和严格 Restore。

## 4. Restore

Runtime 在对外 Ready 前调用 `SessionManager.Restore(ctx, now)`：

1. `Keys("session:")` 获取并排序全部 key。
2. `Get` 每个完整 snapshot，在临时 map 中严格解码和校验。
3. 根据 snapshot 内冻结的 policy 评估 max lifetime/TTL，并同步写回必要状态修正。
4. 全部成功后一次发布内存索引。

任何 key 后缀不匹配、未知 schema、JSON/Provider message 损坏或状态修正写回失败都使 Runtime Not Ready；不得跳过记录后继续。

## 5. Memory 与根 Storage

Memory ContentStore 和根 Storage 是两个接口：

```text
runtime.storage.* -> storage.Storage -> kv_store -> Session snapshots
memory.storage.*  -> memory.ContentStore -> memory_items -> long-term items
```

两者默认使用不同 SQLite 文件，也可以由部署显式配置到同一个文件；表名和 schema version namespace 必须不同。Memory 的 ContentStore commit、Version、TTL、向量 degraded 和 Reindex 语义完全由 Memory 模块负责，根 Storage 不参与。

## 6. Runtime 顺序

```text
load/validate Config
  -> open root Storage
  -> open Memory ContentStore and optional vector components
  -> create Session Manager with root Storage
  -> restore Memory index and Session snapshots
  -> initialize remaining Managers
  -> Ready
```

失败时按初始化逆序关闭已创建组件。正常 shutdown 先停止 API/Agent 请求，再关闭 Session Manager、Memory Manager、根 Storage。只有 Runtime 调用 Storage.Close；上层 Manager 不拥有其生命周期。

## 7. 备份和恢复

- 根 SQLite 备份必须使用 SQLite online backup 或 checkpoint 后复制，不能在写入中直接复制 WAL 主文件。
- Memory 使用独立文件时与根 Storage 一起取得一致时间点备份；Memory 向量索引无需备份。
- Session snapshot 恢复失败时保留原文件并报告具体 key/error class，不自动删除坏记录。
- `runtime.storage.type=memory` 明确表示 Session snapshot 在进程退出后丢失，只用于测试或临时运行。

## 8. 集成测试

1. Session Create/Append/Close 每次只更新一个 `session:<id>` key。
2. Storage.Set 失败时 Session 内存 snapshot 不变且不发布 mutation event。
3. Restore 遇到一个坏 snapshot 时不发布任何部分 Session。
4. Session TTL 到期只改变 snapshot state，key 仍存在。
5. Memory Put 不生成根 Storage key；Session Delete 不删除 Memory item。
6. Runtime shutdown 只关闭一次每个 Storage owner。

---

*最后更新: 2026-07-22*
