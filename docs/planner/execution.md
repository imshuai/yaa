# 计划执行流程

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/planner/execution.md`
> 依赖: `docs/planner/README.md` §3 核心接口, `docs/architecture.md` §3.5

---

## 1. 概述

本文档描述 Plan 从生成到执行完成的完整流程，包括：

- 依赖解析与 DAG 构建
- 并行/串行执行策略
- 错误处理与重试机制
- 执行状态流转

---

## 2. 执行总流程

```text
┌──────────────────────────────────────────────────────────┐
│                     Execution Pipeline                    │
│                                                          │
│  Plan (Pending)                                           │
│    │                                                      │
│    ▼                                                      │
│  1. 构建 DAG ──────────────────► 依赖图 (有向无环图)      │
│    │                                                      │
│    ▼                                                      │
│  2. 拓扑排序 ──────────────────► 执行层级划分             │
│    │                                                      │
│    ▼                                                      │
│  3. 逐层执行                                               │
│    │   ├── 同层无依赖步骤 → 并行执行 (goroutine)          │
│    │   └── 跨层步骤       → 串行等待                       │
│    │                                                      │
│    ▼                                                      │
│  4. 结果收集与状态更新                                     │
│    │                                                      │
│    ├── 全部成功 → Plan (Completed)                        │
│    └── 任一失败 → 错误处理（重试 / 跳过 / 中止）          │
│                                                          │
└──────────────────────────────────────────────────────────┘
```

---

## 3. 依赖解析 — DAG 构建

### 3.1 依赖图模型

Plan 中的 Step 通过 `Depends` 字段声明依赖关系，构成一个 **有向无环图 (DAG)**：

```go
// DAG 节点
type DAGNode struct {
    Step     *Step
    Parents  []*DAGNode  // 依赖的前置节点
    Children []*DAGNode  // 依赖此节点的后续节点
}

// 执行用 DAG
type ExecutionDAG struct {
    Nodes map[string]*DAGNode // stepID → node
    Roots []*DAGNode           // 无依赖的入口节点
}

// BuildDAG 根据 Plan 构建 DAG
func BuildDAG(plan *Plan) (*ExecutionDAG, error) {
    dag := &ExecutionDAG{Nodes: make(map[string]*DAGNode)}

    // 第一遍：创建所有节点
    for i := range plan.Steps {
        step := &plan.Steps[i]
        dag.Nodes[step.ID] = &DAGNode{Step: step}
    }

    // 第二遍：连接依赖边
    for i := range plan.Steps {
        step := &plan.Steps[i]
        node := dag.Nodes[step.ID]

        for _, depID := range step.Depends {
            dep, ok := dag.Nodes[depID]
            if !ok {
                return nil, fmt.Errorf("step %s depends on unknown step %s", step.ID, depID)
            }
            node.Parents = append(node.Parents, dep)
            dep.Children = append(dep.Children, node)
        }

        if len(node.Parents) == 0 {
            dag.Roots = append(dag.Roots, node)
        }
    }

    // 检测环
    if cycle := detectCycle(dag); cycle != nil {
        return nil, fmt.Errorf("dependency cycle detected: %v", cycle)
    }

    return dag, nil
}
```

### 3.2 拓扑排序

拓扑排序将 DAG 划分为执行层级，同层步骤可并行执行：

```go
// TopologicalLevels 返回按层级组织的步骤列表。
// 同一层级的步骤之间无依赖关系，可并行执行。
func TopologicalLevels(dag *ExecutionDAG) [][]*DAGNode {
    var levels [][]*DAGNode
    indegree := make(map[string]int)

    // 计算入度
    for id, node := range dag.Nodes {
        indegree[id] = len(node.Parents)
    }

    // BFS 分层
    var current []*DAGNode
    for _, root := range dag.Roots {
        current = append(current, root)
    }

    for len(current) > 0 {
        levels = append(levels, current)
        var next []*DAGNode
        for _, node := range current {
            for _, child := range node.Children {
                indegree[child.Step.ID]--
                if indegree[child.Step.ID] == 0 {
                    next = append(next, child)
                }
            }
        }
        current = next
    }

    return levels
}
```

### 3.3 示例 DAG

假设有以下 Plan：

| Step | Action | Depends |
|------|--------|---------|
| s1 | search_web | — |
| s2 | search_db | — |
| s3 | parse_results | s1, s2 |
| s4 | generate_report | s3 |
| s5 | save_to_file | s3 |

```text
Level 0:  [s1, s2]        ← 并行执行
              │  │
              ▼  ▼
