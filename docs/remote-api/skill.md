# Skill API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/skills

列出所有已加载的 Skill。

**响应 data:**

```json
{
  "items": [
    {
      "name": "weather",
      "description": "Get current weather and forecasts",
      "version": "1.0.0",
      "triggers": ["weather", "temperature", "forecast"],
      "enabled": true,
      "loaded_at": "2025-07-15T10:00:00Z"
    }
  ],
  "total": 1
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `triggers` | []string | 触发关键词列表 |
| `loaded_at` | string | Skill 加载时间 |

---

## GET /api/v1/skills/:name

获取 Skill 详情。

**响应 data:**

```json
{
  "name": "weather",
  "description": "Get current weather and forecasts",
  "version": "1.0.0",
  "triggers": ["weather", "temperature", "forecast"],
  "enabled": true,
  "tools": ["web_search", "web_fetch"],
  "workflow": "Read SKILL.md → Parse location → Fetch weather → Return result",
  "loaded_at": "2025-07-15T10:00:00Z"
}
```

---

## POST /api/v1/skills/:name/invoke

手动触发 Skill 执行。

**请求 Body:**

```json
{
  "session_id": "ses_01J...",
  "input": "What's the weather in Shanghai?",
  "arguments": {}
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `session_id` | string | ❌ | 关联的 Session ID，留空则在临时上下文中执行 |
| `input` | string | ✅ | 触发输入文本 |
| `arguments` | object | ❌ | 额外参数 |

**响应 data:**

```json
{
  "skill": "weather",
  "session_id": "ses_01J...",
  "result": {
    "location": "Shanghai",
    "temperature": 35,
    "condition": "cloudy",
    "humidity": 78
  },
  "tool_calls": [
    { "name": "web_search", "arguments": { "query": "Shanghai weather" } }
  ],
  "duration_ms": 1200
}
```
