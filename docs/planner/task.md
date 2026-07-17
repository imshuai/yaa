# Task 调度系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/planner/task.md`
> 上级: `docs/planner/README.md`
> 依赖: `docs/architecture.md` §3.5, §5 并发模型
> 代码路径: `internal/task/` (`task.go`, `scheduler.go`, `queue.go`)

---

## 1. 概述

Task 调度系统是 Planner 层的执行引擎。当 Planner 将复杂任务分解为有序步骤（Step）后，Task 调度系统负责**接收、排队、调度、并发执行**这些任务单元，并维护其完整的状态机生命周期。

| 概念 | 职责 | 代码文件 |
|------|------|----------|
| Task | 任务定义，封装单个可执行单元 | `task.go` |
| Queue | 任务队列，支持优先级与 FIFO | `queue.go` |
| Scheduler | 调度器，管理并发与状态流转 | `scheduler.go` |

```text
Planner 生成 Plan
  │
  │  将每个 Step 封装为 Task
  │
  ▼
Queue（入队）
  │
  │  按优先级 / FIFO 排序
  │
  ▼
Scheduler（调度）
  │
  │  检查依赖 → 分配 Worker → 并发执行
  │
  ▼
Worker Pool
  │
  │  执行 Task → 更新状态机
  │
  ▼
结果回调 / 状态广播
```

---

## 2. Task 定义

### 2.1 结构体

```go
// Task 表示一个可调度的任务单元，通常对应 Plan 中的一个 Step。
type Task struct {
    ID         string         // 任务唯一标识
    PlanID     string         // 所属 Plan 的 ID
    StepID     string         // 对应 Plan 中的 Step ID
    Action     string         // 动作类型: tool:<name> / skill:<name> / llm
    Input      map[string]any // 输入参数
    Depends    []string       // 依赖的前置 Task ID 列表
    Priority   Priority       // 优先级
    Status     Status         // 任务状态
    Result     *Result         // 执行结果（完成后填充）
    Error      error          // 执行错误（失败时填充）
    CreatedAt  time.Time      // 创建时间
    StartedAt  *time.Time     // 开始执行时间
    FinishedAt *time.Time     // 完成或失败时间
    RetryCount int            // 已重试次数
    MaxRetries int            // 最大重试次数
}
```

### 2.2 优先级

```go
type Priority int

const (
    PriorityLow    Priority = 0
    PriorityNormal Priority = 1  // 默认
    PriorityHigh   Priority = 2
    PriorityUrgent Priority = 3
)
```

### 2.3 执行结果

```go
// Result 封装 Task 执行的输出。
type Result struct {
    Output  map[string]any // 执行输出
    Metrics map[string]any // 执行指标（耗时、Token 用量等）
}
```

---

## 3. 任务状态机

Task 的生命周期由状态机管理，状态转换严格受控：

```text
                    ┌─────────┐
                    │ Pending │  ← 入队后的初始状态
                    └────┬────┘
                         │ Scheduler 分配 Worker
                         ▼
                    ┌─────────┐
         ┌─────────│ Running │
         │         └────┬────┘
         │              │
    超时/ │              ├──────────────┐
    重试  │              │              │ 执行完成
         │              │              ▼
         │              │         ┌──────┐
         │              │         │ Done │  ← 最终成功状态
         │              │         └──────┘
         │              │ 执行失败
         ▼              ▼
    ┌─────────┐    ┌─────────┐
    │ Retrying│───▶│ Failed  │  ← 最终失败状态
    └─────────┘    └─────────┘
```

### 3.1 状态枚举

```go
type Status string

const (
    StatusPending  Status = "pending"
    StatusRunning  Status = "running"
    StatusDone     Status = "done"
    StatusFailed   Status = "failed"
    StatusRetrying Status = "retrying"
)
```

### 3.2 状态转换规则

| 当前状态 | 允许的下一状态 | 触发条件 |
|----------|---------------|----------|
| `pending` | `running` | Scheduler 分配 Worker |
| `running` | `done` | 执行成功 |
| `running` | `failed` | 执行失败且不可重试 |
| `running` | `retrying` | 执行失败但可重试 |
| `retrying` | `running` | 重试等待结束，重新入队 |
| `retrying` | `failed` | 重试次数耗尽 |
| `done` | — | 终态，不可转换 |
| `failed` | — | 终态，不可转换 |

