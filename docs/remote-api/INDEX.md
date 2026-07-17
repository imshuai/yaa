# Yaa! Remote API 设计

> 本文件是 Remote API 文档的目录索引，描述整体设计原则、接口总览和统一规范。
> 各模块的详细端点定义见同目录下的独立文件。

---

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| RESTful | 资源导向，语义清晰的 URL 设计 |
| 统一响应 | 所有端点返回统一格式的 JSON 响应 |
| 版本化 | URL 路径版本化 `/api/v1/...`，保证向后兼容 |
| 三协议 | HTTP REST（请求-响应）+ WebSocket（双向实时）+ SSE（单向流式） |
| 无状态优先 | 尽量无状态，状态由 Session 层管理 |
| 幂等 | GET/PUT/DELETE 天然幂等，POST 通过可选 `Idempotency-Key` 头支持 |

---

## 2. 文档结构

| 文件 | 模块 | 端点数 | 说明 |
|------|------|--------|------|
| [system.md](./system.md) | 系统 | 3 | 健康检查、版本、配置 |
| [agent.md](./agent.md) | Agent | 9 | CRUD + 启停 + 模型切换 |
| [session.md](./session.md) | Session | 8 | 会话 CRUD + 消息历史 + 上下文压缩 |
| [conversation.md](./conversation.md) | 对话 | 3 | 非流式发送、SSE 事件流、WebSocket |
| [tool.md](./tool.md) | Tool | 3 | 列表、详情、直接调用 |
| [skill.md](./skill.md) | Skill | 3 | 列表、详情、手动触发 |
| [provider.md](./provider.md) | Provider | 6 | CRUD + 模型查询 |
| [memory.md](./memory.md) | Memory | 4 | 查询、写入、删除、清空 |
| [mcp.md](./mcp.md) | MCP | 6 | 服务器管理 + MCP Tool 调用 |
| [auth.md](./auth.md) | Auth/Token | 3 | 令牌创建、列表、撤销 |
| **合计** | **10 模块** | **48** | |

---

## 3. 接口总览

### 3.1 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 健康检查 |
| GET | `/api/v1/version` | 版本信息 |
| GET | `/api/v1/config` | 获取运行时配置（脱敏） |

### 3.2 Agent

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents` | 创建 Agent |
| GET | `/api/v1/agents` | 列出所有 Agent |
| GET | `/api/v1/agents/:id` | 获取 Agent 详情 |
| PUT | `/api/v1/agents/:id` | 更新 Agent 配置 |
| DELETE | `/api/v1/agents/:id` | 删除 Agent |
| POST | `/api/v1/agents/:id/start` | 启动 Agent |
| POST | `/api/v1/agents/:id/pause` | 暂停 Agent |
| POST | `/api/v1/agents/:id/stop` | 停止 Agent |
| PATCH | `/api/v1/agents/:id/model` | 切换 Agent 模型 |

### 3.3 Session

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/agents/:id/sessions` | 为指定 Agent 创建 Session |
| GET | `/api/v1/agents/:id/sessions` | 列出指定 Agent 的 Session |
| GET | `/api/v1/sessions/:id` | 获取 Session 详情 |
| DELETE | `/api/v1/sessions/:id` | 关闭/删除 Session |
| POST | `/api/v1/sessions/:id/clear` | 清空 Session 消息历史 |
| GET | `/api/v1/sessions/:id/messages` | 获取 Session 消息历史 |
| DELETE | `/api/v1/sessions/:id/messages/:msgid` | 删除指定消息 |
| POST | `/api/v1/sessions/:id/context/compress` | 手动触发上下文压缩 |

### 3.4 对话

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/sessions/:id/messages` | 发送消息（非流式） |
| GET | `/api/v1/sessions/:id/events` | 事件流（SSE） |
| WS | `/api/v1/sessions/:id/stream` | 流式对话（WebSocket） |

### 3.5 Tool

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tools` | 列出所有已注册 Tool |
| GET | `/api/v1/tools/:name` | 获取 Tool 详情（Schema、描述） |
| POST | `/api/v1/tools/:name/execute` | 直接调用 Tool（调试用） |

