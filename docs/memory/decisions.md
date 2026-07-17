# Memory 设计决策

> 文档路径: `docs/memory/decisions.md`
> 上级: `docs/memory/README.md` §设计决策

---

## 设计决策

### MD-001: 三层记忆架构，而非单一存储

**决策：** Memory 采用 Short-term / Long-term / Summary 三层架构，而非单一扁平存储。

**理由：**
- 不同生命周期和访问模式的记忆需要不同的存储策略
- 短期记忆频繁读写、随 Session 结束清除或晋升；长期记忆需要持久化和检索
- 摘要记忆是 Context 压缩的产物，语义上独立于原始消息
- 单一存储会导致检索噪音大、存储膨胀、性能下降

**影响：** `MemoryLayer` 枚举贯穿所有接口，存储后端需按层区分策略。

---

### MD-002: 统一 Memory Interface，屏蔽存储差异

**决策：** 所有记忆操作通过 `Memory` interface 完成，存储后端（SQLite / 向量数据库 / 内存）通过配置切换。

**理由：**
- 用户可能从原型（内存）到生产（SQLite）到规模化（向量数据库）逐步升级
- 接口统一让上层逻辑不关心存储细节
- 符合 Config over Code 原则

**影响：** 存储后端需实现 `Memory` interface，扩展能力通过 `MemoryExtended` 可选接口提供。

---

### MD-003: 检索优先，而非全量加载

**决策：** 长期记忆通过 Search 检索后按需注入 Context，而非全量加载到 Context 窗口。

**理由：**
- Context 窗口是稀缺资源，Token 预算有限
- 长期记忆可能无限增长，全量加载不现实
- 检索方式（语义 / 关键词）能精准匹配当前对话需求

**影响：** Agent Loop 需在构建 Context 时插入 Memory 检索步骤，检索结果作为 System Message 注入。

---

### MD-004: 向量搜索为主，关键词搜索为降级回退

**决策：** 优先使用向量语义搜索（需 Embedder），当向量搜索不可用时回退到关键词搜索。

**理由：**
- 语义搜索能理解"意思相近"的记忆，超越字面匹配
- 但 Embedder 需要额外依赖（模型 / API），并非所有部署环境都具备
- 优雅降级保证基本可用性

**影响：** Memory 实现需检测 Embedder 可用性，`Search` 方法内部自动选择搜索策略。

---

### MD-005: Agent 级记忆隔离，Session 级不独立存储

**决策：** 记忆按 Agent 隔离，每个 Agent 拥有独立 Memory 空间；Session 不拥有独立 Memory，但短期记忆随 Session 生命周期管理。

**理由：**
- Agent 是能力主体，记忆应绑定到 Agent 而非单个对话
- 同一 Agent 的多个 Session 共享长期记忆，实现跨对话连续性
- Session 级独立 Memory 会导致记忆碎片化，难以关联

**影响：** `MemoryManager` 以 `agentID` 为 key 管理 Memory 实例，短期记忆通过 `layer` 字段区分 Session。

---

### MD-006: 短期记忆可晋升为长期记忆

**决策：** 提供 `Promote` 操作，将短期记忆手动或自动晋升为长期记忆。

**理由：**
- 并非所有短期记忆都需要持久化，但也非全部应丢弃
- Session 结束时，重要信息（用户偏好、关键决策）应保留
- 晋升机制是"遗忘"与"记住"之间的可控阀门

**影响：** `MemoryExtended` 接口包含 `Promote` 方法；晋升策略可配置（手动 / 自动 / 混合）。

---

### MD-007: 记忆支持过期与淘汰机制

**决策：** 记忆项支持 `ExpiresAt` 字段，并提供 `Expire` 方法清理过期记忆。

**理由：**
- 长期记忆无限增长会导致存储膨胀和检索性能下降
- 部分记忆有自然过期时间（临时任务、限时上下文）
- 主动淘汰是存储管理的必要手段

