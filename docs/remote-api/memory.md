# Memory API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/agents/:id/memory

查询 Agent 的记忆。支持关键词搜索和分页。

**Query 参数:**

| 参数 | 类型 | 说明 |
|------|------|------|
| `q` | string | 搜索关键词（模糊匹配 key 或 value） |
| `page` | int | 页码，默认 1 |
| `page_size` | int | 每页条数，默认 20 |

**响应 data:**

```json
{
  "items": [
    {
      "key": "user_preference",
      "value": "prefers concise answers",
      "scope": "agent",
      "created_at": "2025-07-15T10:00:00Z",
      "updated_at": "2025-07-15T10:00:00Z"
    },
    {
      "key": "last_topic",
      "value": "Go concurrency patterns",
      "scope": "agent",
      "created_at": "2025-07-15T11:00:00Z",
      "updated_at": "2025-07-15T11:00:00Z"
    }
  ],
  "total": 2,
  "page": 1,
  "page_size": 20
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `key` | string | 记忆键名 |
| `value` | string | 记忆内容 |
| `scope` | string | 记忆作用域：`agent`（Agent 级）/ `session`（Session 级） |

---

## POST /api/v1/agents/:id/memory

写入或更新记忆。Key 存在则更新，不存在则创建。

**请求 Body:**

```json
{
  "key": "user_preference",
  "value": "prefers detailed answers with examples",
  "scope": "agent"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `key` | string | ✅ | 记忆键名 |
| `value` | string | ✅ | 记忆内容 |
| `scope` | string | ❌ | 作用域，默认 `agent` |

**响应 data:**

```json
{
  "key": "user_preference",
  "value": "prefers detailed answers with examples",
  "scope": "agent",
  "created_at": "2025-07-15T10:00:00Z",
  "updated_at": "2025-07-15T12:00:00Z"
}
```

---

## DELETE /api/v1/agents/:id/memory/:key

删除指定 Key 的记忆。

**响应 data:**

```json
{ "deleted": true }
```

---

## DELETE /api/v1/agents/:id/memory

清空 Agent 的所有记忆。

**请求 Body（可选）:**

```json
{
  "scope": "agent"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `scope` | string | ❌ | 仅清空指定作用域，留空则清空全部 |

**响应 data:**

```json
{
  "deleted_count": 15,
  "scope": "all"
}
```
