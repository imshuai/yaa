# 内置 Tool - 配置类

> 文档路径: `docs/tool/config-tools.md`
> 上级: [内置 Tool](builtin.md)

---

## 1. v1 边界

v1 只提供 `config_query` 和 `config_reload`。它们读取 Runtime 启动时确定的主配置路径及当前 Effective Config，不接受任意文件路径，也不修改或保存配置文件。

`config_set`、`config_schema`、`config_save` 和 `config_diff` 不实现：Loader 在默认值、环境变量展开和 CLI override 后只保留 Effective Config，无法可靠恢复原始来源层或 Secret 引用。配置修改由运维方编辑文件，再由 watcher 或 `config_reload` 走同一 reload 流程。

## 2. config_query

```go
type ConfigQueryTool struct {
    reload *config.ReloadManager
}
```

参数：

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Dot-separated canonical JSON path. Empty returns the full config.",
      "default": ""
    }
  },
  "additionalProperties": false
}
```

执行时先取得一次 `reload.Current()` snapshot，再调用 Config 模块唯一的 [`config.RedactedView`](../config/overview.md#33-脱敏视图)。路径遍历只能作用于函数返回值。`path` 由 canonical JSON 字段名以 `.` 分隔；数组下标使用十进制 segment，例如 `agents.0.model`。字段名本身不含点且 v1 不实现转义。空 path 返回完整脱敏视图；未知字段、越界下标或穿过标量返回 `ToolResult{IsError:true}`。

脱敏不可关闭。Tool 不接受 `redact_secrets=false`，不返回 Provider `api_key`、HTTP Header/环境变量值或 open-ended options/extra 中的 scalar。返回内容是 JSON object/array/scalar 的编码文本；`RedactedView` 失败是硬错误，不得返回原 snapshot。

## 3. config_reload

```go
type ConfigReloadTool struct {
    reload *config.ReloadManager
}
```

参数：

```json
{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}
```

实现只调用 `reload.Reload()`，不复制 Load、Diff、Validate 或发布逻辑：

```go
result, err := t.reload.Reload()
if err != nil {
    return ToolResult{}, err
}
return JSONResult(result)
```

返回值是 [`config.ReloadResult`](../config/hot-reload.md#3-原子发布)：

```json
{
  "applied": true,
  "changed": ["log.level", "tools.builtin.shell.timeout"],
  "restart_required": false,
  "paths": []
}
```

- `changed` 和 `paths` 均按字典序且不含值。
- 只有全部变化都在 hot-reload allowlist 时 `applied=true`。
- 存在 restart-required path 时不发布任何候选字段，返回 `applied=false`、`restart_required=true` 和 `paths`，这不是 Tool 硬错误。
- Load、类型/语义校验或发布失败返回硬错误，旧 snapshot 保持不变。

Watcher 与此 Tool 共用同一个 `ReloadManager`，因此没有第二套 reload 状态机。Remote API 不提供 Config reload 端点。
