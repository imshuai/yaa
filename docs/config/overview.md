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
- 运行时配置修改（通过 Config Tool `config_set`）
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
- `config_query` 和 `log_query` 等工具默认对敏感字段脱敏
- 配置文件权限建议 `0600`（仅所有者可读写）

### 2.4 热更新

- 支持配置文件监听，变更后自动重新加载
- 热更新通过 `OnChange` 回调机制传播到相关组件
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
    // 当前版本: "1"
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
}
```

### 3.2 Effective Config

"Effective Config" 是指经过多源合并、环境变量展开和默认值注入后的最终配置。所有子系统读取的都是 Effective Config，而非原始配置文件内容。

```text
原始配置文件 (config.yaml)
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
    └─ → Config Tool 读写 (config_query / config_set)
```

### 3.3 配置路径

配置路径使用点分隔的字符串表示法，用于：

- Remote API 查询特定配置项
- Config Tool 的 `path` 参数
- 校验错误定位

```text
runtime.storage.type           → "sqlite"
runtime.api.http.addr          → ":8080"
agents[0].model                → "gpt-4o"
providers[0].api_key           → "${OPENAI_API_KEY}"
tools.builtin.shell.timeout    → 30s
```

### 3.4 配置作用域

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
├── merger.go        # 默认值合并 + 多源覆盖
├── watcher.go       # 文件监听 + 热更新
├── migrate.go       # 配置版本迁移
└── path.go          # 配置路径解析（点分隔 → 结构体字段）
```

---

*最后更新: 2025-07-16*