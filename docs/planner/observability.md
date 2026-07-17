# Planner 可观测性

> 文档路径: `docs/planner/observability.md`
> 上级: `docs/planner/README.md` §4
> 依赖: `docs/planner/planner.md` §3 结构体定义

---

本文档定义 Planner 系统的日志事件、执行指标、SSE 事件流与状态追踪机制，确保 Plan 的规划与执行全程可观测、可审计、可回放。

---

## 1. 规划日志事件表

Planner 在各生命周期阶段发出结构化日志事件，统一使用 `log.Event` 格式：

| 事件名 | 触发时机 | 关键字段 | 级别 |
|--------|----------|----------|------|
| `plan.start` | Planner 开始规划 | `plan_id`, `task`, `agent_id`, `strategy` | INFO |
| `plan.created` | Plan 生成完成 | `plan_id`, `step_count`, `duration_ms` | INFO |
| `plan.rejected` | Plan 被拒绝（校验失败/用户拒绝） | `plan_id`, `reason` | WARN |
| `plan.revised` | Plan 被用户或系统修正 | `plan_id`, `changed_steps`, `reason` | INFO |
| `plan.started` | Plan 进入执行 | `plan_id`, `parallelism` | INFO |
| `plan.completed` | Plan 全部步骤完成 | `plan_id`, `total_duration_ms`, `step_count` | INFO |
| `plan.failed` | Plan 执行失败终止 | `plan_id`, `failed_step_id`, `error` | ERROR |
| `plan.cancelled` | Plan 被取消 | `plan_id`, `reason`, `completed_steps` | WARN |

```go
// PlanLogger 负责输出 Planner 结构化日志事件。
type PlanLogger struct {
    logger *slog.Logger
}

func (l *PlanLogger) OnPlanCreated(p *Plan, duration time.Duration) {
    l.logger.Info("plan.created",
        slog.String("plan_id", p.ID),
        slog.String("task", p.Task),
        slog.Int("step_count", len(p.Steps)),
        slog.Int64("duration_ms", duration.Milliseconds()),
    )
}
```

---

## 2. 执行指标表

Planner 运行时采集以下 Prometheus 指标，前缀统一为 `yaa_planner_`：

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `yaa_planner_plan_total` | Counter | `strategy`, `status` | Plan 总数（按策略与终态） |
| `yaa_planner_plan_duration_seconds` | Histogram | `strategy` | 规划阶段耗时分布 |
| `yaa_planner_step_total` | Counter | `action`, `status` | Step 执行总数 |
| `yaa_planner_step_duration_seconds` | Histogram | `action` | Step 执行耗时分布 |
| `yaa_planner_plan_execution_seconds` | Histogram | — | Plan 端到端执行耗时 |
| `yaa_planner_active_plans` | Gauge | `strategy` | 当前正在执行的 Plan 数 |
| `yaa_planner_active_steps` | Gauge | `action` | 当前正在执行的 Step 数 |
| `yaa_planner_step_retry_total` | Counter | `action`, `reason` | Step 重试次数 |
| `yaa_planner_plan_revision_total` | Counter | `reason` | Plan 被修正次数 |

```go
var (
    planTotal = promauto.NewCounterVec(prometheus.CounterOpts{
        Name: "yaa_planner_plan_total",
        Help: "Total number of plans created.",
    }, []string{"strategy", "status"})

    stepDuration = promauto.NewHistogramVec(prometheus.CounterOpts{
        Name:    "yaa_planner_step_duration_seconds",
        Help:    "Step execution duration.",
        Buckets: prometheus.DefBuckets,
    }, []string{"action"})

    activePlans = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "yaa_planner_active_plans",
        Help: "Number of plans currently executing.",
    }, []string{"strategy"})
)
```

---

## 3. SSE 事件表

Plan 执行过程中通过 SSE 向 Client 推送实时状态，事件前缀为 `plan.`：

| SSE 事件 | 方向 | Payload 字段 | 说明 |
|----------|------|-------------|------|
| `plan.created` | Server → Client | `plan_id`, `task`, `steps[]` | Plan 生成完毕，Client 可预览 |
| `plan.step_updated` | Server → Client | `plan_id`, `step_id`, `status`, `action` | Step 状态变更通知 |
| `plan.step_output` | Server → Client | `plan_id`, `step_id`, `output`, `partial` | Step 产出（可多次，流式） |
| `plan.step_error` | Server → Client | `plan_id`, `step_id`, `error` | Step 执行出错 |
| `plan.completed` | Server → Client | `plan_id`, `summary`, `duration_ms` | Plan 全部完成 |
| `plan.failed` | Server → Client | `plan_id`, `failed_step_id`, `error` | Plan 执行失败终止 |

```go
// SSEPlanStream 通过 SSE 推送 Plan 执行事件。
type SSEPlanStream struct {
    writer http.ResponseWriter
    flusher http.Flusher
}

func (s *SSEPlanStream) Send(event string, payload any) error {
    data, _ := json.Marshal(payload)
    fmt.Fprintf(s.writer, "event: %s\ndata: %s\n\n", event, data)
    s.flusher.Flush()
    return nil
}

// SendStepUpdated 推送 Step 状态变更。
func (s *SSEPlanStream) SendStepUpdated(planID, stepID string, status StepStatus) {
    s.Send("plan.step_updated", map[string]any{
        "plan_id": planID,
        "step_id": stepID,
        "status":  status,
    })
}
```

---

## 4. Plan / Step 状态追踪

### 4.1 状态机

```text
Plan:   pending ──▶ running ──┬──▶ completed
                             └──▶ failed
                             └──▶ cancelled

Step:   pending ──▶ running ──┬──▶ done
                             └──▶ failed
                             └──▶ skipped  (前置依赖失败时)
```

### 4.2 状态快照

Executor 在每次状态变更时记录快照，支持回放与审计：

```go
// PlanSnapshot 是 Plan 在某一时刻的完整状态快照。
type PlanSnapshot struct {
    PlanID    string                `json:"plan_id"`
    Status    PlanStatus            `json:"status"`
    Steps     []StepSnapshot        `json:"steps"`
    Timestamp time.Time             `json:"timestamp"`
    ActiveStepIDs []string          `json:"active_step_ids"`
}

// StepSnapshot 是单个 Step 的状态快照。
type StepSnapshot struct {
    StepID    string      `json:"step_id"`
    Status    StepStatus  `json:"status"`
    Action    string      `json:"action"`
    StartedAt *time.Time  `json:"started_at,omitempty"`
    EndedAt   *time.Time  `json:"ended_at,omitempty"`
    Error     string      `json:"error,omitempty"`
}

// Snapshot 生成当前 Plan 的状态快照。
func (p *Plan) Snapshot() PlanSnapshot {
    snap := PlanSnapshot{
        PlanID:    p.ID,
        Status:     p.Status,
        Timestamp:  time.Now(),
    }
    for i := range p.Steps {
        s := &p.Steps[i]
        ss := StepSnapshot{
            StepID: s.ID, Status: s.Status, Action: s.Action,
            StartedAt: s.StartedAt, EndedAt: s.EndedAt,
        }
        if s.Error != "" {
            ss.Error = s.Error
        }
        if s.Status == StepRunning {
            snap.ActiveStepIDs = append(snap.ActiveStepIDs, s.ID)
        }
        snap.Steps = append(snap.Steps, ss)
    }
    return snap
}
```

---

*最后更新: 2025-07-17*
