# Session 生命周期

> 上级: [Session 系统设计](README.md)
> 配置: [Session 配置参考](config-ref.md)

---

## 1. 状态机

`closed` 是终态。手动操作、清理任务和启动恢复都必须使用同一套转换函数，不能直接改字段。

| 当前状态 | 目标状态 | 触发 | 条件 |
|----------|----------|------|------|
| 不存在 | `created` | `Create` | Agent 存在、policy 合法、容量未满 |
| `created` | `active` | 首个 turn user | `AppendUser` 与 Turn ID 同批提交 |
| `created` | `paused` | TTL / restore | 空闲时间达到 TTL |
| `created` | `closed` | Close / max lifetime / restore | — |
| `active` | `paused` | Pause / TTL / restore | — |
| `active` | `closed` | Close / max lifetime / restore | — |
| `paused` | `active` | Resume | 尚未达到 max lifetime |
| `paused` | `closed` | Close / max lifetime / restore | — |
| `closed` | `closed` | Close | 幂等，无副作用 |

不在表中的转换返回 `ErrInvalidStateTransition`。`Resume` 不会恢复 Closed Session；若当前时间已经达到 max lifetime，则返回 `ErrSessionExpired` 且不改变 Session。只有 cleanup 和启动 Restore 负责把该 Session 提交为 Closed。

```text
Create
  │
  ▼
created ── first user ─────► active ◄── Resume ── paused
   │                           │                    ▲
   ├── TTL ────────────────────┼────────────────────┘
   │                           └── Pause / TTL ─────┘
   └──────────── Close / max_lifetime ─────────────┐
                                                    ▼
                                                  closed
```

## 2. 时间字段

| 字段 | 更新规则 | 用途 |
|------|----------|------|
| `CreatedAt` | Create 时设置一次 | `max_lifetime` 基准 |
| `UpdatedAt` | 每次成功提交消息、状态或 metadata 变更 | 乐观观察和排序 |
| `LastActivityAt` | Create、成功追加消息、Resume 时更新 | `ttl` 空闲基准 |

Pause、Close 和 metadata 更新不刷新 `LastActivityAt`。所有比较使用 UTC，调用方在一次检查中传入同一个 `now`，避免边界漂移。

## 3. 生命周期操作

状态许可矩阵：

| 操作 | `created` | `active` | `paused` | `closed` |
|------|:---------:|:--------:|:--------:|:--------:|
| RunTurn | ✅ | ✅ | `ErrSessionPaused` | `ErrSessionClosed` |
| Pause | 非法转换 | ✅ | 非法转换 | `ErrSessionClosed` |
| Resume | 非法转换 | 非法转换 | ✅ | `ErrSessionClosed` |
| DeleteMessage / ClearMessages | ✅ | ✅ | ✅ | `ErrSessionClosed` |
| Close | ✅ | ✅ | ✅ | 幂等成功 |
| Delete | ✅ | ✅ | ✅ | ✅ |

除表中幂等项外，状态错误不写 snapshot、不更新时间、不发布事件。

### 3.1 Create

Create 的提交顺序固定为：

1. 校验 Agent 和 `max_sessions_per_agent`；只统计该 Agent 的非 Closed Session。
2. 解析根配置、Agent override、Create override 并校验 `SessionPolicy`。
3. 生成 `ses_<ULID>`，三个时间字段设为同一个 `now`，状态设为 `created`。
4. `persist=true` 时同步写入 snapshot。
5. 注册内存索引并发布 `session.created`。

任一步失败都不得留下可查询的半成品。

### 3.2 Pause 和 Resume

- Pause 只允许 `active -> paused`；重复 Pause 返回 `ErrInvalidStateTransition`。
- Resume 只允许 `paused -> active`，先检查 `max_lifetime`。
- Resume 检查到 max lifetime 已到期时返回 `ErrSessionExpired`，不更新时间、不写 snapshot、不发事件。
- Resume 成功时同时更新 `UpdatedAt` 与 `LastActivityAt`，防止下一次 cleanup 立即再次暂停。
- 状态快照提交后发布一次 `session.state.changed`，payload 必须包含 `from`、`to`、`reason` 和 `occurred_at`。

### 3.3 Close 和 Delete

- Close 将任意非 Closed Session 转为 Closed，保留消息和 snapshot 供查询。
- 对 Closed 调用 Close 返回 nil，不写 Storage、不更新时间、不发事件。
- Delete 是物理删除：先删除持久 snapshot，再移除内存索引，最后发布 `session.deleted`。
- Delete 不等同 Close；Remote API 分别暴露两个动作。

## 4. TTL 与最大生命周期

`cleanup_interval` 只决定检查频率，不参与过期时间计算。每次检查按以下顺序处理每个非 Closed Session：

```go
func desiredState(s *Session, now time.Time) State {
    if s.Policy.MaxLifetime > 0 && !now.Before(s.CreatedAt.Add(s.Policy.MaxLifetime)) {
        return StateClosed
    }
    if s.Policy.TTL > 0 &&
        (s.State == StateCreated || s.State == StateActive) &&
        !now.Before(s.LastActivityAt.Add(s.Policy.TTL)) {
        return StatePaused
    }
    return s.State
}
```

边界使用 `now >= deadline` 即到期。`max_lifetime` 优先于 TTL，因此同一次检查只提交最终的 Closed 状态。Paused Session 不因 TTL 再次变化，但仍受 max lifetime 限制。

清理循环必须：

- 使用 Manager 当前配置中的 `cleanup_interval`；
- 逐个通过 Session FIFO gate 提交，避免与 turn 交叉；
- 单个 Session 失败时记录 `session.error` 并继续检查其他 Session；
- Runtime 关闭时响应 `ctx.Done()`。

## 5. 启动恢复

恢复顺序必须可重放且无事件重放：

1. 枚举并排序 `session:` keys。
2. 严格解码、校验 schema、ID、状态、消息序列、Turn ID 判重集和 resolved policy。
3. 以同一个启动时间 `now` 先计算 max lifetime，再计算 TTL。
4. 状态发生变化时同步覆写 snapshot；写入失败则恢复失败。
5. 所有记录验证和必要覆写成功后，原子发布内存索引。
6. 不发布 `session.created` 或 `session.state.changed`；只记录恢复统计日志。

`persist=false` 的 Session 没有 snapshot，因此不会恢复。恢复完成前 Remote API 不得进入 Ready。

---

*最后更新: 2026-07-22*
