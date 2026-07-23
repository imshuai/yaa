# Tool API

> [返回索引](INDEX.md) · canonical 类型见 [Tool Manager](../tool/manager.md#21-toolinfo)

## GET /api/v1/tools

列出 Tool Manager 中所有来源的 Tool，包括 `builtin`、`plugin` 和 `mcp`。数量受配置与已连接扩展限制，不分页。

```json
{
  "items": [
    {
      "name": "http",
      "description": "Send an HTTP request",
      "parameters": {"type": "object", "properties": {"url": {"type": "string"}}, "required": ["url"]},
      "enabled": true,
      "source": "builtin"
    }
  ]
}
```

字段逐一映射 `tool.ToolInfo`；`name` 始终是 canonical Tool name，可能包含 MCP 点分命名空间，不能替换为 turn-local Provider alias。Schema 字段名是 `parameters`，没有另造的 `category`。

## GET /api/v1/tools/:name

`:name` 按 canonical Tool name 精确、大小写敏感寻址。返回一个完整 `ToolInfo`；不存在返回 404 / `40401`。`parameters` 必须是有效 JSON Schema，服务端直接返回 Manager 注册时保存的深拷贝。Provider alias 不是资源 ID。

## 执行边界

v1 没有 `POST /api/v1/tools/:name/execute`。`tool.Manager.Execute` 需要 `agentID` 才能应用 Agent Tool 白名单、超时和审计；绕过 Agent principal 的调试端点会产生第二套权限模型。客户端通过 Session 对话让 Agent 调用 Tool。

---

*最后更新: 2026-07-22*
