# MCP 配置参考

> 本文档详细描述 Yaa! MCP Server 的配置字段、传输方式及使用示例。
> MCP（Model Context Protocol）是 Yaa! 原生支持的能力扩展协议。

---

## 1. 配置字段一览

MCP Server 配置定义在主配置文件的 `mcp.servers` 数组中，每个 Server 支持以下字段：

| 字段 | 类型 | 必填 | 默认值 | 说明 |
|------|------|:----:|--------|------|
| `name` | `string` | ✅ | — | MCP Server 唯一标识名称，用于注册与引用 |
| `transport` | `string` | ✅ | `stdio` | 传输协议，可选 `stdio` / `sse` / `ws` |
| `command` | `string` | ⚠️ | — | stdio 模式下启动 Server 子进程的命令（如 `npx`、`node`） |
| `args` | `[]string` | ❌ | `[]` | stdio 模式下传递给 command 的参数列表 |
| `url` | `string` | ⚠️ | — | SSE / WS 模式下 MCP Server 的连接地址 |
| `env` | `map[string]string` | ❌ | `{}` | stdio 模式下注入子进程的环境变量 |
| `timeout` | `duration` | ❌ | `30s` | 与 MCP Server 通信的超时时间 |
| `auto_start` | `bool` | ❌ | `true` | Runtime 启动时是否自动连接该 MCP Server |

> **⚠️ 条件必填：** `command` 在 `transport: stdio` 时必填；`url` 在 `transport: sse` 或 `ws` 时必填。

---

## 2. 传输方式说明

| 传输方式 | 字段要求 | 适用场景 |
|----------|----------|----------|
| `stdio` | `command` + `args` | 本地子进程，如 npx 启动的 npm 包 |
| `sse` | `url` | 远程 HTTP Server-Sent Events 端点 |
| `ws` | `url` | 远程 WebSocket 端点，双向实时通信 |

---

## 3. YAML 配置示例

### 3.1 stdio 模式（本地子进程）

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"
      env:
        NODE_ENV: "production"
      timeout: 30s
      auto_start: true

    - name: "sqlite"
      command: "uvx"
      args: ["mcp-server-sqlite", "--db-path", "./data/yaa.db"]
      transport: "stdio"
      timeout: 60s
```

### 3.2 SSE 模式（远程端点）

```yaml
mcp:
  servers:
    - name: "remote-tools"
      transport: "sse"
      url: "http://10.0.0.5:3001/sse"
      timeout: 60s
      auto_start: true
```

### 3.3 WebSocket 模式

```yaml
mcp:
  servers:
    - name: "ws-server"
      transport: "ws"
      url: "ws://10.0.0.5:3002/ws"
      timeout: 30s
```

### 3.4 多 Server 混合配置

```yaml
mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"

    - name: "remote-search"
      transport: "sse"
      url: "http://search-service.local:8080/sse"
      timeout: 45s

    - name: "debug-tools"
      transport: "stdio"
      command: "node"
      args: ["./tools/mcp-debug-server.js"]
      env:
        LOG_LEVEL: "debug"
      auto_start: false
```

---

## 4. Go 配置结构体

Yaa! 内部使用以下 Go 结构体表示 MCP Server 配置：

```go
package mcp

import "time"

// ServerConfig 定义单个 MCP Server 的配置。
type ServerConfig struct {
    Name      string            `yaml:"name"`       // 唯一标识名称
    Transport string            `yaml:"transport"`  // stdio | sse | ws
    Command   string            `yaml:"command"`    // stdio: 子进程命令
    Args      []string          `yaml:"args"`       // stdio: 命令参数
    URL       string            `yaml:"url"`        // sse/ws: 连接地址
    Env       map[string]string `yaml:"env"`        // stdio: 环境变量
    Timeout   time.Duration     `yaml:"timeout"`    // 通信超时
    AutoStart bool              `yaml:"auto_start"` // 是否自动启动
}

// Config 是 MCP 模块的整体配置。
type Config struct {
    Servers []ServerConfig `yaml:"servers"`
}

// Validate 校验单个 Server 配置的合法性。
func (s *ServerConfig) Validate() error {
    if s.Name == "" {
        return ErrNameRequired
    }
    switch s.Transport {
    case "stdio":
        if s.Command == "" {
            return ErrCommandRequired
        }
    case "sse", "ws":
        if s.URL == "" {
            return ErrURLRequired
        }
    default:
        return ErrUnsupportedTransport
    }
    return nil
}
```

---

## 5. 加载与启动流程

```go
package mcp

import (
    "context"
    "fmt"
    "log/slog"
)

// Manager 管理所有 MCP Server 的连接与生命周期。
type Manager struct {
    config  Config
    clients map[string]*Client // name → client
    logger  *slog.Logger
}

// NewManager 根据配置创建 MCP Manager。
func NewManager(cfg Config, logger *slog.Logger) *Manager {
    return &Manager{
        config:  cfg,
        clients: make(map[string]*Client),
        logger:  logger,
    }
}

// Start 启动所有 auto_start 为 true 的 MCP Server 连接。
func (m *Manager) Start(ctx context.Context) error {
    for _, sc := range m.config.Servers {
        if !sc.AutoStart {
            m.logger.Info("mcp server skipped (auto_start=false)", "name", sc.Name)
            continue
        }
        if err := m.startServer(ctx, &sc); err != nil {
            return fmt.Errorf("start mcp server %q: %w", sc.Name, err)
        }
    }
    return nil
}

// Stop 优雅关闭所有 MCP Server 连接。
func (m *Manager) Stop() error {
    for name, c := range m.clients {
        m.logger.Info("stopping mcp server", "name", name)
        if err := c.Close(); err != nil {
            m.logger.Error("close mcp server failed", "name", name, "err", err)
        }
    }
    return nil
}
```

---

## 6. 配置注意事项

| 注意点 | 说明 |
|--------|------|
| **名称唯一性** | 每个 Server 的 `name` 必须唯一，重复名称会导致启动失败 |
| **超时设置** | 网络环境较差时建议增大 `timeout`，避免误判连接失败 |
| **auto_start** | 设为 `false` 可延迟启动，需要时通过 Remote API 手动连接 |
| **环境变量** | `env` 仅对 stdio 模式有效，不会影响 SSE/WS 远程端点 |
| **敏感信息** | API Key 等敏感信息应通过 `${VAR_NAME}` 引用环境变量，而非明文写入配置 |
| **路径分隔符** | Windows 环境下 `args` 中的路径使用反斜杠 `\` 时需注意 YAML 转义 |

---

*最后更新: 2025-07-17*
