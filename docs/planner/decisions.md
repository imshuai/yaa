# Planner 设计决策

> 文档路径: `docs/planner/decisions.md`
> 上级: `docs/planner/README.md`

---

## 设计决策

> 完整的设计决策列表，包含架构层面和实现层面的决策。

### PD-001: Planner 作为独立层，不嵌入 Agent

**决策：** Planner 作为 Runtime 的独立模块层存在，而非 Agent 内部逻辑。

**理由：**
- 规划与执行是不同关注点，分离后各自可独立演进
- 支持多种规划器实现（LLM 驱动、规则驱动、混合）
- 测试时可单独 Mock Planner 而不影响 Agent 逻辑

**影响：** Agent 通过接口调用 Planner，Planner 不反向依赖 Agent 实现。

---

### PD-002: 默认使用 LLM 驱动规划

**决策：** 默认规划器通过 Prompt 让 LLM 输出结构化执行计划，而非使用硬编码规则。

**理由：**
- LLM 具备通用推理能力，适应多样化任务
- 规则驱动方式需要为每种任务编写规则，不可扩展
- LLM 规划可与 Agent Loop 中的 Tool 调用自然衔接

**影响：** 规划器依赖 Provider 可用性；需要处理 LLM 输出解析失败的情况。

---

### PD-003: Plan 以 Step 为最小执行单元

**决策：** Plan 由有序的 Step 组成，每个 Step 是最小执行单元（Tool 调用 / Skill 触发 / LLM 推理）。

**理由：**
- Step 粒度清晰，便于状态追踪和恢复
- Step 间可声明依赖关系，支持非线性执行
- 粗粒度比细粒度更稳定，减少 LLM 规划出错的概率

**影响：** Step 执行失败时需要决定是否终止整个 Plan。

---

### PD-004: 支持自动跳过规划

**决策：** 当任务被判定为简单任务时，自动跳过 Planner，直接进入 Agent Loop。

**理由：**
- 简单问答不需要规划，跳过可降低延迟
- 规划本身消耗 LLM 调用（成本和时间）
- `auto_skip` 配置让用户可控

**影响：** 需要实现简单任务检测逻辑（基于任务长度、关键词等启发式规则）。

---

### PD-005: Plan 挂载到 Session 支持恢复

**决策：** 生成的 Plan 挂载到 Session 而非 Agent，支持跨轮次执行和崩溃恢复。

**理由：**
- Session 是状态隔离的基本单位，Plan 属于会话状态
- Runtime 重启后可从 Session 恢复未完成的 Plan
- 多轮对话中可继续执行或修改 Plan

**影响：** Session 需要支持 Plan 的持久化与恢复逻辑。

---

### PD-006: Step 依赖使用 DAG 而非线性列表

**决策：** Step 之间通过 `Depends []string` 声明依赖，形成有向无环图（DAG），而非严格线性顺序。

**理由：**
- 某些步骤可并行执行，提高效率
- 现实任务不总是线性的（如"先查 A 和 B，再合并"）
- DAG 表达力更强，线性列表是 DAG 的特例

**影响：** 需要实现 DAG 依赖检查和循环检测；执行引擎需支持拓扑排序。

---

### PD-007: Planner 接口仅含 `Plan` 方法

**决策：** Planner 接口仅定义 `Plan(ctx, task, agent)` 方法，执行职责分离到 Executor。

**理由：**
- 最小接口原则，降低实现门槛
- 规划与执行是不同关注点，应分离
- Executor 可独立替换或测试

**影响：** 需要单独的 Executor 组件负责 Plan 执行。

---

### PD-008: Step 使用 `map[string]any` 作为 Input

**决策：** Step 的 Input 字段类型为 `map[string]any`，兼容不同 Tool/Skill 的异构参数。

**理由：**
- 不同 Tool/Skill 的参数结构差异大，强类型约束会限制灵活性
- `map[string]any` 与 JSON 天然兼容，便于 LLM 输出解析

**影响：** 参数校验延迟到执行阶段，由 Tool 的 JSON Schema 校验。

---

### PD-009: 新增 `StepSkipped` 状态

**决策：** Step 状态枚举新增 `Skipped`，用于依赖失败时明确标记，区别于 `Failed`。

**理由：**
- `Failed` 表示步骤自身执行失败，`Skipped` 表示因上游失败而未执行
- 区分两者有助于错误诊断和恢复策略

**影响：** 状态机需处理 `Skipped` 状态的转换。

---

### PD-010: Plan 携带 Metadata

**决策：** Plan 结构体包含 `Metadata map[string]any` 字段，支持扩展信息。

**理由：**
- 扩展字段（如重试次数、规划耗时、规划策略）不需要修改核心结构
- Metadata 透传，核心层不解析其内容

**影响：** Plan 的序列化/反序列化需处理 Metadata。

---

## 模块关系图

```text
┌──────────────────────────────────────────────────────────────┐
│                           Agent                                │
│                                                                │
│  ┌──────────┐    ┌────────────┐    ┌─────────────────┐       │
│  │ Session   │◄───│  Planner   │───►│  Tool Manager    │       │
│  │ (Plan)   │    │            │    │                  │       │
│  └──────────┘    └─────┬──────┘    └────────┬─────────┘       │
│                        │                     │                  │
│               ┌────────▼────────┐    ┌──────▼──────┐          │
│               │  LLM Planner     │    │ Tool 实例    │          │
│               │  (默认实现)       │    │              │          │
│               │  Prompt → Plan   │    │ shell/http/ │          │
│               └────────┬────────┘    │ file/...     │          │
│                        │             └──────────────┘          │
│               ┌────────▼────────┐                              │
│               │  Provider        │                              │
│               │  (LLM 调用)      │    ┌──────────────┐          │
│               └─────────────────┘    │ Skill Manager│          │
│                                     │              │          │
│               ┌─────────────────┐    │ (Step 可     │          │
│               │  Rule Planner    │    │  触发 Skill) │          │
│               │  (可选实现)       │    └──────────────┘          │
│               └─────────────────┘                              │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐     │
│  │  Context Manager                                       │     │
│  │  (Plan 摘要注入)                                       │     │
│  └──────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────┘

依赖方向:
  Agent → Planner (调用 Plan 生成计划)
  Planner → Provider (LLM 驱动规划器调用 LLM)
  Planner → Tool Manager (Step 中引用 Tool)
  Planner → Skill Manager (Step 中引用 Skill)
  Planner → Session (Plan 挂载到 Session)
  Planner → Context Manager (Plan 摘要注入)
```

**依赖关系：**
- Planner 依赖 Provider（LLM 驱动模式下），但不直接依赖 Agent 实现
- Planner 不管理 Tool/Skill 的生命周期，仅在 Step 中引用
- Session 提供 Plan 的持久化能力，Planner 不直接操作 Storage
- Context Manager 接收 Planner 的摘要输出，但 Planner 不控制 Context 截断策略
