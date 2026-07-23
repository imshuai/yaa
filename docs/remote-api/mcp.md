# MCP API

> [返回索引](INDEX.md) · 配置见 [MCP 配置](../mcp/config-ref.md)

MCP Server 由 `mcp.servers[]` 配置创建，Remote API 只读连接状态。配置 reload 对 MCP 增删/transport 变更按 [hot-reload](../config/hot-reload.md) 的重启边界处理。

## GET /api/v1/mcp/servers

返回 `mcp.Manager.List()` 的 `[]mcp.ServerStatus`，不分页：

```json
{
  "items": [
    {
      "name": "filesystem",
      "status": "connected",
      "transport": "stdio",
      "protocol_version": "2025-03-26",
      "tool_count": 5,
      "connected_at": "2026-07-22T01:00:00Z"
    }
  ]
}
```

字段固定为 `name`、`status`、`transport`、`protocol_version`、`tool_count`、`connected_at` 和可选脱敏 `last_error`。不返回 command、args、URL、headers、环境变量或 Token；这些属于配置/凭据视图。

状态为 `disconnected`、`connecting`、`connected` 或 `error`。上游失败可以使 Runtime `degraded`，不伪造 Tool 可用。

## GET /api/v1/mcp/servers/:name

返回独立的 `ServerDetail` DTO；它嵌入同一 `mcp.ServerStatus` 字段并追加当前 MCP Tool 的元数据：

```go
type ServerDetail struct {
    mcp.ServerStatus
    Tools []tool.ToolInfo `json:"tools"`
}
```

```json
{
  "name": "filesystem",
  "status": "connected",
  "transport": "stdio",
  "protocol_version": "2025-03-26",
  "tool_count": 1,
  "connected_at": "2026-07-22T01:00:00Z",
  "tools": [
    {"name": "mcp.filesystem.read_file", "description": "Read file contents", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}}, "enabled": true, "source": "mcp"}
  ]
}
```

Tool 名称使用 canonical `mcp.<server>.<tool>` 命名空间，并与 Tool Manager 的 `ToolInfo` 一致。Provider-safe alias 只存在于 turn wire 投影，不替换这里的资源名称。未知 Server 返回 404 / `40401`。`ServerStatus` 与 `ServerDetail` 都不返回 command、args、URL、headers、环境变量或 Token。

## 执行与配置边界

没有 `POST .../tools/:tool`、PUT 或 DELETE。MCP Tool 只能作为已注册 Tool 由 Agent turn 调用，Tool Manager 用真实 `agentID` 检查白名单、超时和参数。直接 HTTP 调用无法提供这个 principal，因此不注册该路由。

---

*最后更新: 2026-07-22*
