# Yaa! 架构设计

> 本文件描述 Yet Another Agent (Yaa!) 的整体架构。
> 项目当前处于架构设计阶段，本文档会随设计迭代持续更新。

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| **Agent First** | Agent 是系统核心，LLM 只是 Agent 的一种能力 |
| **Runtime First** | Runtime 与客户端彻底解耦，Runtime 管理一切 |
| **Remote API First** | 所有能力通过统一 Remote API 暴露 |
| **Provider Independent** | 不绑定任何 LLM 厂商 |
| **Tool First** | Tool 遵循统一接口，可独立开发、安装、升级 |
| **Skill Oriented** | 复杂能力抽象为 Skill，组合多个 Tool |
| **Native MCP** | 原生支持 MCP，既是 Client 也是 Server |
| **Embedding Friendly** | 可独立运行、嵌入 Go 项目、容器化部署 |
| **Windows First** | 优先保证 Windows 7 x64 兼容，单一可执行文件 |
| **Config over Code** | 优先通过配置扩展，而非修改代码 |
| **Backward Compatible** | 稳定性优先于功能数量，保持 API 向后兼容 |

---

## 2. 整体架构

```text
┌──────────────────────────────────────────────────────────────┐
│                         Client                               │
│                                                              │
│  Desktop  WebUI  Mobile  IDE Plugin  CLI  HomeAssistant  ...  │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           │  Remote API
                           │  (HTTP / WebSocket / SSE)
                           │
┌──────────────────────────┴───────────────────────────────────┐
│                      Yaa! Runtime                            │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                    Agent Layer                          │  │
│  │  Agent Manager → Agent × N                              │  │
│  │  (生命周期 / 配置 / 调度)                                 │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│  ┌────────────────────────┴───────────────────────────────┐  │
│  │                   Session Layer                         │  │
│  │  Session Manager → Session × N                         │  │
│  │  (会话隔离 / 状态管理 / 多轮对话)                          │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│  ┌────────────────────────┴───────────────────────────────┐  │
│  │                   Context Layer                         │  │
│  │  Context Manager → 上下文窗口 / 压缩 / 截断               │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│  ┌────────────────────────┴───────────────────────────────┐  │
│  │                   Planner Layer                         │  │
│  │  Task 分解 / 执行计划 / 步骤编排                           │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│  ┌──────────┬──────────┬───┴────┬──────────┬───────────────┐  │
│  │  Tool    │  Skill   │ Memory │   MCP    │   Plugin     │  │
│  │ Registry │ Registry │ System │  Client  │   Manager    │  │
│  │          │          │        │  Server  │              │  │
│  └──────────┴──────────┴────────┴──────────┴───────────────┘  │
│                           │                                  │
│  ┌────────────────────────┴───────────────────────────────┐  │
│  │                  Provider Layer                        │  │
│  │  Provider Manager → OpenAI / Claude / Gemini / ...      │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │   Config      │  │   Storage    │  │     Auth         │   │
│  │  (YAML/TOML)  │  │ (SQLite/BBolt)│  │  (Token/Policy) │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

---

## 3. 核心模块

### 3.1 Runtime

Runtime 是整个系统的根容器，负责：

- 启动与停止所有子系统
- 管理子系统的依赖关系与初始化顺序
- 提供全局配置访问
- 提供健康检查端点
- 管理优雅关闭

```go
type Runtime struct {
    Config    *config.Config
    Agents    *agent.Manager
    Sessions  *session.Manager
    Tools     *tool.Manager
    Skills    *skill.Manager
    Providers *provider.Manager
    Memory    memory.Memory
    MCP       *mcp.Manager
    Storage   storage.Storage
    API       *api.Server
}
```

**初始化顺序：**

```text
Config → Storage → Provider → Memory → Tool → Skill → MCP →
Session → Context → Planner → Agent → Auth → API → Runtime Ready
```

---

### 3.2 Agent

Agent 是系统核心抽象，代表一个具备能力的智能体实例。

```go
type Agent struct {
    ID          string
    Name        string
    ProviderID  string          // 绑定的 LLM Provider
    SystemPrompt string
    Tools       []string        // 可用 Tool 列表
    Skills      []string        // 可用 Skill 列表
    Memory      memory.Memory   // 记忆系统
    Session     *session.Session // 当前会话
}
```

**Agent 生命周期：**

```text
Created → Configured → Running → Paused → Stopped → Destroyed
```

一个 Runtime 可以管理多个 Agent 实例，每个 Agent 可以：
- 绑定不同的 Provider
- 拥有不同的 Tool / Skill 集合
- 维护独立的 Session 和 Memory
- 拥有不同的 System Prompt 和权限

---

### 3.3 Session

Session 管理一次完整的交互上下文。

```go
type Session struct {
    ID        string
    AgentID   string
    Messages  []Message        // 消息历史
    State     SessionState     // Active / Paused / Closed
    CreatedAt time.Time
    UpdatedAt time.Time
    Metadata  map[string]any   // 自定义元数据
}
```

**特性：**
- 一个 Agent 可以有多个并发 Session
- Session 之间状态隔离
- Session 可持久化，重启后恢复
- 支持通过 Remote API 创建、查询、恢复、关闭 Session

---

### 3.4 Context

Context 负责管理传给 LLM 的上下文窗口。

**核心职责：**
- 收集 System Prompt + Session 消息 + Memory 摘要 + Tool 结果
- 根据目标 Provider 的 Token 限制进行截断或压缩
- 支持多种策略：滑动窗口、摘要压缩、重要性排序

```go
type ContextManager interface {
    Build(session *session.Session, opts ...ContextOption) (*Context, error)
    Compress(ctx *Context) (*Context, error)
    Truncate(ctx *Context, maxTokens int) (*Context, error)
}
```

---

### 3.5 Planner

Planner 负责将复杂任务分解为可执行步骤。

```go
type Planner interface {
    Plan(task string, agent *agent.Agent) (*Plan, error)
}