Level 1:  [s3]             ← 等待 s1, s2 完成
           / \
          ▼   ▼
Level 2: [s4, s5]          ← 并行执行
```

---

## 4. 执行引擎

### 4.1 Executor 结构

```go
type Executor struct {
    dag      *ExecutionDAG
    levels   [][]*DAGNode
    results  map[string]StepResult // stepID → result
    mu       sync.RWMutex
    strategy ExecStrategy
}

type ExecStrategy struct {
    MaxParallel  int           // 最大并行度，0 = 不限
    RetryMax     int           // 最大重试次数
    RetryDelay   time.Duration // 重试间隔
    FailMode     FailMode      // 失败策略
}

type FailMode string

const (
    FailAbort  FailMode = "abort"  // 任一步骤失败则中止整个 Plan
    FailSkip   FailMode = "skip"   // 跳过失败步骤，继续执行不依赖它的步骤
    FailRetry  FailMode = "retry"  // 重试失败步骤
)
```

### 4.2 执行主循环

```go
func (e *Executor) Execute(ctx context.Context) error {
    for levelIdx, level := range e.levels {
        // 同层步骤并行执行
        var wg sync.WaitGroup
        errCh := make(chan error, len(level))

        for _, node := range level {
            wg.Add(1)
            go func(n *DAGNode) {
                defer wg.Done()

                // 检查依赖是否全部成功
                if !e.dependenciesSatisfied(n) {
                    errCh <- fmt.Errorf("step %s dependencies not satisfied", n.Step.ID)
                    return
                }

                // 执行步骤（含重试逻辑）
                result, err := e.executeStepWithRetry(ctx, n.Step)
                e.mu.Lock()
                e.results[n.Step.ID] = result
                e.mu.Unlock()

                if err != nil {
                    errCh <- err
                    return
                }
            }(node)
        }

        wg.Wait()
        close(errCh)

        // 收集本层错误
        var errs []error
        for err := range errCh {
            errs = append(errs, err)
        }

        if len(errs) > 0 && e.strategy.FailMode == FailAbort {
            return fmt.Errorf("level %d failed: %v", levelIdx, errs)
        }
    }

    return nil
}
```

---

## 5. 错误处理与重试

### 5.1 重试机制

```go
func (e *Executor) executeStepWithRetry(ctx context.Context, step *Step) (StepResult, error) {
    var lastErr error

    for attempt := 0; attempt <= e.strategy.RetryMax; attempt++ {
        step.Status = StepRunning

        result, err := e.executeStep(ctx, step)
        if err == nil {
            step.Status = StepDone
            return result, nil
        }

        lastErr = err
        log.Printf("step %s attempt %d failed: %v", step.ID, attempt+1, err)

        if attempt < e.strategy.RetryMax {
            // 指数退避
            delay := e.strategy.RetryDelay * time.Duration(1<<attempt)
            select {
            case <-ctx.Done():
                return StepResult{}, ctx.Err()
            case <-time.After(delay):
            }
        }
    }

    step.Status = StepFailed
    return StepResult{}, fmt.Errorf("step %s failed after %d retries: %w",
        step.ID, e.strategy.RetryMax, lastErr)
}
```

### 5.2 失败策略对比

| 策略 | 行为 | 适用场景 | 优点 | 缺点 |
|------|------|----------|------|------|
| `abort` | 立即中止整个 Plan | 强一致性任务 | 避免浪费资源 | 部分结果丢失 |
| `skip` | 跳过失败步骤，继续不依赖它的步骤 | 容错型任务 | 最大化完成度 | 结果可能不完整 |
| `retry` | 重试失败步骤（指数退避） | 网络波动等临时错误 | 自动恢复 | 可能延迟较长 |

### 5.3 依赖失败传播

当某步骤失败时，其所有下游步骤的处理取决于 `FailMode`：

```text
Step s3 失败
    │
    ├── FailAbort:  中止 → s4, s5 不执行
    ├── FailSkip:   跳过 → s4, s5 标记为 Skipped
    └── FailRetry:  重试 → s3 重试成功后 s4, s5 正常执行
                    s3 重试失败 → 降级为 FailSkip 或 FailAbort
