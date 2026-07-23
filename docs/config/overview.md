# 概述与设计理念

> 文档路径: docs/config/overview.md
> 上级: README.md

---

## 1. 配置系统定位

配置系统是 Yaa! Runtime 的第一个被初始化的子系统（参见架构 §3.1 初始化顺序：Config → Storage → ...），所有其他子系统都依赖配置系统提供的 `*config.Config` 实例。

```text
Runtime 启动
    │
    ▼
Config 加载（第一个）
    │
    ├─→ Storage  (type, path)
    ├─→ Provider (api_key, base_url)
    ├─→ Agent    (model, tools, skills)
    ├─→ Tool     (enabled, timeout)
    ├─→ MCP      (servers)
    ├─→ Auth     (tokens)
    └─→ API      (addr, ws, sse)
```

配置系统不仅提供静态配置读取，还承担：

- 运行时配置查询（通过 Remote API `GET /api/v1/config`）
- 运行时配置查询（通过只读 Config Tool `config_query`）
- 配置热更新（文件监听 → 自动重新加载 → 传播变更）
- 配置校验（加载时校验，拒绝非法配置启动）

---

## 2. 设计原则

### 2.1 Config over Code

架构设计目标明确：**优先通过配置扩展，而非修改代码**。

这意味着：
- 新增 Provider 只需在 `providers` 列表中添加一项
- 新增 Agent 只需在 `agents` 列表中添加一项
- 调整 Tool 超时只需修改 `tools.builtin.shell.timeout`
- 启用 MCP Server 只需在 `mcp.servers` 中添加一项

用户不需要编写任何 Go 代码来完成常见配置变更。

### 2.2 多源合并

配置来自多个来源，按优先级递增合并：

```text
默认值 ──→ 配置文件 ──→ 命令行参数
              │
              └──→ 环境变量引用（在配置文件值中展开）
```

**合并策略：** 按字段覆盖，而非整体替换。例如配置文件中只设置了 `runtime.api.http.addr`，未设置 `runtime.api.ws.enabled`，则后者使用默认值。

### 2.3 安全优先

- 敏感信息（API Key、Token、密码）通过 `${VAR_NAME}` 引用环境变量，不硬编码到配置文件
- 配置文件可以安全提交到版本控制系统（不含明文密钥）
- `config_query` 和 Remote Config API 强制使用统一递归脱敏视图
- 配置文件权限建议 `0600`（仅所有者可读写）

### 2.4 热更新

- 支持配置文件监听，变更后自动重新加载
- 热更新通过 `ReloadManager` 原子发布不可变 snapshot；组件在操作开始时读取一次
- 部分配置项支持热更新（如 Agent system_prompt），部分需要重启（如 storage type）
- 热更新失败时回滚到上一个有效配置

### 2.5 向后兼容

- 新增配置字段使用默认值，不破坏旧配置文件
- 废弃字段保留但标记 `deprecated`，加载时输出警告
- 配置版本号字段（`config_version`）支持自动迁移
- 迁移工具在启动时自动处理格式升级

---

## 3. 核心概念

### 3.1 Config 结构

```go
// Config 是 Yaa! 运行时的完整配置。
// 所有子系统从此结构中读取自己的配置。
type Config struct {
    // ConfigVersion 是配置格式版本号，用于迁移。
    // 当前版本: "1.0"
    ConfigVersion string `yaml:"config_version" json:"config_version"`

    // Runtime 是运行时核心配置。
    Runtime RuntimeConfig `yaml:"runtime" json:"runtime"`

    // Agents 是 Agent 列表配置。
    Agents []AgentConfig `yaml:"agents" json:"agents"`

    // Providers 是 LLM Provider 列表配置。
    Providers []ProviderConfig `yaml:"providers" json:"providers"`

    // MCP 是 MCP Server 配置。
    MCP MCPConfig `yaml:"mcp" json:"mcp"`

    // Tools 是 Tool 系统配置。
    Tools ToolsConfig `yaml:"tools" json:"tools"`

    // Skills 是 Skill 系统配置。
    Skills SkillsConfig `yaml:"skills" json:"skills"`

    // Memory 是 Memory 系统配置。
    Memory MemoryConfig `yaml:"memory" json:"memory"`

    // Session 是 Session 默认行为配置。
    Session SessionConfig `yaml:"session" json:"session"`

    // Context 是上下文窗口配置。
    Context ContextConfig `yaml:"context" json:"context"`

    // Planner 是规划与执行配置。
    Planner PlannerConfig `yaml:"planner" json:"planner"`

    // Plugins 是进程外插件配置。
    Plugins PluginsConfig `yaml:"plugins" json:"plugins"`

    // Log 是日志配置。
    Log LogConfig `yaml:"log" json:"log"`
}
```