### 3.3 状态转换实现

```go
// transition 执行状态转换，违反规则时返回错误。
func (t *Task) transition(to Status) error {
    transitions := map[Status]map[Status]bool{
        StatusPending:  {StatusRunning: true},
        StatusRunning:  {StatusDone: true, StatusFailed: true, StatusRetrying: true},
        StatusRetrying: {StatusRunning: true, StatusFailed: true},
        StatusDone:     {},
        StatusFailed:   {},
    }

    allowed, ok := transitions[t.Status][to]
    if !ok || !allowed {
        return fmt.Errorf("invalid transition: %s → %s", t.Status, to)
    }
    t.Status = to
    return nil
}
```

---

## 4. Task Queue

任务队列负责管理待执行任务的排序与取出。默认实现为**优先级队列**：高优先级任务优先出队，同优先级按 FIFO 顺序。

```go
// Queue 是任务队列接口。
type Queue interface {
    Push(task *Task) error              // 入队
    Pop() (*Task, bool)                 // 出队（阻塞由调用方控制）
    Peek() (*Task, bool)                // 查看队首但不移除
    Len() int                           // 队列长度
    Remove(taskID string) bool         // 移除指定任务
    Clear()                             // 清空队列
}
```

### 4.1 优先级队列实现

```go
// priorityQueue 基于堆实现优先级队列。
// 高优先级先出；同优先级按 CreatedAt 先进先出。
type priorityQueue struct {
    mu   sync.Mutex
    items []*Task
}

func (q *priorityQueue) Push(task *Task) error {
    q.mu.Lock()
    defer q.mu.Unlock()
    if task.Status != StatusPending {
        return fmt.Errorf("only pending tasks can be enqueued, got %s", task.Status)
    }
    heap.Push(q, task)
    return nil
}

func (q *priorityQueue) Pop() (*Task, bool) {
    q.mu.Lock()
    defer q.mu.Unlock()
    if len(q.items) == 0 {
        return nil, false
    }
    return heap.Pop(q).(*Task), true
}

// --- heap.Interface 实现 ---

func (q *priorityQueue) Len() int { return len(q.items) }

func (q *priorityQueue) Less(i, j int) bool {
    if q.items[i].Priority != q.items[j].Priority {
        return q.items[i].Priority > q.items[j].Priority // 高优先级在前
    }
    return q.items[i].CreatedAt.Before(q.items[j].CreatedAt) // FIFO
}

func (q *priorityQueue) Swap(i, j int) {
    q.items[i], q.items[j] = q.items[j], q.items[i]
}
```

---

## 5. Scheduler

Scheduler 是调度系统的核心，负责从 Queue 中取出任务、检查依赖、分配 Worker 并发执行，并维护状态机流转。

### 5.1 结构体

```go
// Scheduler 管理任务调度与并发执行。
type Scheduler struct {
    queue    Queue
    executor Executor           // 任务执行器接口
    workers  int                // 最大并发 Worker 数
    sem      chan struct{}      // 并发信号量
    mu       sync.RWMutex
    tasks    map[string]*Task   // 全部任务索引（ID → Task）
    stop     chan struct{}      // 停止信号
    wg       sync.WaitGroup     // 等待所有 Worker 退出
    onDone   func(task *Task)   // 完成回调（可选）
    onFail   func(task *Task)   // 失败回调（可选）
}

// Executor 是任务执行器接口，由 Agent / Tool Manager 等实现。
type Executor interface {
    Execute(ctx context.Context, task *Task) (*Result, error)
}
```

### 5.2 调度循环

