# Session 持久化与恢复

> 上级: [Session 系统设计](README.md)
> 底层接口: [Storage 系统设计](../storage/README.md)

---

## 1. 单一存储抽象

Session 直接复用根 `storage.Storage`，不再定义 `SessionStore`、Session 专用 SQLite 表、异步写队列或第二套后端配置。

| 项目 | 契约 |
|------|------|
| Key | `session:<session-id>`，例如 `session:ses_01J...` |
| 枚举 | `store.Keys("session:")` |
| 写入 | `store.Set(key, snapshot)`，不传 Storage TTL |
| 删除 | `store.Delete(key)` |
| 内容 | 带 `schema_version` 的完整 JSON snapshot |

Session 的 TTL 是生命周期 policy，只改变 Session 状态；不能使用 Storage TTL 自动删除 snapshot。

## 2. Snapshot 格式

编码只使用以下私有 DTO，不能直接 `json.Marshal(Session)`；这样字段名和 duration 格式不会受内存模型改变影响。`message` 内的 Tool name 必须已经是 canonical；Provider-safe alias 及 turn-local map 严禁写入 snapshot：

```go
const maxSessionSnapshotBytes = storage.MaxValueBytes

type snapshotV1 struct {
    SchemaVersion  int                 `json:"schema_version"`
    ID             string              `json:"id"`
    AgentID        string              `json:"agent_id"`
    State          State               `json:"state"`
    CreatedAt      time.Time           `json:"created_at"`
    UpdatedAt      time.Time           `json:"updated_at"`
    LastActivityAt time.Time           `json:"last_activity_at"`
    Policy         policySnapshotV1    `json:"policy"`
    Messages       []messageSnapshotV1 `json:"messages"`
    UsedTurnIDs    []string            `json:"used_turn_ids"`
    Metadata       map[string]any      `json:"metadata"`
}

type policySnapshotV1 struct {
    MaxMessages     int    `json:"max_messages"`
    MaxMessageBytes int    `json:"max_message_bytes"`
    TTL             string `json:"ttl"`
    MaxLifetime     string `json:"max_lifetime"`
    Persist         bool   `json:"persist"`
}

type messageSnapshotV1 struct {
    ID        string            `json:"id"`
    TurnID    string            `json:"turn_id"`
    Message   provider.Message  `json:"message"`
    CreatedAt time.Time         `json:"created_at"`
    Metadata  map[string]any    `json:"metadata"`
}
```

```json
{
  "schema_version": 1,
  "id": "ses_01J...",
  "agent_id": "default",
  "state": "active",
  "created_at": "2026-07-22T01:00:00Z",
  "updated_at": "2026-07-22T01:01:00Z",
  "last_activity_at": "2026-07-22T01:01:00Z",
  "policy": {
    "max_messages": 1000,
    "max_message_bytes": 10485760,
    "ttl": "24h0m0s",
    "max_lifetime": "720h0m0s",
    "persist": true
  },
  "messages": [
    {
      "id": "msg_01J...",
      "turn_id": "turn_01J...",
      "message": {
        "role": "user",
        "content": "hello"
      },
      "created_at": "2026-07-22T01:01:00Z",
      "metadata": {}
    }
  ],
  "used_turn_ids": ["turn_01J..."],
  "metadata": {}
}
```

Snapshot 必须包含解析后的 policy，不能只保存 override 或在恢复时重新读取当前配置。编码时用 `Duration.String()` 生成 duration；解码时用 `time.ParseDuration` 严格解析。时间先规范化为 UTC，再由 `time.Time` 编码为 RFC 3339 Nano。`SessionMessage.TurnID/Payload` 显式映射到 DTO 的 `turn_id/message` 字段。`used_turn_ids` 是已提交 user 的精确 tombstone 集，编码前按字节升序排序；消息删除和 Clear 不移除它，只有物理删除 Session 才释放。

解码要求：

