# MCP 与其他模块集成

> 文档路径: `docs/mcp/integration.md`
> 上级: `docs/mcp/README.md`

---

## 1. 概述

MCP 在 Yaa! 中并非孤立模块——它深度嵌入 Runtime 的初始化链路、Tool 注册体系、Agent 执行循环和 Config 加载流程。本文档描述 MCP 与四大核心模块的集成方式。

Yaa! 的初始化顺序为：

```text
Config → Storage → Provider → Memory → Tool → Skill → MCP →
Session → Context → Planner → Agent → Auth → API → Runtime Ready
```

MCP 排在 Tool 和 Skill 之后初始化，这保证了 MCP Client 可以将外部 Server 的 Tool 直接注册到已经就绪的 Tool Manager 中。

---

## 2. 与 Tool Manager 集成

### 2.1 外部 MCP Tool → Yaa! Tool 适配

MCP Server 暴露的 Tool 通过适配器（Adapter）转换为 Yaa! 的 `Tool` 接口，注册到 Tool Manager 后与内置 Tool 无差别使用。

```go
// MCPToolAdapter 将 MCP Server 的 Tool 适配为 Yaa! Tool 接口。
type MCPToolAdapter struct {
    serverName string   // MCP Server 名称，用作命名空间前缀
    toolName   string   // MCP Tool 原始名称
    schema     json.RawMessage // MCP Tool 的 JSON Schema
    client     *mcp.Client    // 关联的 MCP Client 连接
}

func (a *MCPToolAdapter) Name() string {
    return fmt.Sprintf("mcp.%s.%s", a.serverName, a.toolName)
}

func (a *MCPToolAdapter) Description() string {
    // 从 MCP Tool 元数据中获取描述
    return a.client.GetToolDescription(a.serverName, a.toolName)
}

func (a *MCPToolAdapter) Parameters() ToolSchema {
    return ToolSchema{JSONSchema: a.schema}
}

func (a *MCPToolAdapter) Execute(ctx context.Context, params map[string]any) (ToolResult, error) {
    // 将参数转发给 MCP Server 执行
    rawResult, err := a.client.CallTool(ctx, a.serverName, a.toolName, params)
    if err != nil {
        return ToolResult{IsError: true, Content: fmt.Sprintf("MCP 调用失败: %v", err)}, nil
    }
    return ToolResult{Content: rawResult.Content}, nil
}
```

### 2.2 注册流程

MCP Manager 初始化时，连接所有配置的 MCP Server，枚举其 Tool，逐一注册到 Tool Manager：

```go
func (m *Manager) Init(toolMgr *tool.Manager) error {
    for _, serverCfg := range m.config.Servers {
        client, err := m.connect(serverCfg)
        if err != nil {
            m.logger.Warn("MCP server 连接失败，跳过", "server", serverCfg.Name, "err", err)
            continue
        }
        m.clients[serverCfg.Name] = client

        // 枚举 Server 暴露的 Tool
        tools, err := client.ListTools(ctx)
        if err != nil {
            m.logger.Warn("MCP tool 枚举失败", "server", serverCfg.Name, "err", err)
            continue
        }

        // 将每个 MCP Tool 注册为 Yaa! Tool
        for _, t := range tools {
            adapter := &MCPToolAdapter{
                serverName: serverCfg.Name,
                toolName:  t.Name,
                schema:     t.InputSchema,
                client:     client,
            }
            toolMgr.Register(adapter, tool.ToolConfig{
                Enabled: true,
                Timeout: 60 * time.Second,
            })
            m.logger.Info("MCP Tool 已注册", "tool", adapter.Name())
        }
    }
    return nil
}
```

### 2.3 命名空间与 ToolInfo

MCP Tool 在 Tool Manager 中使用 `mcp.<server>.<tool>` 命名格式，避免与内置 Tool 冲突。

