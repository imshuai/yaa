# MCP API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/mcp/servers

列出所有已注册的 MCP Server。

**响应 data:**

```json
{
  "items": [
    {
      "name": "filesystem",
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
      "status": "connected",
      "tools_count": 5,
      "created_at": "2025-07-15T10:00:00Z"
    },
    {
      "name": "github",
      "transport": "sse",
      "url": "https://mcp.github.com/sse",
      "status": "connected",
      "tools_count": 8,
      "created_at": "2025-07-15T10:00:00Z"
    }
  ],
  "total": 2
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `transport` | string | 传输方式：`stdio` / `sse` / `websocket` |
| `command` | string\|null | stdio 模式的启动命令 |
| `args` | []string\|null | stdio 模式的命令参数 |
| `url` | string\|null | sse/websocket 模式的连接 URL |
| `status` | string | 连接状态：`connected` / `disconnected` / `error` |
| `tools_count` | int | 暴露的 Tool 数量 |

---

## GET /api/v1/mcp/servers/:name

获取 MCP Server 详情，包含暴露的 Tool 列表。

**响应 data:**

```json
{
  "name": "filesystem",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
  "status": "connected",
  "tools": [
    {
      "name": "read_file",
      "description": "Read file contents",
      "schema": {
        "type": "object",
        "properties": {
          "path": { "type": "string", "description": "File path" }
        },
        "required": ["path"]
      }
    },
    {
      "name": "write_file",
      "description": "Write file contents",
      "schema": {
        "type": "object",
        "properties": {
          "path": { "type": "string", "description": "File path" },
          "content": { "type": "string", "description": "File content" }
        },
        "required": ["path", "content"]
      }
    }
  ],
  "created_at": "2025-07-15T10:00:00Z",
  "updated_at": "2025-07-15T10:00:00Z"
}
```

---

## POST /api/v1/mcp/servers

注册新的 MCP Server。

**请求 Body:**

```json
{
  "name": "database",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-sqlite", "--db-path", "/data/app.db"],
  "env": { "NODE_ENV": "production" },
  "auto_connect": true
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | MCP Server 唯一标识 |
| `transport` | string | ✅ | 传输方式：`stdio` / `sse` / `websocket` |
| `command` | string | 条件 | stdio 模式必填 |
| `args` | []string | ❌ | stdio 模式的命令参数 |
| `env` | object | ❌ | 环境变量 |
| `url` | string | 条件 | sse/websocket 模式必填 |
| `auto_connect` | bool | ❌ | 是否自动连接，默认 true |

**响应 data:** 同详情结构。

---

## PUT /api/v1/mcp/servers/:name

更新 MCP Server 配置。更新后会自动重连。

**请求 Body:**

```json
{
  "args": ["-y", "@modelcontextprotocol/server-sqlite", "--db-path", "/data/new.db"],
  "env": { "NODE_ENV": "development" },
  "auto_connect": true
}
```

> 所有字段可选，仅更新提供的字段。

**响应 data:** 更新后的完整 MCP Server 对象。

---

## DELETE /api/v1/mcp/servers/:name

移除 MCP Server。会断开连接，关联的 Agent 自动解除引用。

**响应 data:**

```json
{ "deleted": true }
```

---

## POST /api/v1/mcp/servers/:name/tools/:tool

调用 MCP Server 暴露的 Tool。直接执行，不经过 Agent / LLM。

**请求 Body:**

```json
{
  "arguments": {
    "path": "/data/hello.txt"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `arguments` | object | ✅ | 工具参数，需匹配 Tool 的 Schema |

**响应 data:**

```json
{
  "server": "filesystem",
  "tool": "read_file",
  "arguments": { "path": "/data/hello.txt" },
  "result": {
    "content": "Hello, World!"
  },
  "duration_ms": 12
}
```