type Plan struct {
    Steps []Step
}

type Step struct {
    ID       string
    Action   string          // tool call / skill call / llm call
    Input    map[string]any
    Depends  []string        // 依赖的前置步骤
    Status   StepStatus      // Pending / Running / Done / Failed
}
```

**默认实现：** 简单的 LLM 驱动规划器，通过 Prompt 让模型输出执行计划。

---

### 3.6 Memory

Memory 系统管理 Agent 的记忆。

```go
type Memory interface {
    Add(key string, content string, metadata map[string]any) error
    Get(key string) (*MemoryItem, error)
    Search(query string, limit int) ([]*MemoryItem, error)
    Delete(key string) error
    Clear() error
}
```

**分层设计：**

| 层级 | 说明 | 存储方式 |
|------|------|----------|
| Short-term | 当前 Session 的消息历史 | 内存 / Storage |
| Long-term | 跨 Session 的持久记忆 | SQLite / 向量数据库 |
| Summary | Session 的摘要记忆 | Storage |

---

### 3.7 Tool

Tool 是 Agent 可以调用的原子能力。

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() ToolSchema      // JSON Schema
    Execute(ctx context.Context, params map[string]any) (ToolResult, error)
}
```

**特性：**
- 统一接口，基于 JSON Schema 描述参数
- 自动注册与发现（文件系统扫描 / 配置声明）
- 支持权限控制（哪些 Agent 可用哪些 Tool）
- 内置 Tool：Shell、HTTP、File
- 可通过插件或配置扩展

---

### 3.8 Skill

Skill 是更高层的能力抽象，组合多个 Tool 完成复杂任务。

```go
type Skill struct {
    Name        string
    Description string
    Tools       []string          // 依赖的 Tool
    Prompt      string            // Skill 专用 Prompt
    Config      map[string]any
}
```

**特性：**
- 以文件形式组织（类似 PicoClaw 的 Skill）
- 包含 SKILL.md 描述文件 + 可选脚本/资源
- Runtime 启动时自动扫描加载
- Skill 可以嵌套调用

---

### 3.9 MCP

Yaa! 原生支持 MCP (Model Context Protocol)。

**双角色：**

