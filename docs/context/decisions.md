# Context 设计决策

> 文档路径: `docs/context/decisions.md`
> 上级: `docs/context/README.md` §9

---

## 9. 设计决策

### CX-001: Context Manager 作为独立层，不内嵌于 Session

**决策：** Context 管理作为独立模块存在，而非 Session 的内部逻辑。

**理由：**
- Context 构建涉及多源聚合（System Prompt、历史消息、Memory 摘要、Tool 结果、Skill Prompt），逻辑复杂
- 独立后可单独测试、替换策略、扩展
- 不同 Provider 的 Token 限制差异大，策略需灵活切换
- 与 Session 解耦后，Context 模块可被 Planner 或其他模块复用

**影响：** Context Manager 通过接口暴露 `Build()` / `Compress()` / `Truncate()`，Session 调用但不持有实现细节。

---

### CX-002: Token 计算委托给 Provider，不自行实现分词器

**决策：** Token 计数由 Provider 层提供估算能力，Context Manager 不内置分词器。

**理由：**
- 不同 LLM 使用不同分词器（BPE、SentencePiece、tiktoken…），自行实现维护成本极高
- Provider 层最了解自身模型的 Token 规则
- 精确计数非必需，估算值足够驱动截断/压缩决策
- 零外部依赖原则，避免引入 C 绑定的分词库

**影响：** Provider 接口需暴露 `EstimateTokens(text string) int` 方法；Context Manager 在构建时调用 Provider 估算。

---

### CX-003: 默认策略为滑动窗口 + 摘要压缩，可插拔扩展

**决策：** 内置两种策略——滑动窗口（Sliding Window）和摘要压缩（Summary Compression），通过接口可插拔扩展自定义策略。

**理由：**
- 滑动窗口实现简单、行为可预测，适合大多数场景
- 摘要压缩能保留早期对话的语义，弥补窗口截断的信息丢失
- 不同场景需要不同策略（代码场景保留完整代码块、客服场景保留摘要）
- 接口化策略允许用户按 Agent 粒度配置

**影响：** 定义 `Strategy` 接口，Manager 持有策略实例列表，按优先级链式执行。

---

### CX-004: System Prompt 永不截断，优先级最高

**决策：** System Prompt（含 Skill Prompt）享有最高保留优先级，在任何截断/压缩策略中都不被移除。

**理由：**
- System Prompt 定义 Agent 身份、行为约束和技能指令，丢失会导致行为偏移
- Skill Prompt 是 Agent 当前执行任务的关键指引，移除后无法继续
- 实际场景中 System Prompt 通常远小于 Token 限制，不会成为瓶颈

**影响：** 截断策略从 User/Assistant 消息开始裁剪，System Message 始终保留在 Context 头部。

---

### CX-005: Tool 结果按角色分级保留

**决策：** Tool Call 与 Tool Result 消息的保留优先级低于用户消息，高于普通助手消息。

**理由：**
- Tool 结果通常体积大（文件内容、命令输出），是 Context 膨胀的主要来源
- 用户消息包含核心意图，丢失后对话方向迷失
- 助手消息中的推理过程可被摘要替代，但 Tool 结果的原始数据有时仍需引用
- 分级保留在截断时提供更细粒度的控制

**影响：** 截断顺序：助手消息 → Tool 结果 → 用户消息 → System Prompt（不可截断）。

---

### CX-006: 压缩使用 LLM 异步执行，不阻塞主对话流

**决策：** 摘要压缩通过调用 LLM 异步生成，不阻塞当前对话轮次；压缩完成前使用截断策略兜底。

**理由：**
- LLM 压缩需要一次额外 API 调用，耗时不可忽略
- 阻塞主流程会让用户等待无意义的时间
- 截断策略可立即生效，保证当前轮次正常发送
- 压缩结果缓存后，后续轮次直接使用压缩后的 Context

**影响：** Manager 需要异步压缩任务队列和缓存机制；首次触发压缩时降级为截断。

---

### CX-007: Context 构建结果缓存，按消息指纹命中

