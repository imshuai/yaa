# 内置 Tool — 通用执行类

> 文档路径: `docs/tool/builtin.md`
> 上级: `docs/tool/README.md` §6

---

## 6. 内置 Tool

Yaa! 的内置 Tool 分为三大类：

| 类别 | Tool 列表 | 说明 | 文件 |
|------|-----------|------|------|
| **通用执行** | `shell`, `http`, `file_read`, `file_write`, `file_list`, `file_delete` | 基础操作能力 | 本文件 |
| **配置管理** | `config_query`, `config_set`, `config_reload`, `config_scheme`, `config_save`, `config_diff` | 运行时配置管理 | [config-tools.md](config-tools.md) |
| **内视与管理** | `runtime_status`, `agent_list`, `agent_inspect`, `session_list`, `session_inspect`, `tool_list`, `skill_list`, `provider_list`, `mcp_list`, `log_query`, `metric_query`, `skill_install`, `skill_uninstall`, `skill_enable`, `skill_disable`, `provider_health` | 自我认知与管理 | [introspection.md](introspection.md) |

### 6.1 Shell

执行 Shell 命令，最强大的通用 Tool。

```go
type ShellTool struct {
    config ShellConfig
}

type ShellConfig struct {
    Timeout          time.Duration `yaml:"timeout"`
    AllowedCommands  []string      `yaml:"allowed_commands"`   // 命令白名单，空=全部允许
    BlockedCommands  []string      `yaml:"blocked_commands"`    // 命令黑名单
    WorkingDir       string        `yaml:"working_dir"`         // 工作目录
    Env              map[string]string `yaml:"env"`             // 环境变量
    MaxOutputBytes   int           `yaml:"max_output_bytes"`    // 输出截断
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "command": {
      "type": "string",
      "description": "The shell command to execute"
    },
    "timeout": {
      "type": "integer",
      "description": "Timeout in seconds (overrides default)",
      "default": 30
    },
    "working_dir": {
      "type": "string",
      "description": "Working directory for the command"
    }
  },
  "required": ["command"]
}
```

**安全策略：**

| 配置 | 说明 |
|------|------|
| `allowed_commands` | 白名单模式，只允许列出的命令前缀 |
| `blocked_commands` | 黑名单模式，禁止列出的命令前缀（如 `rm -rf`, `mkfs`） |
| 两者同时配置 | 黑名单优先 |

**输出处理：**

- stdout + stderr 合并返回
- 超过 `max_output_bytes` 时截断，尾部附加 `[output truncated]`
- 退出码非 0 时 `IsError=true`，Content 包含退出码和错误输出

### 6.2 HTTP

发送 HTTP 请求。

```go
type HTTPTool struct {
    config HTTPConfig
}

type HTTPConfig struct {
    Timeout        time.Duration `yaml:"timeout"`
    MaxRedirects   int           `yaml:"max_redirects"`
    AllowedDomains []string      `yaml:"allowed_domains"`   // 域名白名单
    BlockedDomains []string      `yaml:"blocked_domains"`   // 域名黑名单
    MaxResponseBytes int         `yaml:"max_response_bytes"`
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "method": {
      "type": "string",
      "enum": ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"],
      "default": "GET"
    },
    "url": {
      "type": "string",
      "description": "The request URL"
    },
    "headers": {
      "type": "object",
      "description": "Request headers"
    },
    "body": {
      "type": "string",
      "description": "Request body (for POST/PUT/PATCH)"
    },
    "timeout": {
      "type": "integer",
      "description": "Timeout in seconds"
    }
  },
  "required": ["url"]
}
```

**返回内容：**

```json
{
  "status_code": 200,
  "headers": {"content-type": "application/json"},
  "body": "...",
  "elapsed_ms": 342
}
```

### 6.3 File

文件操作 Tool，实际拆分为 4 个子 Tool：

| Tool 名称 | 功能 | Schema 关键参数 |
|-----------|------|----------------|
| `file_read` | 读取文件内容 | `path`, `encoding` (utf-8/base64), `max_bytes` |
| `file_write` | 写入文件 | `path`, `content`, `create_dirs` |
| `file_list` | 列出目录内容 | `path`, `recursive` |
| `file_delete` | 删除文件或空目录 | `path` |

**安全策略：**

```yaml
tools:
  builtin:
    file:
      enabled: true
      options:
        allowed_paths: ["/tmp", "/workspace"]  # 允许访问的路径前缀
        blocked_paths: ["/etc", "/root/.ssh"]   # 禁止访问的路径前缀
        max_file_size: "10MB"                    # 单次读写上限
```

**路径校验：**

```go
func validatePath(path string, allowed, blocked []string) error {
    abs, _ := filepath.Abs(path)

    // 检查黑名单
    for _, b := range blocked {
        if strings.HasPrefix(abs, b) {
            return fmt.Errorf("path %s is blocked", abs)
        }
    }

    // 检查白名单
    if len(allowed) > 0 {
        allowed_ := false
        for _, a := range allowed {
            if strings.HasPrefix(abs, a) {
                allowed_ = true
                break
            }
        }
        if !allowed_ {
            return fmt.Errorf("path %s is not in allowed paths", abs)
        }
    }

    // 防止路径穿越
    // 已通过 filepath.Abs + HasPrefix 隐式处理

    return nil
}
```
