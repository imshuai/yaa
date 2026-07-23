# Agent API

> [返回索引](INDEX.md) · canonical 配置见 [AgentConfig](../config/reference.md#3-agents-节点)

Agent 由 `agents[]` 配置创建，`id` 是唯一寻址键。v1 Remote API 不新增、修改或删除 Agent 配置，也不生成 `agt_*` ID。

## 安全 DTO

列表使用固定 `AgentSummaryView`，详情使用固定 `AgentDetailView`。Remote 只投影 `agent.Manager.Get/Inspect`，不得直接序列化 `AgentConfig`：

```json
{
  "id": "default",
  "name": "Default Agent",
  "provider": "openai",
  "model": "gpt-4o",
  "tools": ["shell", "http"],
  "skills": ["weather"],
  "memory_enabled": true,
  "planner_enabled": false,
  "status": "running"
}
```

`AgentSummaryView` 只有 `id`、`name`、`provider`、`model`、`status`；`AgentDetailView` 在其基础上只增加排序后的 canonical `tools`、`skills`、`memory_enabled` 和 `planner_enabled`。`tools` 不返回 Provider alias 或 alias map。`status` 只有 `running`、`paused`、`stopped`。两者都不包含 `system_prompt`、`tools_config`、`skills_config`、Context/Session/Memory options、路径、Secret 或其他开放 map。

## GET /api/v1/agents

Query：

| 参数 | 默认 | 规则 |
|------|------|------|
| `page` | 1 | `>=1` |
| `page_size` | 20 | `1..100` |
| `status` | 未过滤 | `running|paused|stopped` |

结果按配置中的 Agent ID 升序稳定分页：

```json
{
  "items": [{"id": "default", "name": "Default Agent", "provider": "openai", "model": "gpt-4o", "status": "running"}],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

列表 item 使用 `AgentSummaryView`；详情使用上述 `AgentDetailView`。

## GET /api/v1/agents/:id

`:id` 只接受 `AgentConfig.id`，不接受 name alias。不存在返回 404 / `40401`。响应只能来自 `agent.Manager.Inspect` 的深拷贝，不能从 Config snapshot 补字段。

## POST /api/v1/agents/:id/start

将 `stopped` 或 `paused` 变为 `running`；已经 `running` 时幂等成功。成功 data：

```json
{"id": "default", "status": "running"}
```

## POST /api/v1/agents/:id/pause

将 `running` 变为 `paused`；已经 `paused` 时幂等成功。`stopped` 返回 409 / `40901`。已开始的 Session turn 继续到合法提交边界，之后不再接受新 turn。

## POST /api/v1/agents/:id/stop

将 `running` 或 `paused` 变为 `stopped`；已经 `stopped` 时幂等成功。停止先拒绝新 turn，再取消/等待运行中的 turn 到 shutdown deadline；已提交 Session snapshot 不回滚。

Agent 运行态只存在于当前进程，重启后按 Runtime 启动配置重新建立；这些端点不会回写配置文件。

控制端点统一映射：未知 Agent 为 404 / `40401`；`ErrAgentInvalidState`、`ErrAgentPaused`、`ErrAgentStopped` 为 409 / `40901`；调用 ctx deadline 为 504 / `50401`，client cancel 后通常不再写响应。Stop 已把状态提交为 stopped 后即使等待 turn 超时也不回滚状态。

---

*最后更新: 2026-07-22*
