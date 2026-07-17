# Tool Manager

> 文档路径: `docs/tool/manager.md`
> 上级: `docs/tool/README.md` §3-4

---

## 3. Tool Manager

### 3.1 职责

Tool Manager 是 Tool 系统的中枢，负责：

1. **注册** — 内置 Tool 启动注册、自定义 Tool 动态注册
2. **发现** — 列出所有可用 Tool 及其 Schema
3. **查找** — 按名称查找 Tool 实例
4. **权限** — 检查 Agent 是否有权使用某 Tool
5. **执行** — 调度 Tool 执行（含超时、并发、结果截断）
6. **转换** — 将 Tool 转换为 `ToolDef` 供 Provider 使用

### 3.2 接口定义

```go
// Manager 管理 Tool 的注册、查找和执行。
type Manager struct {
    tools    map[string]Tool           // name → Tool 实例
    configs  map[string]ToolConfig     // name → 配置
    mu       sync.RWMutex
    logger   *slog.Logger
}

// ToolConfig 是 Tool 的运行时配置。
type ToolConfig struct {
    Enabled  bool          `yaml:"enabled"`
    Timeout  time.Duration `yaml:"timeout"`
    MaxRetry int           `yaml:"max_retry"`
    // 厂商/Tool 特有的配置，如 Shell 的 allowed_commands
    Options  map[string]any `yaml:"options"`
}

// Register 注册一个 Tool。
func (m *Manager) Register(tool Tool, config ToolConfig) error

// Unregister 注销一个 Tool。
func (m *Manager) Unregister(name string) error

// Get 按名称查找 Tool。
func (m *Manager) Get(name string) (Tool, error)

// List 列出所有已注册的 Tool。
func (m *Manager) List() []ToolInfo

// ListForAgent 列出 Agent 可用的 Tool（应用权限过滤）。
func (m *Manager) ListForAgent(agentID string) []ToolInfo

// CheckPermission 检查 Agent 是否有权使用某 Tool。
func (m *Manager) CheckPermission(agentID, toolName string) bool

// ToToolDefs 将指定 Tool 列表转换为 Provider 可用的 ToolDef 列表。
func (m *Manager) ToToolDefs(toolNames []string) ([]provider.ToolDef, error)

// Execute 执行指定 Tool。
func (m *Manager) Execute(ctx context.Context, agentID, toolName string, params map[string]any) (ToolResult, error)

// ExecuteBatch 并发执行多个 Tool Call。
func (m *Manager) ExecuteBatch(ctx context.Context, agentID string, calls []provider.ToolCall) ([]ToolResult, error)
```

### 3.3 ToolInfo

```go
// ToolInfo 是 Tool 的元信息，用于列表展示。
type ToolInfo struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
    Enabled     bool           `json:"enabled"`
    Source      string          `json:"source"`  // "builtin" | "plugin" | "mcp"
}
```

### 3.4 注册流程

```text
启动时:
  │
  ├─ 1. 注册内置 Tool
  │     ├─ 通用执行类 (Shell, HTTP, File)
  │     │   └─ 从 config.yaml 读取 tools.builtin.* 配置
  │     │      └─ enabled=false 的跳过
  │     │
  │     ├─ 配置管理类 (Config Query/Set/Reload/Scheme/Save/Diff)
  │     │   └─ 依赖 Config Manager 初始化完成
  │     │   └─ config_set 默认启用，其他默认启用
  │     │   └─ 应用 blocked_paths 安全策略
  │     │
  │     └─ 内视与管理类 (Runtime/Agent/Session/Tool/Skill/Provider/MCP/Log/Metric/Skill管理/Provider探测)
  │         └─ 依赖 Runtime、Agent Manager、Session Manager 等组件初始化完成
  │         └─ 只读内视工具默认启用
  │         └─ 管理类工具 (skill_install/uninstall/enable/disable) 默认禁用
  │
  ├─ 2. 注册插件 Tool
  │     └─ 从 config.yaml 读取 tools.plugins.* 配置
  │        └─ 加载 .so 插件，调用 Register 注册
  │
  ├─ 3. 注册 MCP Tool
  │     └─ MCP Client 连接外部 Server
  │        └─ 将 MCP Tool 转换为 Yaa! Tool 注册
  │
  └─ 4. 应用 Agent 权限配置
        └─ 从 agents[].tools 读取白名单
           └─ 未配置白名单 = 可用所有 Tool
```

### 3.5 权限模型

```yaml
# Agent 级别 Tool 权限
agents:
  - id: "safe-agent"
    tools: ["http", "file_read"]    # 白名单：只能用这两个 Tool

  - id: "full-access-agent"
    tools: []                        # 空数组 = 可用所有已注册 Tool

  - id: "restricted-agent"
    tools:
      allow: ["file_read"]           # 白名单
      deny: ["shell"]                # 黑名单（优先级高于 allow）
```

**权限规则：**