| 角色 | 说明 |
|------|------|
| MCP Client | 连接外部 MCP Server，将其 Tool 作为 Yaa! Tool 使用 |
| MCP Server | 将 Yaa! 的能力通过 MCP 协议暴露给其他 MCP Client |

**支持的传输：**
- stdio
- SSE
- WebSocket

---

### 3.10 Provider

Provider 层抽象所有 LLM 访问。

```go
type Provider interface {
    ID() string
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
    Models() []ModelInfo
}
```

**特性：**
- 统一请求/响应类型，屏蔽各厂商差异
- 支持流式输出
- 支持自动重试与故障转移
- 通过配置注册，无需修改代码
- Provider Manager 统一管理，支持按名称路由

---

### 3.11 Remote API

Remote API 是外部客户端与 Runtime 交互的唯一通道。

**默认协议：**

| 协议 | 用途 |
|------|------|
| HTTP REST | 请求-响应模式（创建 Agent、查询状态、管理配置） |
| WebSocket | 双向实时通信（对话、流式输出、事件推送） |
| SSE | 单向流式推送（流式输出、日志、状态变更） |

**核心端点设计：**

```text
# Agent 管理
POST   /api/v1/agents                    # 创建 Agent
GET    /api/v1/agents                    # 列出 Agent
GET    /api/v1/agents/:id                 # 获取 Agent 详情
PUT    /api/v1/agents/:id                 # 更新 Agent 配置
DELETE /api/v1/agents/:id                # 删除 Agent

# Session 管理
POST   /api/v1/agents/:id/sessions       # 创建 Session
GET    /api/v1/agents/:id/sessions       # 列出 Session
GET    /api/v1/sessions/:id               # 获取 Session 详情
DELETE /api/v1/sessions/:id               # 关闭 Session

# 对话
POST   /api/v1/sessions/:id/messages     # 发送消息（非流式）
WS     /api/v1/sessions/:id/stream       # 流式对话（WebSocket）
GET    /api/v1/sessions/:id/events        # 事件流（SSE）

# Tool
GET    /api/v1/tools                     # 列出可用 Tool
POST   /api/v1/tools/:name/execute        # 直接调用 Tool

# Skill
GET    /api/v1/skills                    # 列出可用 Skill

# Provider
GET    /api/v1/providers                  # 列出已注册 Provider
GET    /api/v1/providers/:id/models       # 列出可用模型

# Memory
GET    /api/v1/agents/:id/memory          # 查询记忆
POST   /api/v1/agents/:id/memory          # 写入记忆
DELETE /api/v1/agents/:id/memory/:key     # 删除记忆

# MCP
GET    /api/v1/mcp/servers                # 列出 MCP Server
POST   /api/v1/mcp/servers                # 注册 MCP Server

# 系统
GET    /api/v1/health                     # 健康检查
GET    /api/v1/version                    # 版本信息
GET    /api/v1/config                     # 获取运行时配置
```

**未来扩展协议：**
- gRPC
- Named Pipe (Windows)
- Unix Domain Socket
- QUIC

---

### 3.12 Config

配置系统支持 YAML / TOML / JSON 格式。

```yaml
# yaa.yaml 示例
runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
  api:
    http:
      addr: ":8080"
    ws:
      enabled: true
    sse:
      enabled: true
  auth:
    enabled: true
    tokens:
      - name: "default"
        token: "yaat-xxxxx"

agents:
  - id: "default"
    name: "Default Agent"
    provider: "openai"
    model: "gpt-4o"
    system_prompt: "You are a helpful assistant."
    tools: ["shell", "http", "file"]
    skills: []
    memory:
      enabled: true

providers:
  - id: "openai"
    type: "openai"
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
  
  - id: "ollama"
    type: "ollama"
    base_url: "http://localhost:11434"

mcp:
  servers:
    - name: "filesystem"
      command: "npx"
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
      transport: "stdio"

tools:
  builtin:
    shell:
      enabled: true
      timeout: 30s
    http:
      enabled: true
```

**特性：**
- 配置文件 + 环境变量 + 命令行参数，优先级递增
- 支持配置热更新（文件监听）
- 敏感信息支持环境变量引用 `${VAR_NAME}`
- 配置校验与默认值合并

---