**决策：** 构建后的 Context 以 Session 最后一条消息的 ID 为指纹缓存，相同指纹直接复用。

**理由：**
- Agent Loop 中 LLM 返回 Tool Call 后需要重新构建 Context，但消息历史未变（仅追加了 Tool 结果）
- 避免重复计算 Token 和重复执行截断策略
- 缓存粒度足够细，Session 中任何新消息都会使缓存失效

**影响：** Manager 维护 `sessionID → (lastMessageID, *Context)` 缓存；缓存失效逻辑简单可靠。

---

### CX-008: 支持 Provider 级别的 Token 预算配置

**决策：** 每个 Provider 可配置 `context_budget`（预留 Token 给输出），Context Manager 构建时读取目标 Provider 的预算值。

**理由：**
- 不同模型的最大 Context 差异巨大（4K ~ 200K+）
- 输出 Token 需要预留空间，不能把全部额度用于输入
- 同一 Agent 切换 Provider 时，Context 策略应自动适配
- 保守默认值（如预算 = 最大 Context × 0.75）保证安全

**影响：** Provider 配置新增 `context_budget` 字段；Manager 在 Build 时获取预算并据此截断。

---

## 10. 模块关系

```text
┌──────────────────────────────────────────────────────────────┐
│                          Agent                                │
│                                                               │
│  ┌────────────┐    ┌──────────────────┐    ┌──────────────┐  │
│  │  Session    │───►│  Context Manager  │───►│  Provider    │  │
│  │  Manager    │    │                  │    │  (Token 估算) │  │
│  │             │    │  Build()         │    └──────────────┘  │
│  │  消息历史    │    │  Compress()      │                      │
│  │  状态管理    │    │  Truncate()      │    ┌──────────────┐  │
│  └────────────┘    └────────┬─────────┘    │  Memory      │  │
│                             │               │  (摘要注入)   │  │
│                    ┌────────▼────────┐      └──────┬───────┘  │
│                    │  Strategy Chain  │◄────────────┘          │
│                    │                  │                        │
│                    │  SlidingWindow   │    ┌──────────────┐   │
│                    │  SummaryCompress │◄───│  Skill Mgr    │   │
│                    │  (可扩展自定义)    │    │  (Prompt 注入) │   │
│                    └──────────────────┘    └──────────────┘   │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐    │
│  │  Context 构成（优先级从高到低）                          │    │
│  │                                                       │    │
│  │  System Prompt  ← 不可截断                              │    │
│  │  Skill Prompt   ← 不可截断                              │    │
│  │  Memory 摘要    ← 高优先级保留                           │    │
│  │  用户消息       ← 高优先级保留                           │    │
│  │  Tool 结果      ← 中优先级保留                           │    │
│  │  助手消息       ← 低优先级（首选截断）                    │    │
│  └──────────────────────────────────────────────────────┘    │
│                                                               │
│  ┌──────────────────────────────────────────────────────┐    │
│  │  缓存层                                                │    │
│  │  sessionID → (lastMessageID, *Context)                │    │
│  │  命中: 直接复用 · 未命中: 重新构建                       │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘

依赖方向:
  Session Manager → Context Manager (构建 Context)
  Context Manager → Provider (Token 估算、预算查询)
  Context Manager → Memory (摘要获取)
  Context Manager → Skill Manager (Skill Prompt 注入)
  Context Manager → Strategy Chain (截断/压缩执行)
  Agent → Context Manager (每轮对话前构建)
  Agent Loop → Context Manager (Tool Call 后重建)
  异步压缩 → Provider (LLM 摘要调用)
```

**依赖关系：**
- Context Manager 依赖 Provider（Token 估算、预算配置、异步压缩调用）
- Context Manager 依赖 Memory（获取长期摘要注入 Context）
- Context Manager 依赖 Skill Manager（获取已激活 Skill 的 Prompt）
- Context Manager 不依赖 Session Manager（由 Session 主动调用，反向无依赖）
- Strategy 接口无外部依赖，纯算法逻辑，可独立测试
- 缓存层为 Manager 内部实现，不依赖外部组件

---

*最后更新: 2025-07-17*
