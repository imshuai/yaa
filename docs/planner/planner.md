# Planner 接口与实现详解

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/planner/planner.md`
> 依赖: `docs/architecture.md` §3.5, `docs/planner/README.md`

---

## 1. 核心结构体定义

### 1.1 Planner 接口

```go
// pkg/planner/planner.go

package planner

import "github.com/imshuai/yaa/pkg/agent"

// Planner 负责将复杂任务分解为可执行步骤。
// 实现可以是 LLM 驱动、静态模板、交互式等。
type Planner interface {
    // Plan 接收原始任务描述和当前 Agent 上下文，
    // 返回一个结构化的执行计划。
    Plan(ctx context.Context, task string, ag *agent.Agent) (*Plan, error)
}
```

**设计要点：**

| 要点 | 说明 |
|------|------|
| 接口最小化 | 仅一个方法，易于实现与替换 |
| Agent 感知 | 传入 `*agent.Agent`，规划时可参考 Agent 能力集 |
| 声明式输出 | 返回 `*Plan`，不执行任何步骤 |
| 可扩展 | 通过组合实现静态规划、交互规划等策略 |

### 1.2 Plan 结构体

```go
// Plan 表示一个执行计划，包含有序的步骤列表。
type Plan struct {
    ID        string      `json:"id"`         // 计划唯一标识（UUID）
    Task      string      `json:"task"`       // 原始任务描述
    Steps     []Step      `json:"steps"`      // 步骤列表（有序）
    Status    PlanStatus  `json:"status"`     // 计划整体状态
    CreatedAt time.Time   `json:"created_at"` // 创建时间
    UpdatedAt time.Time   `json:"updated_at"` // 最后更新时间
    Metadata  map[string]any `json:"metadata,omitempty"` // 扩展元数据
}
```

### 1.3 Step 结构体

```go
// Step 表示计划中的一个执行步骤。
type Step struct {
    ID        string            `json:"id"`        // 步骤唯一标识（如 "s1", "s2"）
    Action    string            `json:"action"`    // 动作类型: "tool" / "skill" / "llm"
    Target    string            `json:"target"`    // 目标名称: Tool 名 / Skill 名 / 空表示纯 LLM
    Input     map[string]any    `json:"input"`     // 步骤输入参数
    Depends   []string          `json:"depends"`   // 依赖的前置步骤 ID 列表
    Status    StepStatus        `json:"status"`    // 步骤状态
    Output    map[string]any    `json:"output,omitempty"`  // 执行结果
    Error     string            `json:"error,omitempty"`   // 错误信息（失败时）
    StartedAt *time.Time        `json:"started_at,omitempty"` // 开始执行时间
    EndedAt   *time.Time        `json:"ended_at,omitempty"`   // 完成或失败时间
}
```

**Action 类型说明：**

| Action | Target | 说明 |
|--------|--------|------|
| `tool` | Tool 名称 | 调用注册的 Tool，如 `web_search` |
| `skill` | Skill 名称 | 调用注册的 Skill，如 `music-download` |
| `llm` | 空 | 纯 LLM 推理，不调用外部工具 |

### 1.4 状态枚举

```go
type PlanStatus string

const (
    PlanPending   PlanStatus = "pending"   // 计划已创建，尚未执行
    PlanRunning   PlanStatus = "running"   // 计划执行中
    PlanCompleted PlanStatus = "completed" // 所有步骤成功完成
    PlanFailed    PlanStatus = "failed"    // 某步骤失败且无法继续
)

type StepStatus string

const (
    StepPending StepStatus = "pending"  // 等待依赖完成
    StepRunning StepStatus = "running"  // 执行中
    StepDone    StepStatus = "done"     // 成功完成
    StepFailed  StepStatus = "failed"   // 执行失败
    StepSkipped StepStatus = "skipped"  // 因依赖失败而跳过
)
```

### 1.5 状态流转图

```text
Plan 状态流转:

  ┌─────────┐     ┌─────────┐     ┌──────────┐
  │ Pending │────▶│ Running │────▶│Completed │
  └─────────┘     └────┬────┘     └──────────┘
                       │
                       ▼
                  ┌─────────┐
                  │  Failed │
                  └─────────┘

Step 状态流转:

  ┌─────────┐     ┌─────────┐
  │ Pending │────▶│ Running │
  └────┬────┘     └────┬────┘
       │               │
       │               ├────▶ ┌──────┐
       │               │      │ Done │
       │               │      └──────┘
       │               │
       │               ├────▶ ┌────────┐
       │               │      │ Failed │
       │               │      └────────┘
       │               │
       └───────────────┴────▶ ┌─────────┐
                               │ Skipped │  (依赖失败时)
                               └─────────┘