- 拒绝未知 `schema_version`、非法枚举、空 ID、Agent 不匹配和未知 JSON 字段；
- 校验 key 后缀与 snapshot `id` 相同；
- 校验所有 Message ID 唯一、每条 Turn ID 合法、消息序列和 Tool unit 完整；
- `used_turn_ids` 必须有序、无重复且包含每条 persisted user 的 Turn ID；每个非 user 消息的 Turn ID 必须对应此前或同批 user；
- 校验 resolved policy，但不将当前配置默认值重新覆盖进去；Tool name 只校验 canonical 格式、消息序列和 unit 关联，不要求当前 Tool Manager 仍注册该名称；
- `metadata: null` 和 `messages: null` 规范化为空 map/slice 后再发布。
- JSON 编码结果不得超过 `maxSessionSnapshotBytes`；解码 persisted byte slice 前也先检查 `len(raw)`，拒绝历史或外部写入的超限 value。

## 3. 同步提交协议

所有变更使用 copy-on-write 候选快照：

```text
读取当前只读快照
  → 深拷贝并校验候选状态
  → persist=true: JSON encode + 16 MiB check + Storage.Set
  → 原子替换内存快照/索引
  → 发布一次事件
```

编码失败返回 `ErrPersistenceFailed`。编码结果超过 16 MiB 时返回同时包装 `ErrPersistenceFailed` 与 `ErrSessionSnapshotTooLarge` 的错误，且不调用 Storage。Storage 的共同 wrapper 也必须以 `storage.ErrValueTooLarge` 防御绕过。

`storage.Storage` 不接受 context，提交点语义固定为：进入 FIFO task 和调用 `Storage.Set`/`Storage.Delete` 前各检查一次 `ctx.Err()`；一旦开始 Storage mutation 就不可中断。`Set` 成功后必须发布相同候选内存 snapshot；`Delete` 成功后必须完成索引和 runner 删除，即使 context 此时刚被取消。两者都返回成功，使持久状态与内存状态不会分叉。Storage mutation 返回失败时内存、时间字段、计数和事件均保持不变。

禁止“先改内存、后台重试”或静默降级为纯内存，这会造成重启后的历史回退。

`persist=false` 时跳过 Storage 步骤，但仍使用同一候选快照和原子内存替换流程。

## 4. Create、Update 和 Delete

| 操作 | Storage 顺序 | 内存顺序 |
|------|--------------|----------|
| Create | `Set` 成功 | 加入 `sessions` 和 Agent 索引 |
| Append / 状态 / metadata | `Set` 候选 snapshot 成功 | 替换当前 snapshot |
| Close | `Set` Closed snapshot 成功 | 替换状态并释放 active capacity |
| Delete | `Delete` 成功 | 移除 snapshot、索引和 runner |

对 `persist=false` 的 Delete 不调用 Storage。`Storage.Delete` 对不存在 key 应幂等，但 Manager 仍需先确认内存 Session 存在，否则返回 `ErrSessionNotFound`。

## 5. Restore

`Restore(ctx, now)` 使用 `Keys("session:")` 读取所有 snapshot。为避免部分可见状态：

1. 在临时 map 中完整加载和验证所有记录。
2. 按 [生命周期](lifecycle.md#5-启动恢复) 重新评估 TTL 和 max lifetime。
3. 同步写回需要状态修正的记录。
4. 从每条记录的 `used_turn_ids` 重建判重索引；全部成功后一次替换 Manager 的空索引。

任一损坏记录、Turn ID 冲突、未知 schema 或必要写回失败都返回 `ErrRestoreFailed`，Runtime 保持 Not Ready；不得跳过损坏记录后宣称恢复成功。日志可以记录失败 key，但不能记录消息内容或 Turn ID。

v1 只读取 `schema_version=1`。未来迁移必须先生成可恢复备份，再以独立、可重试步骤升级；不能在普通读取路径中隐式迁移。

## 6. 崩溃语义

- `Storage.Set` 返回成功而进程在内存发布前崩溃：重启后恢复新 snapshot，允许。
- `Storage.Set` 返回失败：旧 snapshot 仍是权威状态。
- 事件发布失败不回滚已提交 snapshot；记录 `session.error` 指标并继续，因为事件总线不是数据源。
- Storage 关闭由 Runtime 统一负责，Session Manager 不调用 `Close()`。

---

*最后更新: 2026-07-23*
