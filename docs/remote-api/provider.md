# Provider API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/providers

列出所有已注册的 Provider。

**响应 data:**

```json
{
  "items": [
    {
      "id": "prov_openai",
      "name": "openai",
      "type": "openai",
      "base_url": "https://api.openai.com/v1",
      "enabled": true,
      "models_count": 12,
      "created_at": "2025-07-15T10:00:00Z"
    }
  ],
  "total": 1
}
```

---

## GET /api/v1/providers/:id

获取 Provider 详情（API Key 脱敏）。

**响应 data:**

```json
{
  "id": "prov_openai",
  "name": "openai",
  "type": "openai",
  "base_url": "https://api.openai.com/v1",
  "api_key": "***",
  "enabled": true,
  "models_count": 12,
  "config": {
    "timeout": 30,
    "max_retries": 3
  },
  "created_at": "2025-07-15T10:00:00Z",
  "updated_at": "2025-07-15T10:00:00Z"
}
```

---

## GET /api/v1/providers/:id/models

列出 Provider 支持的模型列表。

**响应 data:**

```json
{
  "items": [
    {
      "id": "gpt-4o",
      "name": "GPT-4o",
      "context_window": 128000,
      "max_output": 16384,
      "supports_streaming": true,
      "supports_tools": true,
      "supports_vision": true,
      "pricing": { "input": 2.5, "output": 10, "unit": "per_1m_tokens" }
    },
    {
      "id": "gpt-4o-mini",
      "name": "GPT-4o mini",
      "context_window": 128000,
      "max_output": 16384,
      "supports_streaming": true,
      "supports_tools": true,
      "supports_vision": false,
      "pricing": { "input": 0.15, "output": 0.6, "unit": "per_1m_tokens" }
    }
  ],
  "total": 2
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `context_window` | int | 上下文窗口大小 |
| `max_output` | int | 最大输出 token 数 |
| `supports_streaming` | bool | 是否支持流式输出 |
| `supports_tools` | bool | 是否支持工具调用 |
| `supports_vision` | bool | 是否支持视觉/图片输入 |
| `pricing.input` | float | 输入价格 |
| `pricing.output` | float | 输出价格 |
| `pricing.unit` | string | 计价单位 |

---

## POST /api/v1/providers

注册新的 Provider（运行时动态添加）。

**请求 Body:**

```json
{
  "name": "anthropic",
  "type": "claude",
  "base_url": "https://api.anthropic.com",
  "api_key": "sk-ant-xxx",
  "config": {
    "timeout": 60,
    "max_retries": 3
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | Provider 唯一标识 |
| `type` | string | ✅ | Provider 类型：`openai` / `claude` / `gemini` / `ollama` / `custom` |
| `base_url` | string | ✅ | API 基础 URL |
| `api_key` | string | ✅ | API 密钥 |
| `config` | object | ❌ | 额外配置（超时、重试等） |

**响应 data:** 同详情结构（API Key 脱敏）。

---

## PUT /api/v1/providers/:id

更新 Provider 配置。

**请求 Body:**

```json
{
  "base_url": "https://api.anthropic.com/v1",
  "api_key": "sk-ant-new",
  "config": { "timeout": 120, "max_retries": 5 }
}
```

> 所有字段可选，仅更新提供的字段。

**响应 data:** 更新后的完整 Provider 对象。

---

## DELETE /api/v1/providers/:id

移除 Provider。若有关联的 Agent 正在使用该 Provider，返回错误。

**响应 data:**

```json
{ "deleted": true }
```
