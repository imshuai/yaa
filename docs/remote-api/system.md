# 系统 API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/health

健康检查，用于探活。

**响应 data:**

```json
{
  "status": "healthy",
  "uptime": 86400,
  "agents": { "total": 5, "running": 3, "paused": 1, "stopped": 1 }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | `healthy` / `degraded` / `unhealthy` |
| `uptime` | int | Runtime 启动至今的秒数 |
| `agents.total` | int | 已注册 Agent 总数 |
| `agents.running` | int | 运行中的 Agent 数 |
| `agents.paused` | int | 暂停的 Agent 数 |
| `agents.stopped` | int | 已停止的 Agent 数 |

---

## GET /api/v1/version

获取版本信息。

**响应 data:**

```json
{
  "version": "0.1.0",
  "git_commit": "abc1234",
  "build_time": "2025-07-15T10:00:00Z",
  "go_version": "go1.25.0"
}
```

---

## GET /api/v1/config

获取运行时配置（脱敏输出，API Key 等敏感字段以 `***` 表示）。

**响应 data:**

```json
{
  "listen": ":8080",
  "data_dir": "./data",
  "providers": [
    { "name": "openai", "type": "openai", "api_key": "***", "base_url": "https://api.openai.com/v1" }
  ],
  "default_model": "gpt-4o",
  "log_level": "info"
}
```