### 3.13 Storage

```go
type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte, ttl ...time.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Keys(prefix string) ([]string, error)
}
```

**默认实现：** SQLite（零依赖，单文件，跨平台）

**可选实现：**
- BoltDB (bbolt)
- 内存存储（测试用）

---

### 3.14 Auth

认证与授权系统保护 Remote API。

```go
type Authenticator interface {
    Authenticate(token string) (*Identity, error)
}

type Authorizer interface {
    Authorize(identity *Identity, action string, resource string) (bool, error)
}
```

**特性：**
- Token 认证（静态 Token / JWT）
- 基于角色的权限控制（RBAC）
- 可配置不需要认证的端点（如 `/health`）

---

## 4. 数据流

### 4.1 对话流程

```text
Client
  │
  │  POST /api/v1/sessions/:id/messages  (或 WS /api/v1/sessions/:id/stream)
  │
  ▼
Remote API Server
  │
  │  Auth → 路由 → Handler
  │
  ▼
Session Manager
  │
  │  加载 Session → 追加用户消息
  │
  ▼
Context Manager
  │
  │  构建 Context：System Prompt + 历史消息 + Memory + Tool 结果
  │  → 截断/压缩至 Token 限制内
  │
  ▼
Agent
  │
  │  选择 Provider → 发送 Chat 请求
  │
  ▼
Provider Layer
  │
  │  转换为厂商格式 → 调用 API → 转换为统一格式
  │  → 流式返回 ChatChunk
  │
  ▼
Agent (Tool Loop)
  │
  │  如果 LLM 返回 Tool Call：
  │  → 执行 Tool → 将结果加入 Context → 再次调用 LLM
  │  循环直到 LLM 返回最终文本
  │
  ▼
Session Manager
  │
  │  追加助手消息 → 持久化 Session
  │
  ▼
Remote API Server
  │
  │  流式返回给 Client (SSE / WS)
  ▼
Client
```

### 4.2 Tool 执行流程

```text
LLM 返回 Tool Call
  │
  ▼
Tool Executor
  │
  │  1. 权限检查（Agent 是否有权调用该 Tool）
  │  2. 参数校验（JSON Schema）
  │  3. 执行 Tool
  │  4. 返回结果
  │
  ▼
Context Manager
  │
  │  将 Tool 结果加入上下文
  │
  ▼
Agent → 再次调用 LLM
```

### 4.3 Skill 调用流程

```text
LLM 决定使用 Skill（通过 Prompt 引导）
  │
  ▼
Skill Manager
  │
  │  1. 加载 Skill 定义
  │  2. 注入 Skill Prompt 到上下文
  │  3. 确保 Skill 依赖的 Tool 可用
  │  4. Agent 在 Skill 指导下调用 Tool 完成任务
  │
  ▼
Agent 继续对话流程
```

---

## 5. 并发模型

- Runtime 单进程，多协程
- 每个 Agent 独立协程管理生命周期
- 每个 Session 的消息处理串行，避免并发消息导致状态混乱
- Tool 执行可并行（同一步骤内无依赖的 Tool 可并发）
- Provider 调用支持流式，不阻塞其他 Session
- WebSocket Hub 管理所有 WS 连接，广播事件

---

## 6. 部署模型

```text
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Standalone  │     │  Embedded   │     │  Container  │
│  yaa.exe     │     │  Go import  │     │  docker     │
│  单一可执行文件 │     │  作为库嵌入   │     │  容器化部署   │
└─────────────┘     └─────────────┘     └─────────────┘
      │                    │                    │
      └────────────┬───────┘────────────────────┘
                   │
                   ▼
         同一套 Runtime
         同一套 Remote API
         同一套配置
```

---

## 7. 技术选型

| 领域 | 选型 | 理由 |
|------|------|------|
| 语言 | Go | 单一可执行文件、跨平台、并发模型、零运行时依赖 |
| HTTP 框架 | 标准库 net/http 或轻量框架 | 最小依赖，Windows 兼容 |
| WebSocket | gorilla/websocket 或 nhooyr/websocket | 成熟稳定 |
| 存储 | SQLite (modernc.org/sqlite) | 纯 Go 实现，零 CGO，Windows 友好 |
| 配置 | YAML (gopkg.in/yaml.v3) | 人类友好，生态成熟 |
| 日志 | slog（标准库） | Go 1.21+ 内置，零依赖 |
| CLI | 标准库 flag 或 cobra | Runtime 不需要复杂 CLI |
| 构建 | Makefile + Go cross-compile | 简单直接 |

