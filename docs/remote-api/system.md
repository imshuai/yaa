# 系统 API

> [返回索引](INDEX.md)

## GET /api/v1/health

探活和 readiness。该端点默认 public，但仍返回统一 REST envelope。健康时返回 HTTP 200、`code=0` 和 `data`；未 Ready 时返回 HTTP 503、`code=50301`、`data=null`，不会把半初始化快照当作成功响应。

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "healthy",
    "ready": true,
    "uptime_seconds": 86400,
    "agents": {"total": 1, "running": 1, "paused": 0, "stopped": 0},
    "components": {
      "storage": "ready",
      "session_restore": "ready",
      "memory": "ready",
      "provider": "ready"
    }
  },
  "request_id": "req_01J..."
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | `healthy`、`degraded` 或 `unhealthy` |
| `ready` | bool | 关键初始化和 Session Restore 是否完成 |
| `uptime_seconds` | int64 | Runtime 启动后的秒数 |
| `components` | object | 只返回组件状态，不返回底层错误正文 |

组件状态取 `ready|degraded|unhealthy|not_ready|disabled`。`ready=false` 或 `status=unhealthy` 时返回 HTTP 503、`code=50301`；关键组件 ready 且只有向量/MCP 等可降级组件异常时返回 HTTP 200、`code=0`、`status=degraded`、`ready=true`。响应中的 Agent 数量是当前配置 Agent 的运行态快照。

未 Ready 的错误 envelope 示例：

```json
{"code":50301,"message":"runtime not ready","data":null,"request_id":"req_01J..."}
```

## GET /api/v1/version

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "version": "0.1.0",
    "git_commit": "abc1234",
    "build_time": "2026-07-22T01:00:00Z",
    "go_version": "go1.20.14"
  },
  "request_id": "req_01J..."
}
```

`go_version` 反映构建工具链，项目目标为 Go 1.20.x；实现不得在协议示例中假定 Go 1.25 API。

## GET /api/v1/config

返回当前生效配置的脱敏视图。Handler 只读取一次 `reload.Current()`，并调用 Config 模块唯一的 [`config.RedactedView`](../config/overview.md#33-脱敏视图)；失败使用 HTTP 500 / `50001`，不得回退到未脱敏 snapshot。JSON 层级和字段名与 canonical `config.Config` 相同，不另造 `listen`、`data_dir` 或 `default_model` 等根字段；默认值和环境变量展开已经应用。下例只展示成功 envelope 的 `data` 字段节选，不表示省略的节点不存在或可省略。

```json
{
  "config_version": "1.0",
  "runtime": {
    "storage": {"type": "sqlite", "path": "./data/yaa.db"},
    "api": {"http": {"addr": "127.0.0.1:8080"}, "ws": {"enabled": true}, "sse": {"enabled": true}},
    "auth": {"enabled": true, "token_type": "static", "tokens": [{"name": "admin", "token": "***", "roles": ["admin"]}]}
  },
  "agents": [{"id": "default", "name": "Default Agent", "provider": "openai", "model": "gpt-4o"}],
  "providers": [{"id": "openai", "type": "openai", "api_key": "***", "base_url": "https://api.openai.com/v1"}],
  "mcp": {"servers": [], "server": {"enabled": false}},
  "tools": {"builtin": {}},
  "skills": {"dir": "./skills", "per_skill": {}},
  "memory": {"enabled": true},
  "session": {},
  "context": {},
  "planner": {},
  "plugins": {},
  "log": {"level": "info", "format": "text", "output": "stderr"}
}
```

已知 Secret、MCP `headers`/`env` 和所有开放 `options`/`extra`/Plugin config 都按 `config.RedactedView` 的 fail-closed 规则处理。没有配置的可选节点按 canonical JSON 空值/空集合输出，不能改名或移动层级。

此端点只读。配置文件热更新由 watcher 负责，不提供未定义的 `/api/v1/config/reload` 路由。

---

*最后更新: 2026-07-22*