```

---

## 2. LLM 驱动规划器实现

### 2.1 实现概览

```go
// pkg/planner/llm_planner.go

package planner

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"

    "github.com/imshuai/yaa/pkg/agent"
    "github.com/imshuai/yaa/pkg/provider"
)

// LLMPlanner 是默认的 LLM 驱动规划器。
// 通过构造规划 Prompt，让 LLM 输出结构化的 Plan JSON。
type LLMPlanner struct {
    provider provider.Provider // 用于规划的 LLM Provider
    model    string            // 使用的模型（如 "gpt-4o"）
    prompt   *PromptBuilder    // Prompt 构造器
}

// NewLLMPlanner 创建 LLM 驱动规划器。
func NewLLMPlanner(p provider.Provider, model string) *LLMPlanner {
    return &LLMPlanner{
        provider: p,
        model:    model,
        prompt:   NewPromptBuilder(),
    }
}
```

### 2.2 Plan 方法实现

```go
// Plan 实现 Planner 接口。
func (lp *LLMPlanner) Plan(ctx context.Context, task string, ag *agent.Agent) (*Plan, error) {
    // 1. 构建规划 Prompt
    sysPrompt := lp.prompt.BuildSystemPrompt(ag)
    userPrompt := lp.prompt.BuildUserPrompt(task, ag)

    // 2. 调用 LLM
    req := &provider.ChatRequest{
        Model:       lp.model,
        Temperature: 0.2, // 低温度，确保规划稳定
        Messages: []provider.Message{
            {Role: "system", Content: sysPrompt},
            {Role: "user",   Content: userPrompt},
        },
    }

    resp, err := lp.provider.Chat(context.Background(), req)
    if err != nil {
        return nil, fmt.Errorf("planner: LLM call failed: %w", err)
    }

    // 3. 解析 LLM 输出为 Plan
    plan, err := parsePlanResponse(resp.Content, task)
    if err != nil {
        return nil, fmt.Errorf("planner: parse plan failed: %w", err)
    }

    return plan, nil
}
```

### 2.3 Plan 解析

```go
// parsePlanResponse 将 LLM 输出的 JSON 解析为 Plan 结构。
func parsePlanResponse(rawJSON, task string) (*Plan, error) {
    // LLM 可能输出 ```json ... ``` 包裹的内容，需提取
    jsonStr := extractJSON(rawJSON)

    var raw struct {
        Steps []Step `json:"steps"`
    }
    if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
        return nil, err
    }

    // 校验步骤 ID 唯一性与依赖有效性
    if err := validateSteps(raw.Steps); err != nil {
        return nil, err
    }

    return &Plan{
        ID:        uuid.NewString(),
        Task:      task,
        Steps:     raw.Steps,
        Status:    PlanPending,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }, nil
}

// validateSteps 校验步骤 ID 唯一性及依赖引用有效性。
func validateSteps(steps []Step) error {
    ids := make(map[string]bool, len(steps))
    for i := range steps {
        s := &steps[i]
        if s.ID == "" {
            return fmt.Errorf("step %d: empty ID", i)
        }
        if ids[s.ID] {
            return fmt.Errorf("step %d: duplicate ID %q", i, s.ID)
        }
        ids[s.ID] = true
        for _, dep := range s.Depends {
            if !ids[dep] {
                return fmt.Errorf("step %q: depends on unknown step %q", s.ID, dep)
            }
        }
    }
    return nil
}
```

---

## 3. 规划 Prompt 设计

### 3.1 System Prompt

```text
You are a task planner for an AI Agent runtime called Yaa!.

Your job: decompose a complex task into a series of structured,
executable steps. Each step must specify:
- id: unique step identifier (e.g. "s1", "s2")
- action: one of "tool", "skill", "llm"
- target: tool name, skill name, or empty (for "llm")
- input: parameters as key-value pairs
- depends: list of prerequisite step IDs (empty if none)

Rules:
1. Keep plans minimal — prefer fewer, well-defined steps.
2. Declare dependencies explicitly to enable parallel execution.
3. Use "llm" action for reasoning, summarization, or synthesis.
4. Use "tool" action for concrete operations (web search, file I/O, etc.).
5. Use "skill" action for complex multi-step workflows.
6. Output ONLY valid JSON, no explanation.

Output format:
{
  "steps": [
    {
      "id": "s1",
      "action": "tool",
      "target": "web_search",
      "input": {"query": "..."},
      "depends": []
    }
  ]
}
```

### 3.2 User Prompt 构造

```go
// PromptBuilder 负责构造规划 Prompt。
type PromptBuilder struct{}