```go
// Start 启动调度器主循环。
func (s *Scheduler) Start(ctx context.Context) {
    s.wg.Add(1)
    go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
    defer s.wg.Done()
    for {
        select {
        case <-ctx.Done():
            return
        case <-s.stop:
            return
        default:
            // 从队列取出任务
            task, ok := s.queue.Pop()
            if !ok {
                time.Sleep(100 * time.Millisecond) // 队列为空，短暂等待
                continue
            }
            // 获取并发信号量（控制最大并发数）
            s.sem <- struct{}{}
            s.wg.Add(1)
            go s.run(ctx, task)
        }
    }
}
```

### 5.3 任务执行

```go
// run 执行单个任务，处理状态流转、重试与回调。
func (s *Scheduler) run(ctx context.Context, task *Task) {
    defer s.wg.Done()
    defer func() { <-s.sem }() // 释放信号量

    // 1. 检查依赖是否全部完成
    if !s.dependenciesMet(task) {
        // 依赖未满足，重新入队等待
        time.Sleep(200 * time.Millisecond)
        _ = s.queue.Push(task)
        return
    }

    // 2. 状态转换: pending → running
    s.mu.Lock()
    if err := task.transition(StatusRunning); err != nil {
        s.mu.Unlock()
        return
    }
    now := time.Now()
    task.StartedAt = &now
    s.mu.Unlock()

    // 3. 执行任务
    result, err := s.executor.Execute(ctx, task)

    // 4. 处理结果
    s.mu.Lock()
    defer s.mu.Unlock()
    finishedAt := time.Now()
    task.FinishedAt = &finishedAt

    if err != nil {
        task.Error = err
        // 判断是否可重试
        if task.RetryCount < task.MaxRetries {
            task.RetryCount++
            _ = task.transition(StatusRetrying)
            // 异步重试：等待后重新入队
            go s.scheduleRetry(ctx, task)
            return
        }
        _ = task.transition(StatusFailed)
        if s.onFail != nil {
            s.onFail(task)
        }
        return
    }

    task.Result = result
    _ = task.transition(StatusDone)
    if s.onDone != nil {
        s.onDone(task)
    }
}

// scheduleRetry 在退避延迟后重新入队。
func (s *Scheduler) scheduleRetry(ctx context.Context, task *Task) {
    backoff := time.Duration(task.RetryCount) * time.Second
    select {
    case <-ctx.Done():
        return
    case <-time.After(backoff):
        s.mu.Lock()
        _ = task.transition(StatusRunning)
        s.mu.Unlock()
        _ = s.queue.Push(task)
    }
}
```

### 5.4 依赖检查

```go
// dependenciesMet 检查 Task 的所有依赖是否已 Done。
func (s *Scheduler) dependenciesMet(task *Task) bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    for _, depID := range task.Depends {
        dep, ok := s.tasks[depID]
        if !ok || dep.Status != StatusDone {
            return false
        }
    }
    return true
}
```

### 5.5 提交与停止

```go
// Submit 提交任务到调度器。
func (s *Scheduler) Submit(task *Task) error {
    s.mu.Lock()
    s.tasks[task.ID] = task
    s.mu.Unlock()
    return s.queue.Push(task)
}

// Stop 优雅停止调度器，等待所有运行中的任务完成。
func (s *Scheduler) Stop() {
    close(s.stop)
    s.wg.Wait()
}
```

---

## 6. 并发执行模型

Yaa! 的并发模型遵循架构文档 §5 的约定：**Runtime 单进程，多协程**。Scheduler 通过信号量控制最大并发数，避免无限制创建 goroutine。

```text
                         Scheduler
                            │
              ┌─────────────┼─────────────┐
              │             │             │
         Worker 1       Worker 2       Worker N
         (goroutine)   (goroutine)   (goroutine)
              │             │             │
         ┌────┴────┐   ┌────┴────┐   ┌────┴────┐
         │ Task A  │   │ Task B  │   │ Task C  │
         │ Running │   │ Running │   │ Running │
         └─────────┘   └─────────┘   └─────────┘

    并发控制: sem chan struct{}  (容量 = workers)
    依赖检查: dependenciesMet()  (阻塞未满足依赖的任务)
    重试退避: 指数退避策略        (RetryCount × 1s)
```

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `workers` | 4 | 最大并发 Worker 数 |
| `MaxRetries` | 3 | 单个任务最大重试次数 |
| 重试退避 | `RetryCount × 1s` | 线性退避 |
| 空队列轮询间隔 | 100ms | 队列为空时的等待间隔 |
| 依赖未满足重入队间隔 | 200ms | 依赖未完成时的等待间隔 |

