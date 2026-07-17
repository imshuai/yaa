# 内置 Tool — 内视与管理类

> 文档路径: docs/tool/introspection.md
> 上级: docs/tool/builtin.md 6.5-6.7

---

### 6.5 内视（Introspection）系列工具

内视工具让 Agent 能够**自我认知**——了解自身运行状态、资源占用、组件健康状况。这是 Agent Runtime 区别于简单 Chat 应用的核心能力。

#### 6.5.1 runtime_status

查询 Yaa! 运行时的整体状态。

```go
type RuntimeStatusTool struct {
    runtime *Runtime
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "detail": {
      "type": "string",
      "enum": ["summary", "full"],
      "description": "summary: key metrics only. full: includes goroutine dump, memory breakdown, etc.",
      "default": "summary"
    }
  }
}
```

**返回示例：**

```json
{
  "version": "0.1.0",
  "uptime_seconds": 86400,
  "start_time": "2026-07-14T17:12:00Z",
  "go_version": "go1.25.11",
  "arch": "arm",
  "os": "linux",
  "memory": {
    "alloc_bytes": 134217728,
    "sys_bytes": 268435456,
    "gc_count": 42,
    "gc_pause_ms": 1.2
  },
  "goroutines": 128,
  "active_sessions": 5,
  "active_agents": 3,
  "total_requests": 15234,
  "total_tool_calls": 8921,
  "total_tokens": {
    "input": 4523000,
    "output": 893000
  }
}
```

#### 6.5.2 agent_list

列出所有注册的 Agent 及其状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "enum": ["active", "idle", "error", "all"],
      "default": "all"
    }
  }
}
```

**返回示例：**

```json
{
  "agents": [
    {
      "id": "default",
      "name": "Default Assistant",
      "status": "active",
      "active_sessions": 2,
      "total_sessions": 156,
      "provider": "deepseek",
      "model": "deepseek-chat",
      "tools_count": 12,
      "skills_count": 3
    }
  ]
}
```

#### 6.5.3 agent_inspect

查看指定 Agent 的详细信息。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "agent_id": {
      "type": "string",
      "description": "Agent ID to inspect"
    },
    "include_sessions": {
      "type": "boolean",
      "default": true
    },
    "include_context": {
      "type": "boolean",
      "default": false,
      "description": "If true, include recent context messages (may be large)"
    }
  },
  "required": ["agent_id"]
}
```

**返回内容：**

- Agent 基础信息（ID、名称、状态、创建时间）
- 绑定的 Provider 和 Model
- 可用 Tool 列表及权限状态
- 已加载 Skill 列表
- 活跃 Session 列表及摘要
- Token 使用统计
- 最近错误记录

#### 6.5.4 session_list

列出活跃 Session 及摘要信息。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "agent_id": {
      "type": "string",
      "description": "Filter by agent ID. Empty = all agents."
    },
    "status": {
      "type": "string",
      "enum": ["active", "closed", "all"],
      "default": "active"
    },
    "limit": {
      "type": "integer",
      "default": 20
    }
  }
}
```

**返回示例：**

```json
{
  "sessions": [
    {
      "id": "sess_abc123",
      "agent_id": "default",
      "status": "active",
      "created_at": "2026-07-15T10:00:00Z",
      "last_active": "2026-07-15T17:10:00Z",
      "message_count": 24,
      "token_usage": {
        "input": 12000,
        "output": 3400,
        "total": 15400
      },
      "context_tokens": 15400,
      "max_context_tokens": 64000
    }
  ]
}
```

#### 6.5.5 session_inspect

查看指定 Session 的详细信息，包括上下文和消息历史。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "session_id": {
      "type": "string",
      "description": "Session ID to inspect"
    },
    "include_messages": {
      "type": "boolean",
      "default": true
    },
    "message_limit": {
      "type": "integer",
      "default": 50,
      "description": "Max messages to return"
    },
    "include_tool_results": {
      "type": "boolean",
      "default": false,
      "description": "If true, include tool call results in messages"
    }
  },
  "required": ["session_id"]
}
```

**返回内容：**

- Session 基础信息（ID、Agent、状态、创建/最后活跃时间）
- 上下文统计（消息数、Token 用量、上下文窗口使用率）
- 消息历史（按 `message_limit` 截断，支持排除 Tool 结果以减少体积）
- Provider/Model 信息
- Token 消耗明细

#### 6.5.6 tool_list