func (pb *PromptBuilder) BuildUserPrompt(task string, ag *agent.Agent) string {
    var sb strings.Builder
    sb.WriteString("## Task\n")
    sb.WriteString(task)
    sb.WriteString("\n\n## Available Tools\n")
    for _, name := range ag.Tools {
        sb.WriteString(fmt.Sprintf("- %s\n", name))
    }
    sb.WriteString("\n## Available Skills\n")
    for _, name := range ag.Skills {
        sb.WriteString(fmt.Sprintf("- %s\n", name))
    }
    sb.WriteString("\nGenerate a plan in JSON format.")
    return sb.String()
}
```

### 3.3 Prompt 调优参数

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| Temperature | 0.1 – 0.3 | 低温度保证规划稳定性 |
| Top P | 0.9 | 适度采样，避免退化 |
| Max Tokens | 2048 | 规划输出通常不超过 2K tokens |
| Response Format | JSON | 强制 LLM 输出 JSON（如支持） |

---

## 4. 规划执行流程

```text
用户输入: "搜索 Go 1.25 新特性，总结后保存到文件"
  │
  ▼
┌──────────────────────────────────────────┐
│           LLMPlanner.Plan()              │
│                                          │
│  1. 构建 Prompt (任务 + Agent 能力)       │
│  2. 调用 LLM (temperature=0.2)          │
│  3. 解析 JSON → Plan{Steps}             │
│  4. 校验 ID 唯一性 + 依赖有效性           │
│  5. 返回 Plan (Status=Pending)          │
└──────────────────┬───────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│  Plan {                                  │
│    Steps: [                              │
│      s1: tool/web_search  (depends: []) │
│      s2: llm/summarize    (depends: [s1])│
│      s3: tool/file_write  (depends: [s2])│
│    ]                                    │
│  }                                      │
└──────────────────┬───────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────┐
│         Executor 执行 Plan               │
│                                          │
│  s1 [Pending→Running→Done]  ← 可并行     │
│  s2 [Pending→Running→Done]  ← 等 s1     │
│  s3 [Pending→Running→Done]  ← 等 s2     │
│                                          │
│  Plan.Status → Completed                 │
└──────────────────────────────────────────┘
```

---

## 5. 使用示例

### 5.1 创建并使用 Planner

```go
func exampleUsage() {
    // 初始化 Provider
    openaiProvider := provider.NewOpenAI(cfg)

    // 创建 LLM 规划器
    planner := planner.NewLLMPlanner(openaiProvider, "gpt-4o")

    // 获取 Agent 实例（假设从 Agent Manager 获取）
    ag := agentManager.Get("default")

    // 规划任务
    task := "搜索 Go 1.25 新特性，总结后保存到文件"
    plan, err := planner.Plan(ctx, task, ag)
    if err != nil {
        log.Fatal("plan failed: ", err)
    }

    // 打印计划
    fmt.Printf("Plan: %s\n", plan.ID)
    for _, step := range plan.Steps {
        fmt.Printf("  [%s] %s/%s → depends: %v\n",
            step.ID, step.Action, step.Target, step.Depends)
    }

    // 交给 Executor 执行（见 executor.md）
    // err = executor.Execute(plan, ag)
}
```

### 5.2 自定义 Planner 实现

```go
// StaticPlanner 是一个基于预定义模板的静态规划器。
type StaticPlanner struct {
    templates map[string]*Plan // 任务关键词 → 预定义 Plan
}

func (sp *StaticPlanner) Plan(ctx context.Context, task string, ag *agent.Agent) (*Plan, error) {
    for keyword, plan := range sp.templates {
        if strings.Contains(task, keyword) {
            // 深拷贝 Plan，避免修改模板
            return clonePlan(plan, task), nil
        }
    }
    return nil, fmt.Errorf("no matching template for task: %s", task)
}
```

---

## 6. 设计决策

> 完整的设计决策（含模块关系图）见 [decisions.md](decisions.md)。

关键决策摘要：

| 编号 | 决策 | 说明 |
|------|------|------|
| PD-001 | Planner 作为独立层 | 不嵌入 Agent，可独立替换 |
| PD-002 | LLM 驱动规划 | 通用性强，适配任意任务类型 |
| PD-005 | Plan 挂载到 Session | 支持跨轮次执行和崩溃恢复 |
| PD-006 | DAG 依赖 | 支持并行执行 |
| PD-007 | 接口仅含 `Plan` 方法 | 规划与执行分离 |
| PD-009 | `StepSkipped` 状态 | 区分依赖失败与自身失败 |

---

*最后更新: 2025-07-17*