| 字段 | 值 |
|------|-----|
| `ToolInfo.Name` | `mcp.filesystem.read_file` |
| `ToolInfo.Source` | `"mcp"` |
| `ToolInfo.Description` | MCP Server 提供的描述 |
| `ToolInfo.Parameters` | MCP Server 返回的 JSON Schema |

Agent 的 Tool 白名单中可以直接引用 MCP Tool：

```yaml
agents:
  - id: "fs-agent"
    tools: ["mcp.filesystem.read_file", "mcp.filesystem.write_file"]
```

---

## 3. 与 Agent Loop 集成

### 3.1 执行循环中的 MCP Tool 调用

MCP Tool 注册到 Tool Manager 后，Agent Loop 无需感知 MCP 的存在——所有 Tool 调用统一走 `ToolManager.Execute` / `ExecuteBatch`。

```text
Agent Loop (每轮迭代):
  │
  ├─ 1. 获取 Agent 可用 Tool 列表
  │     └─ ToolManager.ListForAgent(agentID)
  │     └─ 结果包含内置 Tool + 插件 Tool + MCP Tool
  │
  ├─ 2. 转换为 ToolDef，注入 ChatRequest
  │     └─ ToolManager.ToToolDefs(toolNames)
  │     └─ MCP Tool 的 JSON Schema 直接透传
  │
  ├─ 3. 调用 Provider (LLM)
  │     └─ LLM 看到统一的 Tool 定义，不区分来源
  │
  ├─ 4. LLM 返回 Tool Call
  │     └─ FinishReason = "tool_calls"
  │
  ├─ 5. 执行 Tool
  │     └─ ToolManager.ExecuteBatch(ctx, agentID, toolCalls)
  │     └─ MCP Tool → MCPToolAdapter.Execute → MCP Client → 外部 Server
  │     └─ 内置 Tool → 直接本地执行
  │
  ├─ 6. 将结果注入 Context
  │     └─ 每个 ToolCall 生成 Role="tool" 的 Message
  │
  └─ 7. 回到步骤 3
```

### 3.2 MCP Tool 调用的错误处理

```go
func (a *MCPToolAdapter) Execute(ctx context.Context, params map[string]any) (ToolResult, error) {
    // 设置 MCP 调用超时
    callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    rawResult, err := a.client.CallTool(callCtx, a.serverName, a.toolName, params)
    if err != nil {
        // MCP 连接断开 — 尝试重连一次
        if isConnectionError(err) {
            if reconnErr := a.client.Reconnect(callCtx); reconnErr == nil {
                rawResult, err = a.client.CallTool(callCtx, a.serverName, a.toolName, params)
            }
        }
        if err != nil {
            return ToolResult{
                IsError: true,
                Content: fmt.Sprintf("[MCP] %s 调用失败: %v", a.Name(), err),
            }, nil
        }
    }
    return ToolResult{Content: rawResult.Content}, nil
}
```

| 错误类型 | 处理策略 |
|----------|----------|
| 连接断开 | 自动重连 1 次后重试 |
| 超时 | 返回 `IsError=true`，Agent Loop 继续下一轮 |
| 参数校验失败 | 由 Tool Manager 统一拦截，不转发到 MCP Server |
| Server 返回错误 | 透传错误内容到 LLM，由 LLM 决定后续行动 |

---

## 4. 与 Config 集成

### 4.1 配置结构

MCP Server 的配置在 `yaa.yaml` 的 `mcp` 段定义，由 Config Manager 加载后传递给 MCP Manager：

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"
      env:
        NODE_ENV: "production"

    - name: "github"
      transport: "sse"
      url: "http://localhost:3001/sse"
      timeout: 30s

    - name: "remote-tools"
      transport: "websocket"
      url: "ws://10.0.0.5:8080/mcp"
      headers:
        Authorization: "Bearer ${MCP_TOKEN}"
      reconnect:
        enabled: true
        interval: 5s
        max_attempts: 10