列出所有已注册 Tool 及其状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "source": {
      "type": "string",
      "enum": ["builtin", "plugin", "mcp", "all"],
      "default": "all"
    },
    "enabled_only": {
      "type": "boolean",
      "default": false
    }
  }
}
```

**返回示例：**

```json
{
  "tools": [
    {
      "name": "shell",
      "description": "Execute shell commands",
      "source": "builtin",
      "enabled": true,
      "timeout": "60s",
      "registered_agents": ["default", "dev-agent"]
    },
    {
      "name": "http",
      "description": "Send HTTP requests",
      "source": "builtin",
      "enabled": true,
      "timeout": "30s"
    }
  ]
}
```

#### 6.5.7 skill_list

列出已安装 Skill 及加载状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "status": {
      "type": "string",
      "enum": ["loaded", "unloaded", "error", "all"],
      "default": "all"
    }
  }
}
```

**返回示例：**

```json
{
  "skills": [
    {
      "name": "weather",
      "description": "Get current weather and forecasts",
      "status": "loaded",
      "version": "1.2.0",
      "tools_provided": ["weather_query", "weather_forecast"],
      "agents_bound": ["default"]
    }
  ]
}
```

#### 6.5.8 provider_list

列出已注册 Provider 及其模型和健康状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "include_models": {
      "type": "boolean",
      "default": true,
      "description": "If true, include available models for each provider"
    }
  }
}
```

**返回示例：**

```json
{
  "providers": [
    {
      "name": "deepseek",
      "type": "openai",
      "base_url": "https://api.deepseek.com/v1",
      "status": "healthy",
      "last_check": "2026-07-15T17:11:00Z",
      "latency_ms": 234,
      "models": ["deepseek-chat", "deepseek-reasoner"],
      "default_model": "deepseek-chat",
      "rate_limit": {
        "requests_per_minute": 60,
        "tokens_per_minute": 100000
      }
    }
  ]
}
```

#### 6.5.9 mcp_list

列出 MCP Server 连接状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "server_name": {
      "type": "string",
      "description": "Filter by server name. Empty = all servers."
    }
  }
}
```

**返回示例：**

```json
{
  "mcp_servers": [
    {
      "name": "filesystem",
      "transport": "stdio",
      "status": "connected",
      "tools_count": 4,
      "tools": ["read_file", "write_file", "list_dir", "search_files"],
      "last_ping": "2026-07-15T17:11:30Z",
      "uptime_seconds": 3600
    }
  ]
}
```

#### 6.5.10 log_query

查询运行时日志，支持级别、时间范围、关键词过滤。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "level": {
      "type": "string",
      "enum": ["debug", "info", "warn", "error"],
      "default": "info"
    },
    "since": {
      "type": "string",
      "description": "Start time (RFC3339 or relative like '1h', '30m')"
    },
    "until": {
      "type": "string",
      "description": "End time (RFC3339 or relative). Empty = now."
    },
    "keyword": {
      "type": "string",
      "description": "Keyword to filter log messages"
    },
    "component": {
      "type": "string",
      "description": "Filter by component (e.g. 'tool', 'provider', 'agent')"
    },
    "limit": {
      "type": "integer",
      "default": 50,
      "max": 500
    }
  }
}
```

**返回内容：**

- 匹配的日志条目列表（时间、级别、组件、消息、附加字段）
- 按 `limit` 截断，超限时返回总数和是否截断的标记
- 支持相对时间（如 `since: "1h"` 表示最近 1 小时）

#### 6.5.11 metric_query

查询运行时指标。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "metric": {
      "type": "string",
      "description": "Metric name. Empty = return all available metric names.",
      "default": ""
    },
    "since": {
      "type": "string",
      "description": "Start time for time-series data"
    },
    "until": {
      "type": "string",
      "description": "End time. Empty = now."
    },
    "step": {
      "type": "string",
      "description": "Aggregation step for time-series (e.g. '1m', '5m', '1h')",
      "default": "5m"
    },
    "labels": {
      "type": "object",
      "description": "Label filters, e.g. {\"tool\": \"shell\"}"
    }
  }
}
```

**可用指标：**

| 指标 | 类型 | 说明 |
|------|------|------|
| `tool.calls_total` | Counter | Tool 调用总次数 |
| `tool.calls_duration` | Histogram | Tool 执行耗时分布 |
| `tool.calls_errors` | Counter | Tool 执行错误次数 |
| `tool.concurrent` | Gauge | 当前并发执行数 |
| `provider.requests_total` | Counter | Provider 请求总数 |
| `provider.requests_duration` | Histogram | Provider 请求耗时 |
| `provider.tokens_input` | Counter | 输入 Token 总数 |
| `provider.tokens_output` | Counter | 输出 Token 总数 |
| `provider.errors` | Counter | Provider 错误次数 |
| `session.active` | Gauge | 活跃 Session 数 |
| `runtime.memory_alloc` | Gauge | 内存分配量 |
| `runtime.goroutines` | Gauge | Goroutine 数量 |