---

## 7. 与 Planner 的集成

Task 调度系统是 Planner 的下游执行引擎。Planner 生成 Plan 后，将每个 Step 转换为 Task 提交给 Scheduler：

```go
// Planner 将 Plan 中的 Step 提交给 Scheduler
func (p *Planner) submitPlan(ctx context.Context, plan *Plan, sched *task.Scheduler) error {
    for _, step := range plan.Steps {
        t := &task.Task{
            ID:       generateID(),
            PlanID:   plan.ID,
            StepID:   step.ID,
            Action:   step.Action,
            Input:    step.Input,
            Depends:  step.Depends,
            Priority: task.PriorityNormal,
            Status:   task.StatusPending,
            MaxRetries: 3,
            CreatedAt: time.Now(),
        }
        if err := sched.Submit(t); err != nil {
            return fmt.Errorf("submit task %s: %w", t.ID, err)
        }
    }
    return nil
}
```

**执行顺序与依赖关系：**

```text
Plan: Step1 → Step2 → Step3
                ↘ Step4 (Depends: Step1)

转换为 Task 后:
  Task1 (Depends: [])        → 立即可执行
  Task2 (Depends: [Task1])   → 等待 Task1 Done
  Task3 (Depends: [Task2])   → 等待 Task2 Done
  Task4 (Depends: [Task1])   → 等待 Task1 Done（与 Task2 并行）

执行时间线:
  t0: Task1 Running
  t1: Task1 Done → Task2 + Task4 同时 Running（并发）
  t2: Task2 Done → Task3 Running
  t3: Task4 Done
  t4: Task3 Done → Plan 完成
```

---

## 8. 模块关系

```text
┌──────────────────────────────────────────────────────┐
│                     Planner                           │
│   Plan{Steps} → 转换为 Task[] → Submit 到 Scheduler   │
└──────────────────────┬───────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│                    Scheduler                          │
│  ┌─────────┐   ┌───────────┐   ┌─────────────────┐  │
│  │  Queue   │──▶│  Dispatch │──▶│  Worker Pool    │  │
│  │ (优先级) │   │  (依赖检查)│   │  (并发 goroutine)│  │
│  └─────────┘   └───────────┘   └───────┬─────────┘  │
│                                        │             │
│                   ┌────────────────────┘             │
│                   ▼                                  │
│              Executor 接口                            │
│              ├── Tool Manager (tool:<name>)          │
│              ├── Skill Manager (skill:<name>)       │
│              └── Provider (llm)                      │
└──────────────────────────────────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────┐
│                    Session                            │
│  Task 状态变更 → 广播事件 → WS/SSE → Client           │
└──────────────────────────────────────────────────────┘
```

---

## 9. 配置参考

```yaml
# yaa.yaml — Task 调度器配置
scheduler:
  workers: 4              # 最大并发 Worker 数
  default_max_retries: 3   # 默认最大重试次数
  queue_poll_interval: 100ms  # 空队列轮询间隔
  retry_base_backoff: 1s  # 重试退避基数
```

---

## 10. 设计要点总结

| 要点 | 说明 |
|------|------|
| 状态机驱动 | Task 生命周期由严格状态机管理，禁止非法转换 |
| 优先级队列 | 高优先级任务先执行，同优先级 FIFO |
| 并发控制 | 信号量限制最大并发 goroutine 数，防止资源耗尽 |
| 依赖感知 | 执行前检查依赖状态，未满足则重新入队 |
| 自动重试 | 失败后按线性退避策略重试，超过上限标记 Failed |
| 回调机制 | onDone / onFail 回调支持外部观察与事件广播 |
| 优雅停止 | Stop() 等待所有运行中任务完成，不强制中断 |
| 接口解耦 | Executor 接口与 Scheduler 解耦，执行逻辑可替换 |

---

*最后更新: 2025-07-17*
