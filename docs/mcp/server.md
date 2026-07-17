# MCP Server

> 将 Yaa! 的能力（Tool / Skill）通过 MCP 协议暴露给外部 MCP Client，使 Yaa! 作为一个标准 MCP Server 对外提供服务。

## 1. 概述

Yaa! Runtime 既可以作为 MCP Client 调用外部 MCP Server，也可以 **作为 MCP Server** 将自身的 Tool 和 Skill 暴露给其他 MCP Client（如 Claude Desktop、Cursor、其他 Agent 框架等）。

### 核心职责

| 职责 | 说明 |
|------|------|
| Tool 暴露 | 将 Yaa! 内部注册的 Tool 转换为 MCP Tool 格式 |
| Skill 暴露 | 将 Yaa! Skill 包装为 MCP Tool 暴露 |
| 请求路由 | 接收 MCP Client 的 `tools/call` 请求，转发到 Yaa! Runtime 执行 |
| 列表响应 | 响应 `tools/list` 请求，返回当前可用的 Tool 列表 |
| 协议适配 | 在 MCP 协议与 Yaa! 内部接口之间做格式转换 |

## 2. 架构位置

```
┌──────────────────────────────────────────────────┐
│              MCP Client (外部)                   │
│         Claude Desktop / Cursor / ...            │
└──────────────────┬───────────────────────────────┘
                   │ MCP Protocol (stdio / SSE / WS)
                   ▼
┌──────────────────────────────────────────────────┐
│              Yaa! MCP Server                      │
│  ┌─────────────┐  ┌──────────────┐  ┌────────┐ │
│  │ Tool Adapter │  │ Skill Adapter│  │ Router  │ │
│  └──────┬───────┘  └──────┬───────┘  └───┬────┘ │
│         │                 │              │       │
│         ▼                 ▼              │       │
└─────────┼─────────────────┼──────────────┼──────┘
          │                 │              │
          ▼                 ▼              ▼
┌──────────────────────────────────────────────────┐
│              Yaa! Runtime                         │
│   Tool Registry  │  Skill Manager  │  Executor    │
└──────────────────────────────────────────────────┘
```

## 3. 协议实现

### 3.1 支持的传输方式

| 传输方式 | 场景 | 说明 |
|----------|------|------|
| stdio | 本地集成 | Claude Desktop 等本地 Client 的默认方式 |
| SSE | 远程访问 | HTTP Server-Sent Events，单向流 |
| WebSocket | 远程双向 | 全双工通信，适合实时交互 |

### 3.2 支持的 MCP 方法

| MCP 方法 | Yaa! 内部映射 | 说明 |
|-----------|---------------|------|
| `initialize` | 握手协商 | 返回 Server 能力声明 |
| `tools/list` | `ToolRegistry.List()` | 返回所有已注册 Tool |
| `tools/call` | `Executor.Execute()` | 执行指定 Tool |
| `resources/list` | — | 预留，暂不实现 |
| `prompts/list` | — | 预留，暂不实现 |

## 4. Tool 暴露机制

### 4.1 Yaa! Tool → MCP Tool 映射

Yaa! 内部的 Tool 定义与 MCP Tool 格式存在字段映射关系：

| Yaa! Tool 字段 | MCP Tool 字段 | 说明 |
|-----------------|---------------|------|
| `name` | `name` | Tool 名称 |
| `description` | `description` | Tool 描述 |
| `input_schema` | `inputSchema` | JSON Schema 输入参数 |
| `output_schema` | `outputSchema` | JSON Schema 输出（MCP 扩展） |
| `metadata` | `annotations` | 额外元数据 |

### 4.2 Skill 作为 MCP Tool

Yaa! Skill 可以被包装为 MCP Tool 暴露：

```
Skill (SKILL.md + scripts)
    │
    ▼  SkillAdapter.Wrap()
MCP Tool {
    name: "skill.{skill_name}"
    description: skill.description
    inputSchema: skill.trigger_schema
}
```

Skill 被调用时，MCP Server 通过 `Executor` 启动 Skill 工作流，将执行结果转换为 MCP 响应格式。

## 5. 流程图

### 5.1 Tool 列表响应流程

```
MCP Client                Yaa! MCP Server              Yaa! Runtime
    │                          │                           │
    │── tools/list ───────────▶│                           │
    │                          │── ToolRegistry.List() ───▶│
    │                          │◀── tools []Tool ──────────│
    │                          │                           │
    │                          │  格式转换: Yaa! Tool →    │
    │                          │  MCP Tool JSON            │
    │                          │                           │
    │◀── tools/list result ────│                           │
    │    { tools: [...] }      │                           │
```

### 5.2 Tool 执行转发流程

```
MCP Client                Yaa! MCP Server              Yaa! Executor
    │                          │                           │
    │── tools/call ───────────▶│                           │
    │    { name, arguments }   │                           │
    │                          │                           │
    │                          │  解析 name + arguments     │
    │                          │── Execute(name, args) ───▶│
    │                          │                           │
    │                          │                           │  执行 Tool/Skill
    │                          │                           │  生成结果
    │                          │◀── result, error ─────────│
    │                          │                           │
    │                          │  转换为 MCP CallResult    │
    │                          │                           │
    │◀── tools/call result ────│                           │
    │    { content, isError }  │                           │
```

## 6. Go 代码示例

### 6.1 MCP Server 结构定义

