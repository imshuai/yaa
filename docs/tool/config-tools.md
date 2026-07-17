# 内置 Tool — 配置管理类

> 文档路径: docs/tool/config-tools.md
> 上级: docs/tool/builtin.md 6.4

---

### 6.4 Config 系列工具

Config 系列工具让 Agent（以及通过 Remote API 调用的外部客户端）能够在运行时查询、修改、持久化和重载 Yaa! 的配置，实现**自我配置**能力。

#### 6.4.1 config_query

查询当前运行时配置信息。

```go
type ConfigQueryTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Config path to query, dot-separated. e.g. 'provider.default', 'tools.builtin.shell'. Empty returns full config.",
      "default": ""
    },
    "redact_secrets": {
      "type": "boolean",
      "description": "Whether to redact secret values (API keys, passwords). Default true.",
      "default": true
    }
  }
}
```

**行为：**

- `path` 为空时返回完整配置树
- `path` 指定具体路径时返回该节点的值
- `redact_secrets=true`（默认）时，敏感字段（如 `api_key`, `password`, `secret`）以 `***` 替代
- 返回 JSON 格式的配置快照

**安全策略：**

- 敏感字段自动脱敏，防止 API Key 泄露到 LLM Context
- 可通过 `config_query.redact_patterns` 配置自定义脱敏字段名

#### 6.4.2 config_set

修改配置项并实时生效（内存层）。

```go
type ConfigSetTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Config path to set, dot-separated. e.g. 'provider.default.timeout'"
    },
    "value": {
      "description": "New value for the config path. Type must match schema.",
      "anyOf": [
        {"type": "string"},
        {"type": "integer"},
        {"type": "number"},
        {"type": "boolean"},
        {"type": "array"},
        {"type": "object"},
        {"type": "null"}
      ]
    },
    "persist": {
      "type": "boolean",
      "description": "If true, also persist to config file. Default false.",
      "default": false
    }
  },
  "required": ["path", "value"]
}
```

**行为：**

- 修改内存中的配置值，立即生效（如修改 `provider.default.timeout` 会影响后续请求）
- `persist=true` 时同时调用 `config_save` 逻辑写入磁盘
- 修改前通过 `config_scheme` 校验值类型和取值范围
- 修改后触发对应的 `OnChange` 回调（如 Provider 超时变更会刷新 HTTP Client）

**安全策略：**

```yaml
tools:
  builtin:
    config_set:
      enabled: true
      options:
        # 允许修改的配置路径前缀（空=全部允许）
        allowed_paths: []
        # 禁止修改的配置路径前缀
        blocked_paths:
          - "runtime.security"      # 安全相关配置不可通过 Tool 修改
          - "tools.builtin.config"  # 防止递归修改自身配置
        # 是否允许 persist
        allow_persist: true
```

**错误处理：**

| 场景 | 行为 |
|------|------|
| 路径不存在 | `IsError=true`，提示路径无效 |
| 值类型不匹配 | `IsError=true`，提示期望类型和实际类型 |
| 值超出取值范围 | `IsError=true`，提示有效范围 |
| 路径在 blocked_paths 中 | 返回硬错误 `ErrPermissionDenied` |

#### 6.4.3 config_reload

从配置文件重新加载配置。

```go
type ConfigReloadTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "mode": {
      "type": "string",
      "enum": ["merge", "overwrite"],
      "description": "merge: only update fields present in file. overwrite: replace entire config with file content.",
      "default": "merge"
    },
    "path": {
      "type": "string",
      "description": "Custom config file path. Empty uses default config path.",
      "default": ""
    }
  }
}
```

**行为：**

- `merge` 模式：仅更新配置文件中显式声明的字段，保留运行时修改的未声明字段
- `overwrite` 模式：用配置文件内容完全替换当前配置，运行时修改但未持久化的值会丢失
- 重载后触发所有注册的 `OnChange` 回调
- 返回重载摘要：哪些配置项发生了变更

**返回示例：**

```json
{
  "mode": "merge",
  "changed": ["provider.default.timeout", "tools.builtin.shell.timeout"],
  "unchanged": 42,
  "errors": []
}
```

#### 6.4.4 config_scheme

列出所有配置项的 Schema 信息：类型、默认值、取值范围、描述。

```go
type ConfigSchemeTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Config path to get scheme for. Empty returns full scheme.",
      "default": ""
    }
  }
}
```

**返回示例：**

```json
{
  "provider": {
    "default": {
      "type": "object",
      "description": "Default provider configuration",
      "properties": {
        "model": {
          "type": "string",
          "default": "deepseek-chat",
          "description": "Default model name",
          "enum": ["deepseek-chat", "deepseek-reasoner"]
        },
        "timeout": {
          "type": "duration",
          "default": "30s",
          "min": "1s",
          "max": "300s",
          "description": "Request timeout"
        },
        "temperature": {
          "type": "number",
          "default": 0.7,
          "min": 0.0,
          "max": 2.0,
          "description": "Sampling temperature"
        }
      }
    }
  }
}
```

**用途：**

- `config_set` 前查询有效值范围
- Agent 自我探索可用配置项
- 配置文档自动化生成

#### 6.4.5 config_save

将当前运行时配置持久化到配置文件。

```go
type ConfigSaveTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Target file path. Empty uses default config path.",
      "default": ""
    },
    "format": {
      "type": "string",
      "enum": ["yaml", "json", "toml"],
      "default": "yaml"
    },
    "include_defaults": {
      "type": "boolean",
      "description": "If true, include all default values. If false, only save non-default values.",
      "default": false
    },
    "backup": {
      "type": "boolean",
      "description": "If true, backup existing file before overwriting.",
      "default": true
    }
  }
}
```

**行为：**

- `include_defaults=false`（默认）时只保存与默认值不同的配置项，保持配置文件简洁
- `backup=true`（默认）时自动备份原文件（如 `config.yaml.bak.20260715`）
- 保存前自动脱敏检查：不会将明文密钥写入文件，密钥以 `${ENV_VAR}` 引用形式保存

**安全策略：**

- 写入前验证目标路径在 `config_save.allowed_paths` 范围内
- 密钥字段自动转换为环境变量引用

#### 6.4.6 config_diff

对比运行时配置与磁盘配置文件的差异。

```go
type ConfigDiffTool struct {
    configManager *config.Manager
}
```

**Parameters Schema：**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Config file path to compare against. Empty uses default config path.",
      "default": ""
    }
  }
}
```

**返回示例：**

```json
{
  "changed": [
    {
      "path": "provider.default.timeout",
      "runtime": "60s",
      "file": "30s"
    },
    {
      "path": "tools.builtin.shell.timeout",
      "runtime": "120s",
      "file": "60s"
    }
  ],
  "only_in_runtime": ["agents.0.tools_config.shell.options.working_dir"],
  "only_in_file": [],
  "identical": 38
}
```

**用途：**

- `config_save` 前预览将要持久化的变更
- `config_reload` 后确认生效情况
- 排查配置不一致问题