**影响：** 存储后端需支持过期清理；可配置定期清理任务或手动触发。

---

### MD-008: 扩展能力通过能力检测模式暴露

**决策：** 核心 `Memory` interface 保持最小化，高级能力（批量、过滤、更新、晋升、计数）通过 `MemoryExtended` 可选接口暴露，调用方通过类型断言检测。

**理由：**
- 最小接口降低实现门槛，简单存储后端只需实现 5 个方法
- 高级能力是"锦上添花"，不应强制所有后端实现
- 能力检测模式是 Go 语言的惯用做法（如 `io.ReadWriter`）

**影响：** 调用方需编写 `if ext, ok := mem.(MemoryExtended); ok { ... }` 防御代码。

---

## 模块关系

```text
┌──────────────────────────────────────────────────────────────┐
│                          Agent                                │
│                                                                │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐    │
│  │ Context Mgr  │◄───│ Memory Mgr   │───►│ Session Mgr  │    │
│  │ (注入检索结果)│    │ (实例管理)    │    │ (短期记忆来源)│    │
│  └──────────────┘    └──────┬───────┘    └──────────────┘    │
│                              │                                 │
│               ┌──────────────┼──────────────┐                │
│               │              │              │                 │
│        ┌──────▼─────┐ ┌──────▼─────┐ ┌─────▼──────┐          │
│        │ Memory     │ │ Memory     │ │ Memory    │          │
│        │ (Agent A)  │ │ (Agent B)  │ │ (Agent C) │          │
│        └──────┬─────┘ └──────┬─────┘ └─────┬────┘          │
│               │              │              │                 │
│        ┌──────▼──────────────▼──────────────▼────┐           │
│        │            Memory Store                  │           │
│        │  ┌──────────┐ ┌──────────┐ ┌──────────┐│           │
│        │  │ SQLite   │ │ Vector   │ │ In-Memory││           │
│        │  │ Store    │ │ Store    │ │ Store    ││           │
│        │  └──────────┘ └──────────┘ └──────────┘│           │
│        └──────────────────┬─────────────────────┘           │
│                           │                                   │
│                    ┌──────▼──────┐                            │
│                    │  Embedder   │ (可选，向量搜索)            │
│                    │  (Provider  │                            │
│                    │   独立配置) │                            │
│                    └─────────────┘                            │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐    │
│  │  Memory Layer                                          │    │
│  │  ┌────────────┐ ┌────────────┐ ┌────────────┐       │    │
│  │  │ Short-term │ │ Long-term  │ │ Summary    │       │    │
│  │  │ (消息历史) │ │ (持久化)   │ │ (压缩摘要) │       │    │
│  │  └────────────┘ └────────────┘ └────────────┘       │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘

依赖方向:
  Agent → Memory Manager (获取 Memory 实例)
  Memory Manager → Memory Store (底层存储，配置切换)
  Memory Manager → Embedder (向量嵌入，可选)
  Context Manager → Memory (检索后注入 Context)
  Session Manager → Memory (短期记忆来源)
  Memory Store → SQLite / Vector DB / In-Memory (后端实现)
  Embedder → Provider (嵌入模型调用)

数据流:
  Session 消息 → Short-term Memory → (Promote) → Long-term Memory
  Long-term Memory → (Search) → Context Manager → LLM
  Context 压缩 → Summary Memory → Storage
```

**依赖关系：**
- Memory Manager 是核心调度者，管理所有 Agent 的 Memory 实例（懒加载）
- Memory Store 是可替换后端，通过配置选择 SQLite / 向量数据库 / 内存
- Embedder 是可选依赖，仅在向量搜索时需要，不可用时自动降级
- Context Manager 是 Memory 的消费者，检索结果通过 System Message 注入
- Session Manager 是短期记忆的来源，Session 关闭时触发晋升或清理
- 三层 Memory（Short-term / Long-term / Summary）共享存储后端，通过 `layer` 字段区分

---

*最后更新: 2025-07-17*