### 3.6 Skill

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/skills` | 列出所有已加载 Skill |
| GET | `/api/v1/skills/:name` | 获取 Skill 详情 |
| POST | `/api/v1/skills/:name/invoke` | 手动触发 Skill |

### 3.7 Provider

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/providers` | 列出已注册 Provider |
| GET | `/api/v1/providers/:id` | 获取 Provider 详情 |
| GET | `/api/v1/providers/:id/models` | 列出 Provider 支持的模型 |
| POST | `/api/v1/providers` | 注册新 Provider（运行时动态添加） |
| PUT | `/api/v1/providers/:id` | 更新 Provider 配置 |
| DELETE | `/api/v1/providers/:id` | 移除 Provider |

### 3.8 Memory

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/agents/:id/memory` | 查询 Agent 记忆（支持搜索） |
| POST | `/api/v1/agents/:id/memory` | 写入/更新记忆 |
| DELETE | `/api/v1/agents/:id/memory/:key` | 删除指定记忆 |
| DELETE | `/api/v1/agents/:id/memory` | 清空 Agent 所有记忆 |

### 3.9 MCP

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/mcp/servers` | 列出已注册 MCP Server |
| GET | `/api/v1/mcp/servers/:name` | 获取 MCP Server 详情 |
| POST | `/api/v1/mcp/servers` | 注册 MCP Server |
| PUT | `/api/v1/mcp/servers/:name` | 更新 MCP Server 配置 |
| DELETE | `/api/v1/mcp/servers/:name` | 移除 MCP Server |
| POST | `/api/v1/mcp/servers/:name/tools/:tool` | 调用 MCP Server 暴露的 Tool |

### 3.10 Auth / Token

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/tokens` | 列出所有 Token（管理员） |
| POST | `/api/v1/tokens` | 创建新 Token |
| DELETE | `/api/v1/tokens/:name` | 撤销 Token |

---

## 4. 统一响应格式

### 4.1 成功响应

```json
{
  "code": 0,
  "message": "ok",
  "data": { },
  "request_id": "req_xxx"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | int | 0 表示成功，非 0 表示错误 |
| `message` | string | 人类可读的状态描述 |
| `data` | any | 响应数据，类型取决于具体端点 |
| `request_id` | string | 本次请求的唯一标识，用于链路追踪 |

### 4.2 错误响应

```json
{
  "code": 40401,
  "message": "agent not found",
  "data": null,
  "request_id": "req_xxx"
}
```

> `code` 采用 HTTP 状态码 + 两位子码的组合，如 `40401` = HTTP 404 + 子码 01（资源不存在）。

### 4.3 分页响应

列表类端点统一使用分页结构：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [ ],
    "total": 100,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req_xxx"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `items` | array | 当前页的数据项 |
| `total` | int | 总记录数 |
| `page` | int | 当前页码（从 1 开始） |
| `page_size` | int | 每页条数 |

分页参数通过 query string 传递：`?page=1&page_size=20`。

---

## 5. 认证

所有 API 请求需在 Header 中携带 Token：

```
Authorization: Bearer <token>
```

未携带或不合法的 Token 返回 `401 Unauthorized`。

Token 通过 `/api/v1/tokens` 端点创建和管理（见 [auth.md](./auth.md)）。

---

## 6. 错误码

| 错误码 | HTTP 状态 | 说明 |
|--------|-----------|------|
| 0 | 200 | 成功 |
| 40001 | 400 | 请求参数无效 |
| 40002 | 400 | 请求体解析失败 |
| 40003 | 409 | 资源已存在（名称冲突） |
| 40101 | 401 | 未认证（缺少或无效 Token） |
| 40102 | 401 | Token 已过期 |
| 40301 | 403 | 无权限（Scope 不足） |
| 40401 | 404 | 资源不存在 |
| 40901 | 409 | 资源状态冲突（如 Agent 运行中不可删除） |
| 42201 | 422 | 请求语义错误（如 Provider 仍被引用时不可删除） |
| 42901 | 429 | 请求频率超限 |
| 50001 | 500 | 内部错误 |
| 50002 | 500 | Provider 调用失败 |
| 50003 | 500 | Tool 执行失败 |
| 50004 | 502 | MCP Server 连接失败 |
| 50301 | 503 | 服务不可用（Runtime 未就绪） |

---

## 7. 更新日志

| 日期 | 变更 |
|------|------|
| 2025-07-15 | 初版：48 端点，10 模块，含统一响应、认证、错误码 |
| 2025-07-15 | 文档拆分为按模块独立文件，INDEX.md 作为目录索引 |
