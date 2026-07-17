# Planner 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/planner/` (原计划单文件 `docs/planner.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.5, §4 数据流

---

## 1. 概述

### 1.1 什么是 Planner

Planner 是 Yaa! 中**负责任务分解与执行编排**的核心模块。

当 Agent 接收到一个复杂任务时，Planner 将该任务拆解为一系列有序的、可执行的步骤（Step），每个步骤对应一个具体的 Action（Tool 调用、Skill 调用或 LLM 推理），并声明步骤间的依赖关系，最终生成一个结构化的执行计划（Plan）。

| 层级 | 职责 | 类比 |
|------|------|------|
| Tool | 原子操作执行 | 系统调用 |
| Skill | 多步骤工作流 + 领域知识 | 应用程序 |
| **Planner** | **任务分解 + 步骤编排 + 依赖管理** | **项目经理** |
| Agent | 人格 + 能力集合 + 会话管理 | 用户会话 |

### 1.2 与 Agent Loop 的关系

Planner 不是 Agent Loop 的替代，而是 Agent Loop 的**前置增强**。两者的协作关系如下：

```text
用户输入任务
  │
  ▼
Planner.Plan(ctx, task, agent)
  │
  │  将复杂任务分解为 Step 列表
  │  生成 Plan{Steps: [...]}
  │
  ▼
Agent Loop（执行 Plan）
  │
  │  for each step in plan.Steps:
  │    1. 检查 Depends 是否全部 Done
  │    2. 执行 Step.Action（Tool / Skill / LLM）
  │    3. 更新 Step.Status
  │    4. 将结果加入 Context
  │
  ▼
最终回复
```

**关键区分：**

| 维度 | Agent Loop | Planner |
|------|-----------|---------|
| 触发时机 | 每轮对话 | 复杂任务（可选） |
| 决策方式 | LLM 逐步决策 | LLM 一次性规划 |
| 适用场景 | 简单问答、单步任务 | 多步骤、有依赖的复杂任务 |
| 可见性 | 隐式（LLM 内部推理） | 显式（结构化 Plan） |
| 可控性 | 低（LLM 自主） | 高（可审查、可修改） |

### 1.3 核心原则

1. **Interface First** — Planner 通过接口定义，可替换实现
2. **LLM-Driven** — 默认实现由 LLM 驱动，通过 Prompt 引导规划
3. **Declarative** — Plan 是声明式的，描述"做什么"而非"怎么做"
4. **Dependency-Aware** — Step 间通过 Depends 声明依赖，支持并行与串行
5. **Inspectable** — Plan 和 Step 状态可被外部观察与修改
6. **Optional** — Planner 是可选模块，简单任务可不经过 Planner

---

## 2. 设计理念

### 2.1 为什么需要 Planner

原生 Agent Loop 依赖 LLM 在每一步自主决策，存在以下问题：

| 问题 | 说明 |
|------|------|
| 不可预测 | LLM 可能在中途改变策略，导致执行路径不可控 |
| 不可审查 | 用户无法在执行前预览计划 |
| 不可重放 | 缺乏结构化记录，难以复现或调试 |
| 不可并行 | LLM 逐步决策，无法利用步骤间的并行性 |
| 上下文膨胀 | 长任务中规划与执行混在一起，Context 快速膨胀 |

Planner 通过**先规划、后执行**的模式解决这些问题：

```text
传统 Agent Loop:
  用户任务 → LLM 决策 → 执行 → LLM 决策 → 执行 → ... → 完成
  （每步都需要完整 Context，Token 开销大）

Planner 模式:
  用户任务 → Planner 规划 → Plan{Step1, Step2, ...}
  → Step1 执行 → Step2 执行（可并行） → ... → 完成
  （规划一次，执行多次，Token 效率高）
```

### 2.2 规划与执行的分离