**关键约束：**
- 零 CGO 依赖（保证 Windows 7 兼容与交叉编译）
- 优先使用 Go 标准库
- 外部依赖需要审慎评估

---

## 8. 版本与兼容性

### API 版本

- Remote API 通过 URL 路径版本化：`/api/v1/...`
- 新版本不删除旧版本端点，直到明确废弃
- 重大变更需要升版本号 `/api/v2/...`

### 配置兼容

- 配置文件新增字段使用默认值，不破坏旧配置
- 废弃字段保留但标记 `deprecated`
- 配置迁移工具在启动时自动处理

### 插件接口

- Tool / Provider / Plugin 接口变更需遵循 Go interface 兼容规则
- 优先新增方法而非修改现有方法
- 提供适配层处理不兼容变更

---

## 9. 开发路线图

### Phase 0：架构设计 ✅ 当前阶段

- [x] 项目 README
- [x] 目录结构设计
- [x] 整体架构设计文档
- [ ] Remote API 设计文档
- [ ] Provider 层设计文档
- [ ] Tool 系统设计文档
- [ ] Skill 系统设计文档
- [ ] MCP 设计文档
- [ ] Memory 设计文档
- [ ] Session / Context 设计文档
- [ ] 配置系统设计文档
- [ ] 开发路线图

### Phase 1：核心骨架

- [ ] 项目初始化（go.mod、Makefile）
- [ ] Runtime 生命周期管理
- [ ] Config 加载与校验
- [ ] Storage 层（SQLite）
- [ ] 基础 HTTP API Server
- [ ] 健康检查端点

### Phase 2：Agent 核心

- [ ] Provider 层 + OpenAI 实现
- [ ] Session 管理
- [ ] Context 窗口管理
- [ ] Agent 生命周期
- [ ] 基本对话流程（非流式）
- [ ] 流式对话（SSE / WS）

### Phase 3：能力扩展

- [ ] Tool 系统 + 内置 Tool
- [ ] Skill 系统
- [ ] Memory 系统
- [ ] Planner
- [ ] Auth 认证

### Phase 4：生态

- [ ] MCP Client / Server
- [ ] 更多 Provider 实现
- [ ] Plugin 系统
- [ ] pkg/remoteapi 客户端 SDK
- [ ] Docker 部署

### Phase 5：生产化

- [ ] 配置热更新
- [ ] 优雅关闭
- [ ] 监控与指标
- [ ] 性能优化
- [ ] Windows 7 兼容测试
- [ ] 文档完善

---

## 10. 设计决策记录

### ADR-001: 选择 Go 而非 Rust

- **决策**：使用 Go
- **理由**：单一可执行文件、交叉编译简单、GC 降低开发复杂度、Windows 兼容性好、生态丰富

### ADR-002: 选择 SQLite 而非 PostgreSQL

- **决策**：默认使用 SQLite
- **理由**：零依赖、单文件、跨平台、适合单机 Runtime；未来可扩展为外部数据库

### ADR-003: 选择纯 Go SQLite (modernc.org/sqlite) 而非 CGO 版本

- **决策**：使用 modernc.org/sqlite
- **理由**：零 CGO 依赖，保证 Windows 7 兼容与交叉编译能力

### ADR-004: Remote API 使用 HTTP+WS+SSE 而非 gRPC

- **决策**：默认使用 HTTP REST + WebSocket + SSE
- **理由**：最大兼容性，任何客户端都能轻松对接；gRPC 作为未来扩展

### ADR-005: 配置使用 YAML 而非 TOML

- **决策**：主推 YAML
- **理由**：生态成熟、嵌套结构表达力强、用户熟悉度高；同时兼容 TOML/JSON

---

*最后更新: 2025-07-15*