```

### 4.2 配置加载流程

```text
Config Manager
  │
  ├─ 1. 读取 yaa.yaml → 解析 mcp.servers[]
  │
  ├─ 2. 环境变量替换
  │     └─ ${MCP_TOKEN} → os.Getenv("MCP_TOKEN")
  │
  ├─ 3. 配置校验
  │     ├─ name 必填且唯一
  │     ├─ transport 必须为 stdio | sse | websocket
  │     └─ stdio 类型必须有 command
  │
  ├─ 4. 合并默认值
  │     └─ timeout 默认 30s
  │     └─ reconnect.enabled 默认 false
  │
  └─ 5. 传递给 MCP Manager
        └─ mcpManager.Init(config.MCP, toolMgr)
```

### 4.3 配置热更新

Config Manager 检测到 `mcp.servers` 变更时，通知 MCP Manager 执行增量更新：

```go
func (m *Manager) OnConfigUpdate(newCfg *config.MCPConfig) {
    oldNames := m.listServerNames()
    newNames := make(map[string]bool)
    for _, s := range newCfg.Servers {
        newNames[s.Name] = true
    }

    // 移除已删除的 Server — 先从 Tool Manager 注销其 Tool
    for _, name := range oldNames {
        if !newNames[name] {
            m.unregisterServerTools(name)
            m.clients[name].Close()
            delete(m.clients, name)
            m.logger.Info("MCP Server 已移除", "server", name)
        }
    }

    // 新增或更新的 Server — 重连并重新注册 Tool
    for _, s := range newCfg.Servers {
        if m.clients[s.Name] == nil || configChanged(m.configs[s.Name], s) {
            if m.clients[s.Name] != nil {
                m.unregisterServerTools(s.Name)
                m.clients[s.Name].Close()
            }
            m.connectAndRegister(s)
            m.logger.Info("MCP Server 已更新", "server", s.Name)
        }
    }
}
```

---

## 5. 完整集成流程图

```text
 Runtime 初始化
 ─────────────────────────────────────────────────────────────────
 Config          Storage        Provider       Memory
   │               │               │              │
   ▼               ▼               ▼              ▼
 Tool Manager ──► Skill Manager ──► MCP Manager ──► Session ──► ... ──► Ready
   │                                  │
   │  (已注册内置 Tool)                │  connect servers
   │                                  │  enumerate MCP Tools
   │  ◄────────────────────────────────┘  register as Tool
   │
   │  Tool Manager 现在持有:
   │    builtin tools + plugin tools + MCP tools
   │
   ▼
 Agent Loop (运行时)
 ─────────────────────────────────────────────────────────────────
   │
   ├─ ListForAgent(agentID) → [shell, http, mcp.fs.read_file, ...]
   ├─ ToToolDefs(...) → 注入 ChatRequest.Tools
   ├─ Provider.Chat(ctx, request) → LLM 返回 Tool Call
   ├─ ExecuteBatch(ctx, agentID, toolCalls)
   │    ├─ shell → 本地执行
   │    └─ mcp.fs.read_file → MCPToolAdapter → MCP Client → 外部 Server
   ├─ 将 Tool 结果注入 Context
   └─ 回到 Provider.Chat (下一轮)
```

---

## 6. 集成关系总结

| 集成对象 | 集成方式 | 关键接口 |
|----------|----------|----------|
| Tool Manager | MCP Tool 适配为 `Tool` 接口并注册 | `tool.Manager.Register()` |
| Agent Loop | 通过 Tool Manager 统一调度，无需感知 MCP | `ToolManager.ExecuteBatch()` |
| Config | `mcp.servers[]` 配置由 Config Manager 加载 | `config.MCPConfig` |
| Config 热更新 | 监听配置变更，增量增删 Server 和 Tool | `Manager.OnConfigUpdate()` |

MCP 的设计哲学是**透明集成**：外部 MCP Tool 注册后，对 Agent Loop、Provider 和 Context Manager 完全透明，它们只与统一的 Tool 接口交互。这保证了 MCP 能力的加入不会引入任何特殊路径，降低了系统复杂度。

---

*最后更新: 2025-07-17*
