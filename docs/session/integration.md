# Session 集成

> 上级: [Session 系统设计](README.md)
> 相关模块: [Context](../context/README.md)、[Memory](../memory/README.md)、[Tool](../tool/README.md)、[Provider](../provider.md)

---

## 1. 依赖方向

```text
Remote API ──► Agent/Session Manager ──► Storage
                       │
                       ├──► Memory（可选读取/显式写入）
                       ├──► Context Manager ──► Provider
                       └──► Tool Manager ───────┘
```

Session Manager 不依赖 Provider、Context、Tool 或 Memory 的实现；它只保存 Provider 的统一消息值，并提供状态、历史、持久化和串行边界。

## 2. Agent 完整 turn

完整 Provider/Tool/stream 状态机只在 [Agent 执行契约](../agent.md#4-唯一-turn-流程) 定义，Session 不维护第二份伪实现。边界固定为：Agent 调用

```go
sessions.RunTurn(ctx, sessionID, turnID, onQueued,
    func(turnCtx context.Context, turn *session.Turn) error {
        return a.runTurn(turnCtx, turn, request)
    })
```

callback 的首个写操作必须是 `turn.AppendUser`；后续 `Turn.Append` 自动使用相同 Turn ID。Provider、Memory、Context、Planner 和 Tool 都只接收 `turnCtx`。实现必须保证：

- user 消息成功写入后，即使 Provider 因取消失败，也保留该已接受输入；
- Provider 的 delta 不直接写 Session；只有完整 final assistant 或完整 Tool unit 才能追加；
- Agent 先以 canonical history 组装请求，再用冻结的 [Provider Tool projection](../tool/provider.md) 投影 alias；`Context.Build` 只处理已投影 request 副本，`built.Request.Messages` 不回写 Session，estimator 看到真实 wire alias；
- Tool Manager 使用 `ExecuteBatch(turnCtx, tool.ExecutionScope{AgentID: a.id, SessionID: sessionID}, calls)`；result 按原始 ToolCalls 顺序组成一批，失败 Tool 也生成合法 `role=tool` result；
- Agent 不在 Close、每条消息或 Restore 中隐式调用 Memory 摘要。

`Turn.AppendUser/Append` 使用 RunTurn 派生的 context 并直接在当前 runner task 内提交，不会再次进入队列。callback 不得把 `Turn` 传给其他 goroutine或保存到返回之后。

## 3. Context 集成

Context 输入是 Agent 已完成 alias 投影的完整 `provider.ChatRequest`，至少包含：

- Agent/Skill 注入的 system messages（只存在于请求副本）；
- Memory 返回的已选 items（只存在于请求副本）；
- Session 的历史 `Payload`；
- 当前 user、Tool definitions、ToolChoice、ResponseFormat、Extra 等完整请求字段。

Context 按目标 Model 的窗口和 `ContextConfig` 执行 hybrid/truncate/reject。被裁剪或摘要的消息绝不写回 Session；同一 turn 的 assistant Tool call 与 results 仍由 Session 作为不可拆分 unit 保存。

## 4. Memory 集成

Memory v1 是 Agent-scoped long-term store；Session 历史本身就是 short-term source of truth，不再维护未定义的 Session summary cache。

| 时机 | 行为 | 失败处理 |
|------|------|----------|
| Build 前 | Agent 按 AgentID、可选 SessionID 读取 Memory items | 返回 Memory 错误，未生成 assistant |
| 用户明确保存 | Agent 调用 Memory `Put` | 按 Memory 契约报告错误 |
| Close / Restore | 不自动摘要或 promote | 无隐式副作用 |
| Context 返回 | 注入 request 副本 | 不追加 system 到 Session |

Memory item 若带 SessionID，只是检索 scope；Session 删除不自动删除 Agent Memory，清理由 Memory API 明确执行。

## 5. Tool 集成

1. Agent 根据 Effective Tool 权限和 Session canonical history 创建一次冻结 `ProviderToolProjection`；definitions 只含当前 enabled/authorized Tool，history-only 名称只用于回传历史。
2. Agent 深拷贝并投影完整请求（含历史 ToolCalls、tool `Name` 和 `specific` ToolChoice）后才调用 Context；Context 将 alias schema 纳入 token 估算，不持有映射。
3. Provider 返回 assistant ToolCalls 后，Agent 对 direct/stream 使用同一精确 alias 反查；unknown/非法/history-only alias 返回 `ErrAgentProviderProtocol`，不执行、不发 `tool_call`、不提交 partial。
4. 只有 canonical ToolCalls 才交给 Tool Manager 校验权限和参数并执行；Agent 将 canonical assistant ToolCalls 与每个 `role=tool` result 一次性传给 `Session.Append`。
5. 成功提交后复用本 turn projection，重新构建并投影下一轮 Context，再次调用 Provider。

Tool Manager 不直接修改 Session 内部 slice，也不把被截断的结果写成另一种消息类型。

## 6. Remote API 集成

资源管理端点定义在 [remote-api/session.md](../remote-api/session.md)，对话端点定义在 [remote-api/conversation.md](../remote-api/conversation.md)。Handler 只做认证、参数解码、Agent/Session 归属检查和错误映射；状态转换、容量、消息序列、持久化和事件由 Manager 负责。

- `DELETE /sessions/:id` 是物理删除；`POST /close` 才是关闭。
- `POST /pause`、`POST /resume` 明确驱动状态机。
- `GET /messages` 只读取已提交 snapshot。
- `/events` 可转发 Session mutation 事件；流式 token 使用 conversation frame。
- 不提供 Session 级 `/context/compress`，因为 Context 视图是每次请求临时生成的。

## 7. Runtime 初始化与 reload

Runtime 初始化只使用 [架构文档](../architecture.md#31-runtime) 的唯一顺序；尤其不能合并其中的 `MCP.Prepare → Skill → Config.Activate(binding) → MCP.Activate` 阶段。Session Restore 成功前 API readiness 为 false。

热更新解析新 Config 并完成 Provider/Model/Agent 绑定校验后：

- 新建 Session 使用新根/Agent policy；
- 已存在 Session 继续使用 snapshot 中的 resolved policy；
- Context/Provider 在一个 turn 开始时复制同一 candidate 快照，不能中途混用旧值和新值。

---

*最后更新: 2026-07-23*
