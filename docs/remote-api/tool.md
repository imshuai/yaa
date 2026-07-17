# Tool API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/tools

列出所有已注册的内置 Tool。

**响应 data:**

```json
{
  "items": [
    {
      "name": "web_search",
      "description": "Search the web for information",
      "category": "web",
      "enabled": true
    },
    {
      "name": "file_read",
      "description": "Read file contents",
      "category": "filesystem",
      "enabled": true
    }
  ],
  "total": 2
}
```

> Tool 列表不分页，数量有限，一次性返回。

---

## GET /api/v1/tools/:name

获取 Tool 详情，包含完整的参数 Schema。

**响应 data:**

```json
{
  "name": "web_search",
  "description": "Search the web for information",
  "category": "web",
  "enabled": true,
  "schema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Search query"
      },
      "count": {
        "type": "integer",
        "description": "Number of results",
        "default": 5
      }
    },
    "required": ["query"]
  }
}
```

---

## POST /api/v1/tools/:name/execute

直接调用 Tool，用于调试和测试。不经过 Agent / LLM，直接执行并返回结果。

**请求 Body:**

```json
{
  "arguments": {
    "query": "Go 1.25 release notes",
    "count": 3
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `arguments` | object | ✅ | 工具参数，需匹配 Tool 的 Schema |

**响应 data:**

```json
{
  "tool": "web_search",
  "arguments": { "query": "Go 1.25 release notes", "count": 3 },
  "result": {
    "results": [
      { "title": "Go 1.25 Release Notes", "url": "https://go.dev/doc/go1.25", "snippet": "..." }
    ]
  },
  "duration_ms": 340
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `result` | any | 工具执行结果，结构因工具而异 |
| `duration_ms` | int | 执行耗时（毫秒） |
