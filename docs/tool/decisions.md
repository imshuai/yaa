# 设计决策

> 文档路径: docs/tool/decisions.md
> 上级: README.md 12-13

---

## 12. 设计决策

### TD-001: Tool 结果是文本而非结构化对象

- **决策**：`ToolResult.Content` 是 `string` 而非 `any`
- **理由**：LLM 的 Function Calling 返回的是文本，Tool 结果最终也是作为文本注入 Context。保持 string 简化了处理链路，结构化数据由 Tool 自行 JSON 序列化

### TD-002: 软错误 vs 硬错误

- **决策**：`ToolResult.IsError` 和 `Execute` 返回的 `error` 分离
- **理由**：软错误（如命令退出码非 0、HTTP 404）是 Tool 的正常返回，LLM 可据此调整策略；硬错误（如超时、权限拒绝）是系统级异常，需要重试或终止

### TD-003: 并发执行而非串行

- **决策**：同一轮多个 Tool Call 并发执行
- **理由**：Tool 之间通常无依赖关系（如同时查天气和查股票），并发可显著降低延迟。有依赖的 Tool 会被 LLM 分到不同轮次

### TD-004: 参数校验在 Manager 层

- **决策**：JSON Schema 校验由 Tool Manager 统一处理，Tool 实现不需要重复校验
- **理由**：减少 Tool 实现的样板代码，保证校验一致性

### TD-005: Tool 不感知 LLM 协议

- **决策**：Tool interface 中没有 LLM 相关概念（如 ToolDef、ToolCall）
- **理由**：Tool 是纯执行单元，与 LLM 协议解耦。Manager 负责 Tool ↔ Provider 的协议转换

### TD-006: 截断在 Manager 层

- **决策**：Tool 结果截断由 Manager 统一处理
- **理由**：Tool 实现可能返回超大结果（如读取大文件），统一截断保证 Context 不会爆炸。Tool 实现可以自行预截断，但 Manager 的截断是最终保障

### TD-007: Context 原子单元

- **决策**：assistant(tool_calls) + tool 消息作为 Context 截断的原子单元
- **理由**：拆分会导致 LLM 看到 tool_calls 但找不到对应结果，产生混乱

### TD-008: 深度思考与 Tool 的配合

- **决策**：有 Tool Call 的轮次，`reasoning_content` 必须保留在 Context 中
- **理由**：DeepSeek 等厂商要求多轮 Tool 调用时必须回传 reasoning_content，否则返回 400 错误。Context Manager 需理解此约束

### TD-009: Config Tool 遵循统一热更新边界

- **决策**：v1 不提供 `config_set`/`config_save`；配置文件由运维方修改，watcher 与 `config_reload` 共用 `ReloadManager`，restart-required 变化只返回结构化结果而不发布候选快照
- **理由**：复用 Config Manager 的单一 Load/diff/Store 契约，避免内存显示新值但底层资源仍使用旧值

### TD-010: 敏感字段自动脱敏

- **决策**：`config_query` 强制调用唯一 `config.RedactedView`，且没有关闭开关；v1 不提供需要内部日志存储的 `log_query`
- **理由**：Tool 结果会注入 LLM Context，`ExecutionScope` 没有独立管理员 principal，任何可关闭脱敏都会绕过边界

### TD-011: v1 不提供 Skill 管理 Tool

- **决策**：不注册 `skill_install`、`skill_uninstall`、`skill_enable` 或 `skill_disable`
- **理由**：Skill Manager 只在启动时构造不可变 snapshot；部署系统修改文件和配置后重启 Runtime

### TD-012: 内视工具使用固定安全视图

- **决策**：v1 不提供 goroutine dump、跨 Agent Context 或任意 Session Tool result 等详细模式；每个 Tool 返回固定、脱敏且有界的 DTO
- **理由**：Tool Manager 只有 Agent/Session execution scope，没有 Auth role。删除敏感模式比引入一条不可执行的“管理员”分支更明确

### TD-013: Config 查询只读

- **决策**：`config_query` 只读取强制脱敏的 Effective Config；不提供 `config_schema`、`config_diff` 或来源层恢复能力
- **理由**：Loader 不保留原始来源层或 Secret 引用；只读 Effective Config 是唯一可稳定实现的视图

### TD-014: Provider alias 是 turn-local wire 投影

- **决策**：Runtime/Session/Remote/MCP 始终使用 canonical Tool name；只有发送 Provider 的请求副本使用 [确定性 Provider-safe alias](provider.md)，并通过 turn-local executable map 精确反查响应
- **理由**：MCP 点分名、Unicode 等合法 canonical name 不能满足所有厂商的 function-name 正则。持久化 alias 会把 Session 绑定到 Provider 和算法版本；让 adapter 各自改名又会产生无法统一反查的多套规则

---



## 13. 模块关系

```text
┌──────────────────────────────────────────────────────────┐
│                        Agent                              │
│                                                           │
│  ┌─────────┐    ┌──────────┐    ┌─────────────┐          │
│  │ Context  │◄──│ Tool Mgr │───►│  Provider   │          │
│  │ Manager  │    │          │    │             │          │
│  └─────────┘    └────┬─────┘    └──────┬──────┘          │
│                      │                  │                 │
│                      │           ┌──────▼──────┐         │
│               ┌──────▼──────┐    │  ToolDef    │         │
│               │ Tool 实例    │    │ ToolCall    │         │
│               │             │    │ ToolChoice  │         │
│               │ 通用执行类   │    └─────────────┘         │
│               │  Shell/HTTP/ │                            │
│               │  File        │                            │
│               │             │                            │
│               │ 配置管理类   │◄──► Config Manager        │
│               │ Query/Reload │                            │
│               │             │                            │
│               │ 内视管理类   │◄──► Runtime / Agent Mgr   │
│               │  Introspect  │◄──► Session Mgr            │
│               │             │◄──► Skill Manager          │
│               │             │◄──► Provider Manager       │
│               │             │◄──► MCP Client              │
│               │             │◄──► Log / Metric Store     │
│               └─────────────┘                            │
└───────────────────────────────────────────────────────────┘

依赖方向:
  Agent → Tool Manager (调用)
  Tool Manager → Tool 实例 (执行)
  Tool Manager → Provider (turn-local ToolDef alias 投影)
  Context Manager ← Tool Manager (结果注入)
  MCP Client → Tool Manager (注册 MCP Tool)
  Config Tools → Config Manager (脱敏查询/统一 reload)
  Introspection Tools → Runtime/Agent/Session/Skill/Provider/MCP (查询状态)
```

**依赖关系：**
- Tool Manager 依赖 Provider 的类型定义（`ToolDef`, `ToolCall`），但不依赖 Provider 实现
- Tool 实例只依赖 Tool interface，不感知 Provider / Context / Agent
- Context Manager 依赖 Provider 的 `Message` 类型，与 Tool Manager 协作处理 Tool 消息
- Provider alias 只存在于投影后的 request 与反查前的 response；Session/Remote/执行路径均为 canonical

---
