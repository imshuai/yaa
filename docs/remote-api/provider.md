# Provider API

> [返回索引](INDEX.md) · canonical 配置见 [Config providers](../config/reference.md#4-providers-节点) · 模型类型见 [ModelInfo](../provider.md#36-modelinfo)

Provider 在 Runtime 启动时由 `providers[]` 配置注册。v1 Remote API 只读，不提供运行时 POST/PUT/DELETE；配置变更由文件 reload 或重启边界处理。

## ProviderView

列表和详情使用固定 DTO，不直接序列化 `ProviderConfig`：

```go
type ProviderSummary struct {
    ID     string   `json:"id"`
    Type   string   `json:"type"`
    Models []string `json:"models"` // model IDs，稳定排序
}

type ProviderView struct {
    ID            string               `json:"id"`
    Type          string               `json:"type"`
    Timeout       string               `json:"timeout"`
    MaxRetries    int                  `json:"max_retries"`
    RetryInterval string               `json:"retry_interval"`
    Models        []provider.ModelInfo `json:"models"`
}
```

`api_key`、`base_url` 和开放厂商扩展 `extra` 始终省略，不输出占位符，也不生成 `name`、`enabled`、时间戳或 nested `config`。`base_url` 可能暴露内部网络拓扑；只有具备 `read:config` 的调用方可通过统一脱敏的 `GET /api/v1/config` 查看完整配置形状。

## GET /api/v1/providers

返回当前 Manager 已注册 Provider。列表数量受配置约束，不分页：

```json
{
  "items": [
    {"id": "openai", "type": "openai", "models": ["gpt-4o"]}
  ]
}
```

列表 item 固定为 `ProviderSummary`；详情端点固定为 `ProviderView`。

## GET /api/v1/providers/:id

`:id` 必须是配置中的 Provider ID。响应示例：

```json
{
  "id": "openai",
  "type": "openai",
  "timeout": "120s",
  "max_retries": 3,
  "retry_interval": "1s",
  "models": [{
    "id": "gpt-4o",
    "name": "GPT-4o",
    "context_window": 128000,
    "max_output": 16384,
    "supports_tools": true,
    "supports_vision": true,
    "supports_streaming": true,
    "supports_thinking": false,
    "thinking_efforts": [],
    "min_thinking_budget": 0
  }]
}
```

## GET /api/v1/providers/:id/models

返回 `{ "items": [...] }`，其中每项是 canonical `provider.ModelInfo`。字段固定为：

| 字段 | 类型 |
|------|------|
| `id` | string |
| `name` | string |
| `context_window` | int |
| `max_output` | int |
| `supports_tools` | bool |
| `supports_vision` | bool |
| `supports_streaming` | bool |
| `supports_thinking` | bool |
| `thinking_efforts` | []string |
| `min_thinking_budget` | int |

不承诺 pricing 字段；定价不是 `ModelInfo` 的 v1 属性。

Provider 不存在返回 404 / `40401`。Provider 请求失败由对话 API 按 [统一错误码](INDEX.md#7-错误码) 映射，不能通过此只读 API 修改重试或 base URL。

---

*最后更新: 2026-07-22*
