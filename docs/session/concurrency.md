# Session 并发模型

> 上级: [Session 系统设计](README.md)
> 总体原则: 同一 Session 串行，不同 Session 并行

---

## 1. 并发边界

同一 Session 的一个任务必须覆盖完整 Agent turn：接受 user、构建 Context、调用 Provider、执行全部 Tool、追加 Tool unit、再次调用 Provider、提交 final assistant。Pause、Resume、Close、Delete、消息删除和 cleanup 转换也进入同一个 gate，不能插入 turn 中间。`RunTurn` callback 获得只在该 task 中有效的 `*Turn`；其 `Snapshot` 和 `Append` 不会二次排队。

```text
Session A queue: turn-1 → pause → turn-2
Session B queue: turn-9 ──────────────────►
                  串行       不同 Session 并行
```

排队顺序以 Manager 接受任务的顺序为准。队列满时提交方等待；`ctx.Done()` 可以取消尚未开始的任务。v1 不返回 `ErrSessionBusy`、`ErrLockTimeout` 或自定义排队超时。

## 2. Manager 结构

```go
type Manager struct {
    mu          sync.RWMutex
    sessions    map[string]*Session
    agentIdx    map[string]map[string]struct{}
    runners     map[string]*runner
    activeTurns map[turnKey]*turnControl
    usedTurnIDs map[string]map[string]struct{} // session ID -> committed IDs
    store       storage.Storage
}

type runner struct {
    tasks chan task
    done  chan struct{}
}

type task struct {
    key    turnKey // zero value for lifecycle/cleanup tasks
    ctx    context.Context
    run    func(context.Context) error
    out    chan error
}

type turnControl struct {
    agentID string
    ctx     context.Context
    cancel  context.CancelCauseFunc
    done    chan struct{}
}
```

这不是公开扩展接口。固定容量只用于背压，不形成配置项；容量选择由实现测试确定。Runner 每次只执行一个 task，task 内可以进行耗时 Provider 调用，因此不同 Session 必须拥有不同 runner。每个 runner 用一个小 mutex/counter 记录 running + queued 数量，从而在接受时生成 position；`onQueued(position)` 在所有 Manager/runner 锁外调用，position 只是接受时前方任务数，不随后更新。

## 3. 锁与快照规则

| 资源 | 保护方式 | 禁止事项 |
|------|----------|----------|
| `sessions`、`agentIdx`、`runners` | `Manager.mu` | 持锁调用 Storage、Provider、Tool 或 Event Bus |
| 单 Session 写顺序 | runner FIFO | 绕过 runner 直接修改 Session |
| Session 读取 | Manager 下的不可变 snapshot | 向调用方返回内部 slice/map |
| Event subscribers | Event Bus 自身锁 | 在 Session 锁内阻塞发布 |

写 task 从当前 snapshot 深拷贝候选值；Storage 成功后只在短暂的 `Manager.mu` 临界区替换指针。`Get` 因而只能看到旧的完整 snapshot 或新的完整 snapshot，不能看到部分 Tool unit。

锁顺序固定为：先取得 runner 执行权，再短暂取得 `Manager.mu`。任何路径不得在持有 `Manager.mu` 时等待 runner，以免 Delete、cleanup 和查询形成死锁。

`Turn` handle 记录 runner 身份、Turn ID 和派生 `turnCtx`，仅供 callback 同步调用。callback 退出后调用其方法必须返回内部错误；实现不得通过 context value 判断“是否已持锁”，也不得允许 handle 跨 goroutine 并发使用。

## 4. Turn 取消与失败

