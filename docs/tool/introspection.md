# 内置 Tool - 只读内视

> 文档路径: docs/tool/introspection.md
> 上级: [内置 Tool](builtin.md)

---

## 1. v1 边界

内视 Tool 只把现有 Runtime/Manager snapshot 投影为有界 JSON 文本，不建立第二套 Registry、状态缓存或权限系统。所有调用仍经过 `tool.Manager.Execute` 的 Agent allowlist、timeout、并发和结果上限。

v1 不提供：

- Skill install/uninstall/enable/disable/reload；
- Provider health probe、health cache、rate-limit cache 或 failover；
- goroutine dump、System Prompt、消息正文、Tool result、配置 options 或 Secret；
- `log_query` 或 `metric_query`。当前架构没有日志存储或时序数据库；日志与指标由配置的外部 sink 消费。

所有参数 Schema 都设置 `additionalProperties:false`。列表按稳定主键升序；空 slice 编码为 `[]`。不存在的资源返回 `ToolResult{IsError:true}`，Manager/context 错误仍作为硬错误返回。以下 Agent、Session、Tool 和 Skill 视图都以 `Execute` 收到的 `ExecutionScope.AgentID` 为唯一 caller；参数不能选择其他 Agent。

## 2. runtime_status

参数是空 object：

```json
{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}
```

返回固定摘要：

```json
{
  "version": "0.1.0",
  "go_version": "go1.20.14",
  "uptime_seconds": 86400,
  "ready": true
}
```

`version`/`go_version` 来自构建信息，uptime 使用 Runtime 启动时刻计算，`ready` 与 `GET /api/v1/health` 读取同一状态。没有 `detail` 或 `full` 模式。

## 3. agent_list

```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "enum": ["running", "paused", "stopped"]
    }
  },
  "additionalProperties": false
}
```

调用 `agent.Manager.Get(scope.AgentID)`，因此最多返回 caller Agent 自身。省略 `status` 表示不过滤；若 caller 状态不匹配则返回空 `items`。格式固定：

```json
{
  "items": [
    {
      "id": "default",
      "name": "Default Agent",
      "provider": "openai",
      "model": "gpt-4o",
      "status": "running"
    }
  ]
}
```

状态枚举与 Remote `AgentSummaryView` 相同。

## 4. agent_inspect

```json
{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}
```

调用 `agent.Manager.Inspect(scope.AgentID)`，返回 caller 摘要及已授权的 Tool/Skill 名称：

```json
{
  "id": "default",
  "name": "Default Agent",
  "provider": "openai",
  "model": "gpt-4o",
  "status": "running",
  "tools": ["http", "shell"],
  "skills": ["weather"],
  "memory_enabled": true,
  "planner_enabled": false
}
```

Tool 名称来自 `tool.Manager.ListForAgent(scope.AgentID)`，Skill 名称来自 `skill.Manager.ResolveForAgent(scope.AgentID)`。结果不包含 System Prompt、Session、Context、options 或配置 Secret。

## 5. session_list

```json
{
  "type": "object",
  "properties": {
    "state": {
      "type": "string",
      "enum": ["created", "active", "paused", "closed"]
    },
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
  },
  "additionalProperties": false
}
```

调用 `session.Manager.List(ctx, scope.AgentID, query)`，按 `created_at` 降序、ID 降序，最多返回 `limit` 项。每项只包含 `id`、`agent_id`、`state`、`message_count`、`created_at` 和 `updated_at`；不返回 metadata 或消息。

## 6. session_inspect

```json
{
  "type": "object",
  "properties": {
    "session_id": {"type": "string", "minLength": 1}
  },
  "required": ["session_id"],
  "additionalProperties": false
}
```

调用 `session.Manager.Get` 后必须验证 `Session.AgentID == scope.AgentID`；不匹配与不存在使用相同的 `ToolResult{IsError:true}`，避免跨 Agent 枚举。成功时返回与 `session_list` 相同的固定元数据字段。v1 不接受 `include_messages`、`include_context` 或 `include_tool_results`。

## 7. tool_list

```json
{
  "type": "object",
  "properties": {
    "source": {
      "type": "string",
      "enum": ["builtin", "plugin", "mcp"]
    }
  },
  "additionalProperties": false
}
```

过滤并返回 `tool.Manager.ListForAgent(scope.AgentID) []ToolInfo`；因此结果天然只含 enabled 且授权的 Tool：

```json
{
  "items": [
    {
      "name": "http",
      "description": "Send an HTTP request",
      "parameters": {"type": "object", "properties": {"url": {"type": "string"}}},
      "enabled": true,
      "source": "builtin"
    }
  ]
}
```

## 8. skill_list

```json
{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}
```

结果由 `skill.Manager.ResolveForAgent(scope.AgentID)` 投影为与 Remote `SkillSummary` 相同的安全字段；只包含 caller 已绑定且 loaded 的 Skill：

```json
{
  "items": [
    {
      "name": "weather",
      "description": "Get current weather and forecasts",
      "version": "1.0.0",
      "status": "loaded"
    }
  ]
}
```

不返回 Prompt、Path、options、Agent binding 或虚构的 `tools_provided`。

## 9. provider_list

参数是空 object。返回 `provider.Manager.List() []ProviderInfo`，不执行网络请求：

```json
{
  "items": [
    {
      "id": "openai",
      "type": "openai",
      "models": [{"id": "gpt-4o", "name": "GPT-4o"}]
    }
  ]
}
```

完整 model item 使用 canonical `provider.ModelInfo`。不返回 API key、base URL、health、latency、rate limit 或 default model。

## 10. mcp_list

```json
{
  "type": "object",
  "properties": {
    "server_name": {"type": "string", "minLength": 1}
  },
  "additionalProperties": false
}
```

过滤并返回 `mcp.Manager.List() []ServerStatus`。省略 `server_name` 返回全部；结果字段与 Remote MCP 列表相同，不包含 command、args、URL、headers、env 或 Token。

## 11. 最小验证

1. 每个 Schema 拒绝未知字段和 trailing JSON。
2. List 顺序稳定，nil slice 输出 `[]`。
3. caller 无法列出其他 Agent、其他 Agent 的 Session 或未授权 Tool/Skill；不存在与越权结果不可区分。
4. 输出中没有 Prompt、消息、Tool result、options、路径或 Secret。
5. `provider_list` 不发网络请求；所有 Tool 都不修改 Manager 状态。
6. 大列表仍由 Tool Manager 的 `max_result_tokens` 最终限制。

---

*最后更新: 2026-07-22*
