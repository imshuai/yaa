# Skill 调用契约

> 上级: [Skill 系统设计](README.md)
> Session 提交: [Session 集成](../session/integration.md)

---

## 1. 单一流程

Skill 不靠分析 assistant 自由文本触发，也不产生隐藏的第二个 turn。每个 Agent 的 Skill 集合由启动配置固定，每次正常 turn 都走同一流程：

```text
accept and commit current user message
  -> SkillManager.ResolveForAgent(agentID)
  -> render one system message per resolved Skill
  -> retrieve and render optional Memory system message
  -> append Session message snapshot
  -> attach Tool definitions allowed for the Agent
  -> ContextManager.Build
  -> Provider Chat/StreamChat and normal Tool loop
  -> commit complete Tool units and final assistant
```

Skill Manager 不调用 Provider、Tool Manager Execute 或 Session Append。Agent 只是把 Skill snapshot 投影成候选 `provider.ChatRequest.Messages`。

## 2. Prompt 投影

每个 `ResolvedSkill` 生成一个独立的 `provider.Message{Role:"system"}`：

```text
## Skill: web-scraper

Options:
{"max_pages":20}

Instructions:
# Web Scraper
Use the HTTP tool to fetch each page...
```

规则：

1. 依赖 Skill 在使用者之前，同层按 name 升序。
2. options 使用 `encoding/json` 编码，HTML escaping 关闭；空 options 输出 `{}`。
3. body 只规范化 CRLF，不做模板替换、变量展开或 Markdown 重写。
4. Skill system messages 位于 Agent base system prompt 之后、Memory system message之前、Session user/history 之前。
5. 全部 Skill system messages 都是 Context 的 protected unit；超出预算返回 `ErrContextOverflow`，不能截掉一部分 Skill 后继续。

Agent 不把 Prompt、options 或“active skill”写入 Session。下一 turn 从同一不可变 Manager snapshot 重新投影，因此 Restore 不需要持久化 Skill 状态。

## 3. Tool 关系

`Skill.Tools` 只是启动期依赖声明：

- Tool Manager 已经是所有 Tool 的唯一 registry 和执行 owner。
- Skill 不自动扩大 `agents[].tools`；依赖 Tool 必须同时被 Agent allowlist 允许。
- LLM 根据 Skill Prompt 产生标准 Provider Tool call；Agent 仍通过 `tool.Manager.Execute(ExecutionScope{AgentID, SessionID}, ...)` 执行。
- Tool result、失败、重试和完整 Tool unit 提交不因 Skill 而改变。

资源文件也必须通过现有 File/Shell Tool 访问。Skill 路径不能绕过 allowed/blocked path、命令 allowlist、超时或输出上限。

## 4. Skill 依赖

`Skill.Skills` 表示 Prompt 依赖，不是递归运行时调用。Manager 在启动时计算传递闭包并去重，Agent 一次取得已经排好序的完整 `ResolvedSkill` 列表。运行期不存在递归栈、动态 activation 或嵌套深度配置。

如果 A 和 B 都依赖 C，C 只投影一次；如果 Agent 只列出 A 但没有把 C 放入自己的精确 Skill allowlist，启动绑定失败。

## 5. 失败与取消

| 阶段 | 行为 |
|------|------|
| ResolveForAgent 失败 | 当前 turn 失败；已提交 user 保留 |
| options JSON 编码失败 | 启动校验应已阻止；运行期视为内部错误 |
| protected Skill prompts 超预算 | 返回 Context overflow，不调用 Provider |
| Provider/Tool 失败 | 使用各自 canonical 错误；不由 Skill 重试 |
| request ctx 取消 | 立即停止后续 Provider/Tool；已提交 unit 不回滚 |

流式输出仍只使用 [ConversationFrame](../remote-api/conversation.md)；没有 `skill_started`、`skill_completed` 或其他 Skill frame。

## 6. 最小测试

1. 相同 Config 和文件输入产生字节相同、顺序相同的 Skill system messages。
2. 共享依赖只出现一次，环在启动时拒绝。
3. Skill Prompt 不进入 Session snapshot，Restore 后候选请求仍可重新构造。
4. 不允许的 Tool 依赖在启动绑定阶段失败，而不是运行到 LLM call 后才失败。
5. Skill prompts 单独超过预算时 Provider 调用次数为 0。

---

*最后更新: 2026-07-22*
