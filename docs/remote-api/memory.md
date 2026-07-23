# Memory API

> [返回索引](./INDEX.md)
> 领域契约: [Memory 系统设计](../memory/README.md)

所有 REST 响应使用 [统一 envelope](INDEX.md#4-rest-envelope)，本文只展示 `data`。Memory v1 的 layer 固定为 `long_term`；Agent ID 来自路径，Session 来源通过 `session_id` 指定。每个 handler 在入口读取一次 Config snapshot，解析该 Agent 的 `config.MemoryPolicy`，并把它显式传给本请求的全部 Memory 调用。

## DTO

```json
{
  "agent_id": "default",
  "session_id": "ses_01J...",
  "layer": "long_term",
  "key": "preference.answer_style",
  "content": "用户偏好简洁回答",
  "metadata": {"source": "user"},
  "created_at": "2026-07-22T01:00:00Z",
  "updated_at": "2026-07-22T01:00:00Z",
  "expires_at": null,
  "version": 1,
  "index_status": "ready"
}
```

`index_status` 是 handler 在构造响应时调用 `memory.Manager.IndexStatus(agentID)` 得到的派生字段：`ready` 或 `degraded`。它不写入 MemoryItem。`version`、时间和 `agent_id`/`layer` 为只读；未知字段、伪造 Version 或非 `long_term` layer 返回 `400 / 40001`。

## GET /api/v1/agents/:id/memory

按 scope 搜索 Agent 的记忆。它是有上限的 Search，不承诺 `total` 或完整分页；服务端始终最多返回 limit 条，最大 100。

| Query | 默认值 | 规则 |
|-------|--------|------|
| `q` | 空 | 对 key/content 做大小写折叠 substring；空表示不做文本过滤 |
| `session_id` | 空 | 空表示 Agent 全部来源；非空只查该 Session 来源 |
| `include_global` | `false` | session_id 非空时可设 true，联合 Agent 全局 items |
| `limit` | 0 | 0 使用 effective `vector.top_k`；1..100 |
| `metadata` | 空 | JSON object；顶层值深度相等匹配 |

向量启用且 q 非空时使用向量 Search；失败是否回退关键词由 Agent effective policy 决定。关键词结果按 `updated_at DESC, session_id ASC, key ASC` 排序。

响应 `data`：

```json
{
  "items": [
    {
      "agent_id": "default",
      "session_id": "ses_01J...",
      "layer": "long_term",
      "key": "preference.answer_style",
      "content": "用户偏好简洁回答",
      "metadata": {"source": "user"},
      "created_at": "2026-07-22T01:00:00Z",
      "updated_at": "2026-07-22T01:00:00Z",
      "expires_at": null,
      "version": 1,
      "score": 0
    }
  ],
  "limit": 10,
  "index_status": "ready"
}
```

`score` 属于 SearchResult，不属于 item；关键词路径固定为 0。API 不返回过期 item。

## GET /api/v1/agents/:id/memory/:key

读取单个 item。`session_id` 是必填 query 参数，即使读取 Agent 全局 item 也要显式传 `session_id=`；空值表示全局主键。`:key` 是一个 RFC 3986 percent-encoded path segment：客户端对原始 UTF-8 key 做一次 `url.PathEscape`，服务端从 raw path 解码一次，允许编码后的 `/`，不得按解码后的 slash 重新拆路由。缺少参数、scope 不完整或 key 不存在分别返回 400/404。

```text
GET /api/v1/agents/default/memory/preference.answer_style?session_id=ses_01J...
GET /api/v1/agents/default/memory/preference.answer_style?session_id=
```

## POST /api/v1/agents/:id/memory

执行唯一 Put/upsert。请求 body：

```json
{
  "session_id": "ses_01J...",
  "key": "preference.answer_style",
  "content": "用户偏好简洁回答",
  "metadata": {"source": "user"},
  "expires_at": null
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|:----:|------|
| `session_id` | string | 否 | 空表示 Agent 全局 item |
| `key` | string | 是 | 1..256 UTF-8 bytes |
| `content` | string | 是 | 1..65536 UTF-8 bytes |
| `metadata` | object | 否 | JSON object，编码后最多 16384 bytes |
| `expires_at` | RFC3339/null | 否 | null 应用 default TTL；zero RFC3339 表示永不过期 |

Manager 的 `PutResult.Created` 决定 HTTP 200（更新）或 201（新建），`PutResult.Item` 构造完整 DTO。ContentStore 已提交但 index degraded 时仍返回 2xx，并将 `index_status` 设为 `degraded`；客户端不得据此重复 Put。

## DELETE /api/v1/agents/:id/memory/:key

删除一个完整 scope 的 item。`session_id` 必填 query，空值表示 Agent 全局 item。成功返回：

```json
{"deleted": true, "agent_id": "default", "session_id": "ses_01J...", "layer": "long_term", "key": "preference.answer_style"}
```

不存在返回 404；删除后 index 清理失败仍视为内容删除成功，并由健康状态标记 degraded。

## DELETE /api/v1/agents/:id/memory

清空 scope。`session_id` 可选：省略或空值清除该 Agent 的全部 long-term items；非空只清除该 Session 来源。响应：

```json
{"deleted_count": 15, "agent_id": "default", "session_id": "", "layer": "long_term"}
```

请求不得使用 `scope=agent|session|all` 等未定义枚举，也不接受 body 中的另一套 scope DTO。

## POST /api/v1/agents/:id/memory/promote

显式将 Session 来源 item 复制为 Agent 全局 item。请求：

```json
{"session_id": "ses_01J...", "key": "preference.answer_style"}
```

源保留，目标使用空 `session_id` 和当前 default TTL；目标冲突按 Put 更新。成功返回目标 DTO，并发布一次 `memory.promoted`。Session Close 不会自动调用此端点。

## POST /api/v1/agents/:id/memory/reindex

要求 vector enabled。`write:memory` 的 Agent principal 可同步重建指定 Agent 的可重建向量索引；body 可省略，响应：

```json
{"agent_id": "default", "layer": "long_term", "status": "ready", "indexed": 42}
```

request context 为 `context.Canceled` 时客户端已经断开，handler 通常不再写响应；只有 request context 的 deadline exceeded 返回 `504 / 50401`。其他 embedding dimension、内部 timeout、index 或存储故障返回 `503 / 50301`，旧索引保持可用。Reindex failure 同时保留 `ErrMemoryReindexFailed` 与底层原因。

## 错误映射

| 条件 | HTTP/business code |
|------|-------------------|
| invalid key/content/scope/limit/unknown field/managed field/expired input/unsupported layer | 400 / `40001` |
| item 不存在或已过期 | 404 / `40401` |
| Memory disabled | 409 / `40901` |
| Memory quota | 429 / `42901` |
| Manager closed、ContentStore unavailable/corrupt、vector unavailable、Reindex failure、内部 timeout | 503 / `50301` |
| request context deadline exceeded | 504 / `50401` |

`session_id` 只是来源 namespace，不要求对应 Session 当前存在；Session 被删除后历史 Memory 仍按 Agent scope 可读/可删。只有 Agent 不匹配的路径访问才按 `40401` 处理。request context 为 `context.Canceled` 时通常不写错误 envelope。响应错误只包含稳定 error class 和 request ID，不泄露 Content、metadata value、embedding、数据库路径或凭据。

---

*最后更新: 2026-07-22*