### 6.6 管理系列工具

管理系列工具让 Agent 能够执行**管理操作**——安装/卸载 Skill、探测 Provider 健康状态等。

#### 6.6.1 skill_install

安装一个 Skill。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "source": {
      "type": "string",
      "description": "Skill source: local path, git URL, or registry name"
    },
    "name": {
      "type": "string",
      "description": "Skill name (for registry lookup). Optional if source is a direct path/URL."
    },
    "version": {
      "type": "string",
      "description": "Specific version to install. Empty = latest."
    },
    "auto_bind": {
      "type": "boolean",
      "default": true,
      "description": "If true, automatically bind the skill to the calling agent."
    }
  },
  "required": ["source"]
}
```

**行为：**

- 从本地路径、Git URL 或 Registry 下载 Skill
- 解压到 skills 目录
- 加载 Skill 的 Tool 和配置
- `auto_bind=true` 时自动将 Skill 的 Tool 绑定到当前 Agent

#### 6.6.2 skill_uninstall

卸载一个 Skill。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Skill name to uninstall"
    },
    "force": {
      "type": "boolean",
      "default": false,
      "description": "If true, uninstall even if bound to active agents."
    },
    "keep_files": {
      "type": "boolean",
      "default": false,
      "description": "If true, keep skill files on disk, only unregister."
    }
  },
  "required": ["name"]
}
```

**行为：**

- 从 Tool Manager 注销 Skill 提供的所有 Tool
- 从所有 Agent 解绑
- `force=false` 时，如果 Skill 绑定到活跃 Agent 则拒绝卸载
- `keep_files=false` 时删除 Skill 目录

#### 6.6.3 skill_enable / skill_disable

启用或禁用一个已安装的 Skill。

**Parameters Schema (skill_enable)：**

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Skill name to enable"
    },
    "agent_id": {
      "type": "string",
      "description": "Agent ID to bind. Empty = all agents."
    }
  },
  "required": ["name"]
}
```

**Parameters Schema (skill_disable)：**

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Skill name to disable"
    },
    "agent_id": {
      "type": "string",
      "description": "Agent ID to unbind from. Empty = all agents."
    }
  },
  "required": ["name"]
}
```

**行为：**

- `skill_enable` — 注册 Skill 的 Tool 到 Tool Manager，绑定到指定 Agent
- `skill_disable` — 从指定 Agent 解绑，注销 Tool（但保留 Skill 文件和配置）
- 禁用是运行时操作，不修改磁盘配置

#### 6.6.4 provider_health

主动探测 Provider 健康状态。

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "provider": {
      "type": "string",
      "description": "Provider name to check. Empty = check all providers."
    },
    "timeout": {
      "type": "integer",
      "description": "Probe timeout in seconds",
      "default": 10
    }
  }
}
```

**返回示例：**

```json
{
  "providers": [
    {
      "name": "deepseek",
      "status": "healthy",
      "latency_ms": 234,
      "models_available": ["deepseek-chat", "deepseek-reasoner"],
      "rate_limit_remaining": 45,
      "rate_limit_reset": "2026-07-15T17:12:00Z",
      "error": null
    },
    {
      "name": "openai",
      "status": "unhealthy",
      "latency_ms": 0,
      "error": "connection timeout"
    }
  ]
}
```

**行为：**

- 向 Provider 发送轻量级探测请求（如 `/models` 端点）
- 记录响应延迟
- 检查速率限制头
- 更新 Provider 的健康状态缓存
- 可作为故障转移的决策依据

### 6.7 内置 Tool 安全策略汇总

| Tool | 安全机制 |
|------|---------|
| `shell` | 命令白/黑名单、工作目录限制、输出截断 |
| `http` | 域名白/黑名单、重定向限制、响应截断 |
| `file_read/write/list/delete` | 路径白/黑名单、文件大小限制 |
| `config_query` | 敏感字段自动脱敏 |
| `config_set` | 路径白/黑名单、persist 开关、类型校验 |
| `config_reload` | 路径限制 |
| `config_scheme` | 只读，无风险 |
| `config_save` | 路径限制、密钥自动转引用、自动备份 |
| `config_diff` | 只读，无风险 |
| `runtime_status` | `detail=full` 需要更高权限 |
| `agent_inspect` | `include_context=true` 需要更高权限 |
| `session_inspect` | `include_tool_results=true` 需要更高权限 |
| `log_query` | 敏感日志脱敏 |
| `metric_query` | 只读，无风险 |
| `skill_install/uninstall` | 默认禁用，需显式启用 |
| `skill_enable/disable` | 需要管理权限 |
| `provider_health` | 只读探测，无风险 |

---

