# 设计决策

> 文档路径: docs/tool/decisions.md
> 上级: README.md 12-13

---

## 12. 设计决策

### PD-001: Tool 结果是文本而非结构化对象

- **决策**：`ToolResult.Content` 是 `string` 而非 `any`
- **理由**：LLM 的 Function Calling 返回的是文本，Tool 结果最终也是作为文本注入 Context。保持 string 简化了处理链路，结构化数据由 Tool 自行 JSON 序列化

### PD-002: 软错误 vs 硬错误

- **决策**：`ToolResult.IsError` 和 `Execute` 返回的 `error` 分离
- **理由**：软错误（如命令退出码非 0、HTTP 404）是 Tool 的正常返回，LLM 可据此调整策略；硬错误（如超时、权限拒绝）是系统级异常，需要重试或终止

### PD-003: 并发执行而非串行

- **决策**：同一轮多个 Tool Call 并发执行
- **理由**：Tool 之间通常无依赖关系（如同时查天气和查股票），并发可显著降低延迟。有依赖的 Tool 会被 LLM 分到不同轮次

### PD-004: 参数校验在 Manager 层

- **决策**：JSON Schema 校验由 Tool Manager 统一处理，Tool 实现不需要重复校验
- **理由**：减少 Tool 实现的样板代码，保证校验一致性

### PD-005: Tool 不感知 LLM 协议

- **决策**：Tool interface 中没有 LLM 相关概念（如 ToolDef、ToolCall）
- **理由**：Tool 是纯执行单元，与 LLM 协议解耦。Manager 负责 Tool ↔ Provider 的协议转换

### PD-006: 截断在 Manager 层

- **决策**：Tool 结果截断由 Manager 统一处理
- **理由**：Tool 实现可能返回超大结果（如读取大文件），统一截断保证 Context 不会爆炸。Tool 实现可以自行预截断，但 Manager 的截断是最终保障

### PD-007: Context 原子单元

- **决策**：assistant(tool_calls) + tool 消息作为 Context 截断的原子单元
- **理由**：拆分会导致 LLM 看到 tool_calls 但找不到对应结果，产生混乱

### PD-008: 深度思考与 Tool 的配合

- **决策**：有 Tool Call 的轮次，`reasoning_content` 必须保留在 Context 中
- **理由**：DeepSeek 等厂商要求多轮 Tool 调用时必须回传 reasoning_content，否则返回 400 错误。Context Manager 需理解此约束

### PD-009: Config Tool 运行时生效而非重启

- **决策**：`config_set` 修改立即在内存层生效，通过 `OnChange` 回调传播到相关组件
- **理由**：Agent Runtime 的核心价值是长期运行、无需重启。配置变更应实时生效，`config_reload` 和 `config_save` 提供磁盘层面的同步

### PD-010: 敏感字段自动脱敏

- **决策**：`config_query` 和 `log_query` 默认对 `api_key`、`password`、`secret`、`token` 等字段脱敏
- **理由**：Tool 结果会注入 LLM Context，存在信息泄露风险。脱敏是默认安全行为，可通过 `redact_secrets=false` 显式关闭（需管理权限）

### PD-011: 管理类 Tool 默认禁用

- **决策**：`skill_install`、`skill_uninstall`、`skill_enable`、`skill_disable` 默认 `enabled=false`
- **理由**：这些 Tool 会改变运行时结构（安装/卸载/绑定 Skill），风险较高。只读内视工具默认启用，写入/管理工具默认禁用，由用户显式开启

### PD-012: 内视工具分级权限

- **决策**：内视工具的"详细模式"（如 `runtime_status.detail=full`、`agent_inspect.include_context=true`、`session_inspect.include_tool_results=true`）需要更高权限
- **理由**：摘要信息对调试有帮助且无风险，但完整上下文、Goroutine dump、Tool 结果可能包含敏感数据。分级权限平衡了可用性和安全性

### PD-013: config_diff 作为运维辅助

- **决策**：`config_diff` 作为独立 Tool 而非 `config_save` 的参数
- **理由**：diff 是只读操作，可安全地给 Agent 使用；save 是写操作，需更高权限。分离后 Agent 可以随时 diff 但不能随意 save

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
│               │  Config×6    │                            │
│               │             │                            │
│               │ 内视管理类   │◄──► Runtime / Agent Mgr   │
│               │  Introspect  │◄──► Session Mgr            │
│               │  + Admin     │◄──► Skill Manager          │
│               │             │◄──► Provider Registry      │
│               │             │◄──► MCP Client              │
│               │             │◄──► Log / Metric Store     │
│               └─────────────┘                            │
└───────────────────────────────────────────────────────────┘

依赖方向:
  Agent → Tool Manager (调用)
  Tool Manager → Tool 实例 (执行)
  Tool Manager → Provider (ToolDef 转换)
  Context Manager ← Tool Manager (结果注入)
  MCP Client → Tool Manager (注册 MCP Tool)
  Config Tools → Config Manager (读写配置)
  Introspection Tools → Runtime/Agent/Session/Skill/Provider/MCP (查询状态)
  Admin Tools → Skill Manager / Provider Registry (管理操作)
```

**依赖关系：**
- Tool Manager 依赖 Provider 的类型定义（`ToolDef`, `ToolCall`），但不依赖 Provider 实现
- Tool 实例只依赖 Tool interface，不感知 Provider / Context / Agent
- Context Manager 依赖 Provider 的 `Message` 类型，与 Tool Manager 协作处理 Tool 消息

---

