# Agent API

> [← 返回索引](./INDEX.md)

---

## POST /api/v1/agents

创建 Agent。

**请求 Body:**

```json
{
  "name": "my-agent",
  "display_name": "My Assistant",
  "model": "gpt-4o",
  "provider": "openai",
  "system_prompt": "You are a helpful assistant.",
  "temperature": 0.7,
  "max_tokens": 4096,
  "tools": ["web_search", "file_read"],
  "skills": ["weather", "summarize"],
  "mcp_servers": ["filesystem"],
  "memory_enabled": true,
  "metadata": { "project": "demo" }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | Agent 唯一标识（slug 格式） |
| `display_name` | string | ❌ | 显示名称 |
| `model` | string | ✅ | 使用的模型 ID |
| `provider` | string | ❌ | Provider 名称，默认使用系统默认 |
| `system_prompt` | string | ❌ | 系统提示词 |
| `temperature` | float | ❌ | 采样温度，默认 0.7 |
| `max_tokens` | int | ❌ | 最大输出 token 数 |
| `tools` | []string | ❌ | 启用的内置 Tool 列表 |
| `skills` | []string | ❌ | 启用的 Skill 列表 |
| `mcp_servers` | []string | ❌ | 关联的 MCP Server 列表 |
| `memory_enabled` | bool | ❌ | 是否启用记忆，默认 false |
| `metadata` | object | ❌ | 自定义元数据 |

**响应 data:**

```json
{
  "id": "agt_01J...",
  "name": "my-agent",
  "display_name": "My Assistant",
  "model": "gpt-4o",
  "provider": "openai",
  "status": "stopped",
  "system_prompt": "You are a helpful assistant.",
  "temperature": 0.7,
  "max_tokens": 4096,
  "tools": ["web_search", "file_read"],
  "skills": ["weather", "summarize"],
  "mcp_servers": ["filesystem"],
  "memory_enabled": true,
  "metadata": { "project": "demo" },
  "created_at": "2025-07-15T10:00:00Z",
  "updated_at": "2025-07-15T10:00:00Z"
}
```

---

## GET /api/v1/agents

列出所有 Agent（分页）。

**Query 参数:**

| 参数 | 类型 | 说明 |
|------|------|------|
| `page` | int | 页码，默认 1 |
| `page_size` | int | 每页条数，默认 20 |
| `status` | string | 按状态过滤：`running` / `paused` / `stopped` |

**响应 data:**

```json
{
  "items": [
    {
      "id": "agt_01J...",
      "name": "my-agent",
      "display_name": "My Assistant",
      "model": "gpt-4o",
      "provider": "openai",
      "status": "running",
      "created_at": "2025-07-15T10:00:00Z",
      "updated_at": "2025-07-15T10:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

---

## GET /api/v1/agents/:id

获取 Agent 详情。

**路径参数:**

| 参数 | 说明 |
|------|------|
| `id` | Agent ID 或 name |

**响应 data:** 同 POST /api/v1/agents 响应结构。

---

## PUT /api/v1/agents/:id

更新 Agent 配置。全量更新，未提供的字段会被重置为默认值。

**请求 Body:** 同创建请求，所有字段均为可选。

**响应 data:** 更新后的完整 Agent 对象。

---

## DELETE /api/v1/agents/:id

删除 Agent。删除前会自动停止 Agent。

**响应 data:**

```json
{ "deleted": true }
```

---

## POST /api/v1/agents/:id/start

启动 Agent（从 stopped/paused 状态恢复运行）。

**响应 data:**

```json
{
  "id": "agt_01J...",
  "status": "running",
  "started_at": "2025-07-15T10:00:00Z"
}
```

---

## POST /api/v1/agents/:id/pause

暂停 Agent。暂停后 Agent 保留在内存中，可快速恢复。

**响应 data:**

```json
{
  "id": "agt_01J...",
  "status": "paused"
}
```

---

## POST /api/v1/agents/:id/stop

停止 Agent。释放运行时资源，Session 数据保留。

**响应 data:**

```json
{
  "id": "agt_01J...",
  "status": "stopped"
}
```

---

## PATCH /api/v1/agents/:id/model

切换 Agent 模型，运行时生效，无需重启 Agent。

**请求 Body:**

```json
{
  "model": "claude-sonnet-4-20250514",
  "provider": "claude"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | ✅ | 新的模型 ID |
| `provider` | string | ❌ | Provider 名称，留空则沿用当前 |

**响应 data:**

```json
{
  "id": "agt_01J...",
  "model": "claude-sonnet-4-20250514",
  "provider": "claude",
  "updated_at": "2025-07-15T10:00:00Z"
}
```