```go
package mcp

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/imshuai/yaa/internal/tool"
    "github.com/imshuai/yaa/internal/skill"
    "github.com/imshuai/yaa/internal/executor"
)

// MCPServer 将 Yaa! 能力通过 MCP 协议暴露
type MCPServer struct {
    toolRegistry *tool.Registry
    skillMgr     *skill.Manager
    executor     *executor.Executor
    transport    Transport
}

// MCPTool 表示 MCP 协议中的 Tool 定义
type MCPTool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}

// MCPToolListResponse tools/list 的响应
type MCPToolListResponse struct {
    Tools []MCPTool `json:"tools"`
}

// MCPCallToolRequest tools/call 的请求
type MCPCallToolRequest struct {
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}

// MCPCallToolResult tools/call 的响应
type MCPCallToolResult struct {
    Content []MCPContent `json:"content"`
    IsError bool         `json:"isError,omitempty"`
}

// MCPContent 表示 Tool 执行返回的内容块
type MCPContent struct {
    Type string `json:"type"` // "text" | "image" | "resource"
    Text string `json:"text,omitempty"`
}
```

### 6.2 Tool 列表响应实现

```go
// ListTools 响应 MCP tools/list 请求，返回所有已注册的 Tool
func (s *MCPServer) ListTools(ctx context.Context) (*MCPToolListResponse, error) {
    // 1. 从 Runtime 获取所有已注册 Tool
    yaaTools := s.toolRegistry.List()

    mcpTools := make([]MCPTool, 0, len(yaaTools))
    for _, t := range yaaTools {
        mcpTools = append(mcpTools, MCPTool{
            Name:        t.Name,
            Description: t.Description,
            InputSchema: t.InputSchema,
        })
    }

    // 2. 将 Skill 也包装为 MCP Tool
    skills := s.skillMgr.List()
    for _, sk := range skills {
        mcpTools = append(mcpTools, MCPTool{
            Name:        fmt.Sprintf("skill.%s", sk.Name),
            Description: sk.Description,
            InputSchema: sk.TriggerSchema,
        })
    }

    return &MCPToolListResponse{Tools: mcpTools}, nil
}
```

### 6.3 Tool 执行转发实现

```go
// CallTool 响应 MCP tools/call 请求，转发到 Yaa! Executor 执行
func (s *MCPServer) CallTool(ctx context.Context, req MCPCallToolRequest) (*MCPCallToolResult, error) {
    // 1. 执行 Tool（适用于普通 Tool 和 Skill 包装的 Tool）
    result, err := s.executor.Execute(ctx, req.Name, req.Arguments)
    if err != nil {
        // 执行失败，返回 isError = true
        return &MCPCallToolResult{
            Content: []MCPContent{
                {Type: "text", Text: fmt.Sprintf("Tool execution failed: %v", err)},
            },
            IsError: true,
        }, nil
    }

    // 2. 将 Yaa! 执行结果转换为 MCP Content 格式
    content := []MCPContent{
        {Type: "text", Text: result.Output},
    }

    // 3. 如果结果包含图片，追加 image content
    if result.ImageBase64 != "" {
        content = append(content, MCPContent{
            Type: "image",
            Text: result.ImageBase64, // 实际为 base64 编码
        })
    }

    return &MCPCallToolResult{Content: content}, nil
}
```

### 6.4 启动 MCP Server

```go
// Start 启动 MCP Server，监听指定传输方式
func (s *MCPServer) Start(ctx context.Context, transportType string) error {
    switch transportType {
    case "stdio":
        s.transport = NewStdioTransport()
    case "sse":
        s.transport = NewSSETransport(":8080")
    case "websocket":
        s.transport = NewWebSocketTransport(":8080")
    default:
        return fmt.Errorf("unsupported transport: %s", transportType)
    }

    // 注册消息处理器
    s.transport.Handle("tools/list", func(ctx context.Context, raw json.RawMessage) (any, error) {
        return s.ListTools(ctx)
    })
    s.transport.Handle("tools/call", func(ctx context.Context, raw json.RawMessage) (any, error) {
        var req MCPCallToolRequest
        if err := json.Unmarshal(raw, &req); err != nil {
            return nil, err
        }
        return s.CallTool(ctx, req)
    })

    return s.transport.Serve(ctx)
}
```

## 7. 配置

在 Yaa! 配置文件中启用 MCP Server：

```json
{
  "mcp": {
    "server": {
      "enabled": true,
      "transport": "stdio",
      "expose_tools": true,
      "expose_skills": true,
      "tool_prefix": "",
      "exclude": ["internal.*"]
    }
  }
}
```

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 MCP Server |
| `transport` | string | `"stdio"` | 传输方式 |
| `expose_tools` | bool | `true` | 是否暴露 Tool |
| `expose_skills` | bool | `true` | 是否暴露 Skill |
| `tool_prefix` | string | `""` | Tool 名称前缀 |
| `exclude` | []string | `[]` | 排除的 Tool 名称模式 |

## 8. 安全与权限

- **Tool 过滤**：通过 `exclude` 配置排除内部 Tool，避免敏感操作暴露
- **权限继承**：MCP Server 执行 Tool 时继承 Yaa! Runtime 的权限上下文
- **调用审计**：所有来自 MCP Client 的 `tools/call` 请求记录到审计日志
- **并发控制**：MCP Server 复用 Yaa! Executor 的并发限制和超时机制