```text
┌─────────────────────────────────────────────────┐
│                  Planning Phase                  │
│                                                  │
│  输入: task description + agent capabilities     │
│  输出: Plan{Steps: [Step1, Step2, Step3, ...]}   │
│                                                  │
│  ┌──────┐    ┌──────┐    ┌──────┐               │
│  │Step 1│───▶│Step 2│───▶│Step 3│               │
│  │Search│    │Parse│    │Save │               │
│  └──────┘    └──────┘    └──────┘               │
│                  │                               │
│                  └──────┐                        │
│                         ▼                        │
│                  ┌──────┐                        │
│                  │Step 4│  (Depends: Step 2)    │
│                  │Report│                        │
│                  └──────┘                        │
└─────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────┐
│                 Execution Phase                  │
│                                                  │
│  for step in plan.Steps:                         │
│    if all(dep.status == Done for dep in step):  │
│      execute(step) → update step.status         │
│                                                  │
│  → Step 1 (Pending → Running → Done)            │
│  → Step 2 (Pending → Running → Done)            │
│  → Step 3 (Pending → Running → Done) ← 并行     │
│  → Step 4 (Pending → Running → Done)            │
└─────────────────────────────────────────────────┘
```

### 2.3 可替换的规划策略

Planner 是接口，默认提供 LLM 驱动规划器，但支持多种实现：

| 实现策略 | 说明 | 适用场景 |
|----------|------|----------|
| LLM 规划器（默认） | 通过 Prompt 让 LLM 输出结构化 Plan | 通用复杂任务 |
| 静态规划器 | 基于预定义模板生成 Plan | 固定流程任务 |
| 交互式规划器 | 与用户交互确认每一步 | 需要人工审核的任务 |
| 混合规划器 | LLM 规划 + 人工修正 | 高风险任务 |

---

## 3. 核心接口定义

### 3.1 Planner 接口

```go
// Planner 负责将复杂任务分解为可执行步骤。
type Planner interface {
    Plan(ctx context.Context, task string, agent *agent.Agent) (*Plan, error)
}
```

### 3.2 Plan 结构体

```go
// Plan 表示一个执行计划，包含有序的步骤列表。
type Plan struct {
    ID     string    // 计划唯一标识
    Task   string    // 原始任务描述
    Steps  []Step    // 步骤列表
    Status PlanStatus // Pending / Running / Completed / Failed
}
```

### 3.3 Step 结构体

```go
// Step 表示计划中的一个执行步骤。
type Step struct {
    ID      string         // 步骤唯一标识
    Action  string         // 动作类型: tool / skill / llm
    Input   map[string]any // 步骤输入参数
    Depends []string       // 依赖的前置步骤 ID
    Status  StepStatus     // Pending / Running / Done / Failed
}
```

### 3.4 状态枚举

```go
type PlanStatus string

const (
    PlanPending   PlanStatus = "pending"
    PlanRunning   PlanStatus = "running"
    PlanCompleted PlanStatus = "completed"
    PlanFailed    PlanStatus = "failed"
)

type StepStatus string

const (
    StepPending StepStatus = "pending"
    StepRunning StepStatus = "running"
    StepDone    StepStatus = "done"
    StepFailed  StepStatus = "failed"
)
```

---

## 4. 文件索引

| 文件 | 内容 |
|------|------|
| [planner.md](planner.md) | Planner 接口与实现详解 — 结构体定义、LLM 驱动规划器、规划 Prompt 设计、Go 代码示例 |
| [task.md](task.md) | Task 调度系统 — Task 定义、优先级队列、Scheduler、并发执行模型 |
| [execution.md](execution.md) | Plan 执行流程 — 依赖调度、并行执行、错误处理与重试 |
| [integration.md](integration.md) | 与 Agent / Tool / Skill 的集成 |
| [config-ref.md](config-ref.md) | 配置参考 — Planner 配置、调度器参数 |
| [errors.md](errors.md) | 错误处理 — 规划失败、执行失败、重试策略 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策（PL-001 ~ PL-010）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-17*