### 3.2 Effective Config

"Effective Config" 是指经过多源合并、环境变量展开和默认值注入后的最终配置。所有子系统读取的都是 Effective Config，而非原始配置文件内容。

```text
原始配置文件 (yaa.yaml)
    │
    ├─ 环境变量展开 (${VAR} → 实际值)
    ├─ 默认值注入 (缺失字段填充默认值)
    ├─ 命令行参数覆盖
    │
    ▼
Effective Config (内存中的 *config.Config)
    │
    ├─ → 各子系统读取
    ├─ → Remote API 暴露 (GET /api/v1/config)
    └─ → Config Tool 只读/重载 (config_query / config_reload)
```

### 3.3 脱敏视图

Config 模块只提供一套对外视图；Remote API 和 `config_query` 不得各自维护字段列表：

```go
var ErrConfigRedactionFailed = errors.New("config: redaction failed")

// RedactedView 返回 canonical Config 的 JSON-compatible 深拷贝，不修改 cfg。
func RedactedView(cfg *Config) (any, error)
```

实现顺序固定：

1. 拒绝 nil；用 `encoding/json` 序列化 `cfg`，再以 `Decoder.UseNumber` 解码为 `map[string]any`。任何错误用 `%w` 包装 `ErrConfigRedactionFailed`。
2. 将 `runtime.auth.tokens[*].token`、`runtime.auth.jwt.secret`、`providers[*].api_key` 和 `memory.embedding.api_key` 的值替换为字符串 `"***"`。
3. 对 `mcp.servers[*].headers`、`mcp.servers[*].env` 及以下开放 Map 递归处理：object/array 保持结构，所有 string/bool/number scalar 替换为 `"***"`，`null` 保持 `null`。

```text
providers[*].extra
tools.builtin.*.options
agents[*].tools_config.*.options
skills.per_skill.*.options
agents[*].skills_config.*.options
plugins.entries[*].config
```

开放 Map 采用 fail-closed，而不是根据看起来像 Secret 的 key 猜测；新增开放 Map 时必须同时加入该列表。函数保留所有 canonical 字段、空 object/array 和非敏感值。`GET /api/v1/config` 与 `config_query` 都先从同一次 `reload.Current()` 取得 snapshot，再调用此函数；Tool 的 `path` 遍历只能发生在完整脱敏之后。

最小测试必须覆盖已知 Secret、MCP headers/env、开放 Map 中嵌套 object/array、`null`、输入不变性以及失败包装；两个入口对同一 snapshot 的完整视图必须深度相等。

### 3.4 配置路径

配置路径使用点分隔的字符串表示法，用于 Config Tool 的 `path` 参数和校验错误定位。Remote `GET /api/v1/config` 始终返回完整视图，不接受 path 参数。

```text
runtime.storage.type           → "sqlite"
runtime.api.http.addr          → "127.0.0.1:8080"
agents.0.model                 → "gpt-4o"
providers.0.api_key            → "***"  (脱敏视图)
tools.builtin.shell.timeout    → 30s
```

### 3.5 配置作用域

| 作用域 | 说明 | 示例 |
|--------|------|------|
| **全局** | 所有 Agent 共享的配置 | `runtime.*`, `tools.builtin.*` |
| **Provider 级** | 单个 LLM Provider 的配置 | `providers[0].api_key` |
| **Agent 级** | 单个 Agent 的配置 | `agents[0].model` |
| **Skill 级** | 单个 Skill 的配置 | `skills.per_skill.<name>.options` |

Agent 级配置可以覆盖全局配置中的对应部分。例如 Agent 级 `tools_config.shell.timeout` 覆盖全局 `tools.builtin.shell.timeout`。

---

## 4. 目录结构

```text
internal/config/
├── config.go        # Config 结构体定义 + Default()
├── loader.go        # 配置加载（多格式解析 + 多源合并）
├── envvar.go        # 环境变量引用展开 (${VAR_NAME})
├── validator.go     # 配置校验（必填、类型、范围检查）
├── defaults.go      # Default + ApplyElementDefaults
├── watcher.go       # 文件监听 + 热更新
├── migrate.go       # 配置版本迁移
└── redact.go        # 唯一 RedactedView 实现
```

---

*最后更新: 2026-07-22*