| 配置 | 含义 |
|------|------|
| `tools: [...]` | 白名单模式，只能用列表中的 Tool |
| `tools: []` 或未配置 | 可用所有已注册 Tool |
| `tools.deny: [...]` | 黑名单，即使 allow 中有也被拒绝 |
| 未注册的 Tool | 永远不可用，无论权限配置如何 |

**权限检查流程：**

```text
ToolManager.Execute(agentID, toolName, params)
  │
  ├─ 1. 查找 Tool 实例 → 不存在则返回 ErrToolNotFound
  │
  ├─ 2. 检查 Tool 是否 Enabled → 否则返回 ErrToolDisabled
  │
  ├─ 3. 检查 Agent 权限
  │     ├─ Agent 有白名单 → toolName 必须在白名单中
  │     ├─ Agent 有黑名单 → toolName 不能在黑名单中
  │     └─ 无白名单 → 允许
  │
  ├─ 4. 参数校验（JSON Schema）
  │     └─ 不通过则返回 ErrInvalidParams（含校验错误详情）
  │
  ├─ 5. 执行 Tool（带超时 context）
  │
  └─ 6. 结果截断 + 返回
```

---

## 4. Tool 执行引擎

### 4.1 单次执行流程

```go
func (m *Manager) Execute(ctx context.Context, agentID, toolName string, params map[string]any) (ToolResult, error) {
    // 1. 查找 Tool
    tool, err := m.Get(toolName)
    if err != nil {
        return ToolResult{}, err
    }

    // 2. 权限检查
    if !m.CheckPermission(agentID, toolName) {
        return ToolResult{}, ErrPermissionDenied
    }

    // 3. 参数校验
    if err := validateParams(tool.Parameters(), params); err != nil {
        return ToolResult{IsError: true, Content: fmt.Sprintf("参数校验失败: %v", err)}, nil
    }

    // 4. 超时控制
    config := m.configs[toolName]
    timeout := config.Timeout
    if timeout == 0 {
        timeout = 30 * time.Second // 全局默认
    }
    execCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // 5. 重试
    var result ToolResult
    maxRetry := config.MaxRetry
    for attempt := 0; attempt <= maxRetry; attempt++ {
        result, err = tool.Execute(execCtx, params)
        if err == nil {
            break
        }
        // 不可重试的错误直接返回
        if !isRetryable(err) {
            break
        }
        // 指数退避
        if attempt < maxRetry {
            backoff := time.Duration(1<<attempt) * time.Second
            select {
            case <-execCtx.Done():
                return ToolResult{}, execCtx.Err()
            case <-time.After(backoff):
            }
        }
    }

    // 6. 结果截断
    result.Content = truncateResult(result.Content, MaxToolResultTokens)

    // 7. 记录日志
    m.logger.Info("tool executed",
        "tool", toolName,
        "agent", agentID,
        "error", err,
        "is_error", result.IsError,
    )

    return result, err
}
```

### 4.2 并发执行

当 LLM 在一轮中返回多个 Tool Call 时，Yaa! 并发执行：

```go
func (m *Manager) ExecuteBatch(ctx context.Context, agentID string, calls []provider.ToolCall) ([]ToolResult, error) {
    results := make([]ToolResult, len(calls))
    var wg sync.WaitGroup

    for i, call := range calls {
        wg.Add(1)
        go func(idx int, c provider.ToolCall) {
            defer wg.Done()

            // 解析参数
            var params map[string]any
            if err := json.Unmarshal([]byte(c.Function.Arguments), &params); err != nil {
                results[idx] = ToolResult{
                    IsError: true,
                    Content: fmt.Sprintf("参数解析失败: %v", err),
                }
                return
            }

            // 执行
            result, err := m.Execute(ctx, agentID, c.Function.Name, params)
            if err != nil {
                results[idx] = ToolResult{
                    IsError: true,
                    Content: fmt.Sprintf("Tool 执行失败: %v", err),
                }
                return
            }
            results[idx] = result
        }(i, call)
    }

    wg.Wait()
    return results, nil
}
```

**并发控制：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `tools.max_concurrent` | 5 | 同一 Agent 同时执行的最大 Tool 数 |
| `tools.max_concurrent_per_session` | 3 | 同一 Session 内最大并发 Tool 数 |

超过并发上限的 Tool Call 排队等待，而非直接拒绝。

### 4.3 超时与取消

```yaml
# 全局默认
tools:
  default_timeout: 30s
  max_timeout: 300s          # Tool 可配置的最大超时上限

  builtin:
    shell:
      timeout: 60s           # Shell 超时
    http:
      timeout: 30s
    file:
      timeout: 10s
```

**超时层次（优先级递增）：**

1. `tools.default_timeout` — 全局默认
2. `tools.builtin.<name>.timeout` — Tool 级别配置
3. Agent 配置覆盖 — `agents[].tools_config.<name>.timeout`
4. Session 运行时覆盖 — API 请求中指定

**取消传播：**

- Session 关闭 → context 取消 → 所有进行中的 Tool 执行被取消
- 用户中断对话 → 同上
- Tool 内部应尊重 context.Done()，及时退出