- `RunTurn` 接受时先在 `activeTurns` 预留 key，并用 `context.WithCancelCause(ctx)` 保存真实 `CancelCauseFunc`；与 queued/running 或 `usedTurnIDs` 冲突时不入队。
- 排队阶段取消：runner 跳过 task；若 user 尚未提交则释放预留且不消费 Turn ID，返回 `context.Cause(turnCtx)`。
- Provider/Tool 阶段取消：向下游传播同一个 context；已提交 snapshot 保留，未提交 batch 丢弃。
- `CancelTurn` 查找精确 `(sessionID, turnID)` 并调用保存的 cancel；`CancelAgentTurns(ctx, agentID, cause)` 收集该 Agent 的全部 handle 后逐一取消，并等待它们从 registry 移除或 ctx 到期。它是管理型收拢操作，Manager 进入 `closing`/`closed` 后仍可调用；快照为空时幂等返回 `nil`，有 handle 时等待 context 到期则返回 `context.Cause(ctx)`。二者都不得在 `Manager.mu` 内执行 cancel 或等待。
- `RunTurn` 在预留成功后立即安装唯一 defer；无论 enqueue 失败、queued cancel、Session Delete、panic、callback error 或成功，该 defer 都从 `activeTurns` 删除并 `close(done)`。`CancelAgentTurns` 只等待这些 done channel，不轮询 map。
- callback 或下游观察到的是 `turnCtx.Err()`；`RunTurn` 对调用方返回非 nil `context.Cause(turnCtx)`，业务层不得把所有取消压扁为 `context.Canceled`。
- `AppendUser` 已提交的 ID 已在同一 snapshot 和 `usedTurnIDs` 中，不能因失败或取消释放；未提交 ID 只由上述 defer 释放预留。
- Storage 失败：当前 task 返回 `ErrPersistenceFailed`，runner 继续处理下一个任务。
- panic：runner 捕获、记录 `session.error`，使当前 task 失败并继续服务；不得让单个 Session 终止整个 Runtime。

Tool calls 可以在当前 turn 内并发执行，但必须等待全部完成并按原始 call 顺序组成原子 Tool unit。并行 Tool 不允许启动同一 Session 的第二个 turn。

## 5. 生命周期与容量竞争

Create 在 Manager 全局临界区预留该 Agent 的容量和新 ID，随后持久化；失败时释放预留。这样两个并发 Create 不会同时越过 `max_sessions_per_agent`。

Close 提交后立即不再计入容量。Delete 必须在自己的 runner task 中完成，然后从索引摘除并停止 runner；已排队但尚未执行的任务统一返回 `ErrSessionNotFound`。Cleanup 与手动状态操作共用同一 task 路径。

## 6. Runtime 关闭

Runtime 只调用一次 `Manager.Shutdown(ctx)`。该方法幂等，拥有 cleanup context、所有 Session runner 和它们的 WaitGroup：

关闭顺序：

1. Runtime 停止 Remote API 接入并调用 `Agent.Manager.Quiesce()`；此后没有新 turn 入队，已登记 turn 保持运行。
2. Manager 原子标记 closing，并立即取消 cleanup context、停止 cleanup timer；此后不得再生成 cleanup task，新提交返回 `ErrManagerClosed`。`CancelAgentTurns` 不属于新提交，仍可用于收拢 registry；没有活动 turn 时返回 `nil`。
3. 已经进入 runner 队列的 cleanup 与普通 task 一样 drain；等待已开始和已排队 task 完成，直到 shutdown context 到期。
4. 到期时收集所有 `turnControl.cancel`，以 shutdown deadline cause 逐一调用，再等待 runner 退出。Provider、Tool 和 callback 必须遵守派生 context；Go 无法强制终止忽略 context 的 goroutine，这种实现属于接口违约并记录 fatal shutdown 日志。
5. 因每次变更已同步持久化，不执行额外 `persistAll`。
6. 所有 cleanup/runner goroutine 退出后 `Shutdown` 才返回；若 shutdown context 曾结束，返回捕获的 `context.Cause(ctx)`。
7. Storage 最后由 Runtime 关闭。

禁止直接关闭仍可能被发送的 task channel；实现可用 closing flag + WaitGroup 避免 send-on-closed-channel。

## 7. 最小并发验证

实现至少覆盖：

- 同一 Session 100 个并发提交按接受顺序完成；
- 不同 Session 的阻塞 Provider 调用可重叠；
- 排队 context 取消不会执行 task；
- queued 取消前未提交 user 时 Turn ID 可重用；提交 user 后取消、Clear、DeleteMessage 和 Restore 后均拒绝重复 ID；
- CancelTurn、Agent Stop 和 shutdown deadline 都能取消保存的 handle；
- Tool unit 对并发读者始终全有或全无；
- Close/Delete/cleanup 不会插入正在运行的 turn；
- `go test -race ./...` 无数据竞争。

---

*最后更新: 2026-07-22*
