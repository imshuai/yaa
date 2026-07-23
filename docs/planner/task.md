# Planner 执行节点

> 上级: [Planner 系统设计](README.md)

---

## 1. v1 没有 Task 子系统

`Step` 已经是最小执行单元。再把 Step 包装成 `Task`，并增加 Queue、Worker、Scheduler、优先级和第二套状态机会重复 Session FIFO 与 Executor 的职责，因此 v1 不实现这些类型。

```text
Session FIFO gate
  -> 一个临时 Plan
  -> Executor 的内存 DAG
  -> 有界 goroutine 执行 ready Step
  -> PlanResult
  -> 释放全部执行状态
```

执行节点只是 `Executor.Execute` 内部数据，不导出：

```go
type node struct {
    step       Step
    remaining  int
    dependents []string
}
```

`remaining` 是尚未成功的直接依赖数。它只由单个调度 goroutine 修改；Step worker 通过结果 channel 回报完成，不共享修改 `node` 或结果 map。

---

## 2. 并发边界

- 同一 Session 仍由 Session turn FIFO gate 串行化。
- 不同 Session 可以并行执行各自的 Plan。
- 单个 Plan 的并发 Step 数由 `planner.max_concurrent` 限制。
- `max_concurrent` 不是全局后台任务池，也不允许 Step 脱离 turn context 存活。
- Runtime shutdown 等待现有 turn 的统一 shutdown deadline；Planner 不再维护额外 worker 生命周期。

完整 DAG、失败和取消规则见 [执行流程](execution.md)。