```

```go
func (e *Executor) propagateFailure(failedNode *DAGNode) {
    queue := []*DAGNode{failedNode}
    for len(queue) > 0 {
        node := queue[0]
        queue = queue[1:]

        for _, child := range node.Children {
            if child.Step.Status == StepPending {
                child.Step.Status = StepFailed
                child.Step.Error = fmt.Sprintf("upstream step %s failed", node.Step.ID)
                queue = append(queue, child)
            }
        }
    }
}
```

---

## 6. 并行/串行执行策略

### 6.1 策略选择

| 策略 | 说明 | 配置 |
|------|------|------|
| 全并行 | 所有无依赖步骤同时执行 | `MaxParallel = 0` |
| 限流并行 | 限制最大并发数 | `MaxParallel = N` |
| 全串行 | 严格按顺序执行 | `MaxParallel = 1` |

### 6.2 并行度控制

```go
func (e *Executor) Execute(ctx context.Context) error {
    sem := make(chan struct{}, e.strategy.MaxParallel) // 信号量控制并行度

    for _, level := range e.levels {
        var wg sync.WaitGroup
        for _, node := range level {
            wg.Add(1)
            go func(n *DAGNode) {
                defer wg.Done()

                if e.strategy.MaxParallel > 0 {
                    sem <- struct{}{}        // 获取令牌
                    defer func() { <-sem }() // 释放令牌
                }

                _, err := e.executeStepWithRetry(ctx, n.Step)
                if err != nil {
                    e.handleFailure(n, err)
                }
            }(node)
        }
        wg.Wait() // 等待同层全部完成
    }
    return nil
}
```

---

## 7. 完整执行流程图

```text
                        ┌─────────┐
                        │ Plan 生成 │
                        │ (Pending) │
                        └────┬─────┘
                             │
                    ▼────────┴────────▼
                 ┌─┴───────────────┴─┐
                 │  构建 DAG + 环检测 │
                 └────────┬──────────┘
                          │ 有环?
                   ┌──────┴──────┐
                   ▼             ▼
                 是              否
                   │             │
            ┌──────▼──────┐      ▼
            │  返回错误    │  ┌───┴────────────┐
            └─────────────┘  │  拓扑排序分层    │
                           └──────┬───────────┘
                                  ▼
                     ┌────────────────────┐
                     │  遍历每一层 (Level)  │
                     └────────┬───────────┘
                              ▼
                   ┌──────────────────────┐
                   │  同层步骤并行执行      │
                   │  (信号量限流)         │
                   └────────┬─────────────┘
                            ▼
                   ┌──────────────────────┐
                   │  步骤执行 + 重试      │
                   │  (指数退避)           │
                   └────────┬─────────────┘
                            │
                     ┌──────┴──────┐
                     ▼             ▼
                   成功           失败
                     │             │
                     │      ┌──────┴──────┐
                     │      ▼             ▼
                     │   FailRetry     Abort/Skip
                     │   重试到上限       │
                     │      │             │
                     │      ▼             │
                     │   传播失败       标记下游
                     │   到下游         Skipped
                     │      │             │
                     ▼      ▼             ▼
              ┌──────────────────────────────┐
              │     还有下一层?               │
              └──────┬──────────┬────────────┘
                     ▼          ▼
                    是          否
                     │          │
              回到遍历层     ┌───┴──────┐
                           ▼          ▼
                    全部成功      有失败
                           │          │
                    ┌──────┴──┐  ┌────┴─────┐
                    ▼         ▼  ▼          │
              Plan.Completed  Plan.Failed  │
                              │            │
                              └────────────┘
```

---

## 8. 执行结果收集

```go
type StepResult struct {
    StepID    string         `json:"step_id"`
    Status    StepStatus     `json:"status"`
    Output    map[string]any `json:"output"`
    Error     string         `json:"error,omitempty"`
    Duration  time.Duration  `json:"duration"`
    Attempts  int            `json:"attempts"`
}

type PlanResult struct {
    PlanID   string                `json:"plan_id"`
    Status   PlanStatus            `json:"status"`
    Steps    map[string]StepResult `json:"steps"`
    Total    time.Duration         `json:"total_duration"`
}
```

执行完成后，`PlanResult` 被写入 Context，供后续 Agent Loop 或用户查看。

---

*最后更新: 2025-07-17*
