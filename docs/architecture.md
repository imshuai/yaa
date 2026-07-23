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
│  │  单 turn 计划生成 / DAG 校验 / 步骤执行                     │  │
│  └────────────────────────┬───────────────────────────────┘  │
│                           │                                  │
│  ┌──────────┬──────────┬───┴────┬──────────┬───────────────┐  │
│  │  Tool    │  Skill   │ Memory │   MCP    │   Plugin     │  │
│  │ Manager  │ Manager  │ System │  Client  │   Manager    │  │
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
│  │  (YAML/TOML)  │  │(SQLite/Memory)│  │  (Token/Policy) │   │
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
    Config    *config.ReloadManager
    Agents    *agent.Manager
    Sessions  *session.Manager
    Context   *ctxwindow.Manager
    Tools     *tool.Manager
    Skills    *skill.Manager
    Providers *provider.Manager
    Memory    *memory.Manager
    AuthN     auth.Authenticator
    AuthZ     auth.Authorizer
    MCP       *mcp.Manager
    Plugins   *plugin.Manager
    Storage   storage.Storage
    API       *api.Server
}
```

**初始化顺序：**

```text
Config.Load（一次）→ ReloadManager bootstrap → Storage → Provider → Memory →
Tool builtins → Plugin → MCP.Prepare（上游 Proxy + 本地 listener，不 Serve）→ Skill →
Config.Activate(binding) → MCP.Activate（本地 Serve）→ Session Restore →
Context → Agent（含每 Agent Planner）→ Auth → API →
Config Watcher → Runtime Ready
```

bootstrap 到 `Config.Activate` 始终使用同一个不可变 snapshot；不得再次读取配置文件。Activate 前不启动 MCP Serve、watcher、Config Tool、Remote listener 或 Agent turn。任一步失败，Runtime 立即标记 Not Ready，并按已成功启动组件的逆序执行 rollback；所有进程 Kill 后必须 Wait，所有 goroutine 必须有 cancel + WaitGroup，关闭错误用 `errors.Join` 聚合后返回最早的启动错误。Runtime readiness 是关键组件状态的动态 AND，并在 health/request gate 每次读取，其中必须包含 `MCP.Ready()`；本地 MCP Serve 运行期异常因此立即进入 Not Ready，无需另建一套缓存状态。

正常关闭顺序固定为：先原子标记 Not Ready，停止 watcher 和 API 接入，再调用 `Agent.Quiesce()` 只拒绝新 turn；`Session.Shutdown(ctx)` 在统一 deadline 内 drain，超时才取消运行 turn并等待 runner 退出，随后 `Agent.Shutdown(ctx)` 收拢异常残留。之后按依赖逆序关闭 MCP、Plugin、Tool、Memory、Provider 和根 Storage。`MCP.Stop(ctx)` 或 `Plugin.StopAll(ctx)` 即使因 caller deadline 先返回，Runtime 仍必须等待对应 `Done()`；MCP 再用 `Stop(context.Background())`、Plugin 用 `WaitStopped()` 取得最终 teardown 结果后，才能关闭 Tool Manager。后台清理不得越过 owner 顺序。无生命周期资源的 Auth、Context、Skill 和 Planner snapshot 只解除引用。每个 owner 只关闭自己的资源一次。入口使用 `signal.NotifyContext(..., os.Interrupt)`；Windows 7 不依赖 `SIGTERM`。

---

### 3.2 Agent

Agent 是系统核心抽象和唯一 turn 编排 owner。权威类型、Manager API、Provider/Tool loop、流式 accumulator、Planner 绑定和取消契约见 [Agent 执行契约](agent.md)。Agent 由 `agents[]` 配置创建，`AgentConfig.id` 是唯一 ID；v1 不通过 Remote API新增、修改或删除配置。Manager 只维护运行态：

```text
stopped ── start ──> running ── pause ──> paused
   ^                    |                    |
   └────── stop ────────┴────── stop ───────┘
```

一个 Runtime 可以管理多个 Agent 实例，每个 Agent：
- 绑定不同的 Provider
- 拥有不同的 Tool / Skill 集合
- 以 AgentID 隔离自己的 Session 和 Memory scope；Manager/基础设施由 Runtime 共享
- 拥有不同的 System Prompt、权限和可选 Planner/Executor
- 只通过 `session.Manager.RunTurn` 执行完整 turn；Remote 和其他模块不能复制执行循环

---

### 3.3 Session

Session 管理一次完整的交互上下文；权威字段、状态机、snapshot 和错误见 [Session 文档](session/README.md)。

```go
type Session struct {
    ID             string
    AgentID        string
    State          session.State
    CreatedAt      time.Time
    UpdatedAt      time.Time
    LastActivityAt time.Time
    Messages       []session.SessionMessage
    Metadata       map[string]any
    Policy         config.SessionPolicy
    SchemaVersion  int
}
```

**特性：**
- 一个 Agent 可以有多个并发 Session
- Session 之间状态隔离
- `persist=true` 时通过根 `storage.Storage` 同步保存 `session:<id>` snapshot；恢复前 Runtime 不 Ready
- 同一 Session 的完整 turn 进入 FIFO gate；不同 Session 并行
- Close 保留历史，DELETE 才是物理删除；Remote API 语义见 [Session API](remote-api/session.md)

---

### 3.4 Context

Context 负责管理传给 LLM 的上下文窗口。

**核心职责：**
- 接收 Agent 已组装的完整 `provider.ChatRequest`
- 根据目标 Model 的硬窗口和 Effective Context Config 估算输入
- 按 `hybrid`、`truncate` 或 `reject` 处理旧完整 unit

Runtime 与 Agent 直接持有 `*ctxwindow.Manager`。v1 只有一个实现，不额外声明单实现接口；`BuildInput`、`BuildOutput` 和错误契约见 [Context Manager](context/manager.md)。

---

### 3.5 Planner

Planner 是可选的单 turn 前置步骤：把复杂输入生成临时 Plan，由 Executor 在当前 Session FIFO turn 内执行。Plan 不写入 Session snapshot、不跨重启恢复，也没有独立 Remote API。权威类型、DAG 校验和失败语义见 [Planner 文档](planner/README.md)。

---

### 3.6 Memory

Memory 是 Runtime 共享 Manager 上按 AgentID 隔离的 long-term ContentStore；权威模型、`PutResult`、检索排序、TTL 和 degraded 语义见 [Memory 文档](memory/README.md)。Session 自己拥有完整消息历史，Session Close/Delete 不触发 Memory 写入或删除。Agent 在 Context Build 前先检索并格式化 Memory；只有显式 Put/Promote 会提交内容。

---

### 3.7 Tool

Tool 是 Agent 可以调用的原子能力。

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage // JSON Schema object
    Execute(ctx context.Context, scope ExecutionScope, params map[string]any) (ToolResult, error)
}
```

**特性：**
- 统一接口，基于 JSON Schema 描述参数
- builtin、Plugin Proxy 和 MCP Proxy 按固定启动顺序注册
- 支持权限控制（哪些 Agent 可用哪些 Tool）
- 内置 Tool：Shell、HTTP、File
- 可通过 Plugin RPC 或 MCP 扩展

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
- Skill 依赖只在启动时展开为确定顺序的 Prompt，不产生运行时嵌套调用

---

### 3.9 MCP

Yaa! 原生支持 MCP (Model Context Protocol)。

**双角色：**

| 角色 | 说明 |
|------|------|
| MCP Client | 连接外部 MCP Server，将其 Tool 作为 Yaa! Tool 使用 |
| MCP Server | 将 Yaa! 的能力通过 MCP 协议暴露给其他 MCP Client |

**支持的传输：**
- `stdio`
- `streamable_http`（MCP 2025-03-26）
- `sse`（仅兼容 MCP 2024-11-05 legacy Server）

WebSocket 只属于 Yaa! Remote API，不是 MCP transport。

---

### 3.10 Provider

Provider 层抽象所有 LLM 访问。

```go
type Provider interface {
    ID() string
    Type() string
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error)
    EstimateInputTokens(ctx context.Context, req *ChatRequest) (int, error)
    Models() []ModelInfo
    Close() error
}
```

**特性：**
- 统一请求/响应类型，屏蔽各厂商差异
- 支持流式输出
- 在首次响应前对明确可重试错误执行有限重试；v1 不做多区域或多 Provider failover
- 通过配置注册，无需修改代码
- Provider Manager 统一管理，支持按名称路由

---

### 3.11 Remote API

Remote API 是外部进程与 Runtime 交互的统一通道；嵌入式 Go 调用可以直接使用 Runtime/Manager API。

**默认协议：**

| 协议 | 用途 |
|------|------|
| HTTP REST | 请求-响应模式（查询配置、管理 Session/Memory 和运行态） |
| WebSocket | 双向实时通信（对话、流式输出、事件推送） |
| SSE | 单向流式推送（流式输出、日志、状态变更） |

`remote-api/INDEX.md` 是路由、权限和 envelope 的唯一清单。v1 的 Agent、Provider、Tool、Skill 与 MCP 配置均来自 Config；Remote API 对它们只提供读取和 Agent 运行态操作，不提供第二套动态配置存储。

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
      addr: "127.0.0.1:8080"
    ws:
      enabled: true
    sse:
      enabled: true
  auth:
    enabled: true
    tokens:
      - name: "default"
        token: "${YAA_AUTH_TOKEN}"
        roles: ["admin"]

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
  server:
    enabled: false
    agent_id: "default"

tools:
  builtin:
    shell:
      enabled: true
      timeout: 30s
      options:
        allowed_commands: []
    http:
      enabled: true
      options:
        max_response_bytes: 1048576
```

**特性：**
- 配置文件 + 环境变量 + 命令行参数，优先级递增
- 支持配置热更新（文件监听）
- 敏感信息支持环境变量引用 `${VAR_NAME}`
- 配置校验与默认值注入

---

### 3.13 Storage

```go
type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte, ttl ...time.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Keys(prefix string) ([]string, error)
    Close() error
}
```

**默认实现：** SQLite（零依赖，单文件，跨平台）

**可选实现：**
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
- 可配置不需要认证的端点（如 `/api/v1/health`）

---

### 3.15 Plugin

Plugin 是进程外 Tool 扩展机制。Runtime 在启动阶段读取 Manifest、启动独立插件进程，并通过版本化 RPC 协议注册稳定 Tool Proxy。Skill 仍由 SKILL.md 目录加载，可依赖 Plugin 提供的 Tool；Hook、Provider 和 Memory 不通过 v1 Plugin RPC 扩展。唯一 concrete API 分别见 [Plugin Loader](plugin/loader.md#2-路径与发现) 与 [Plugin Manager](plugin/manager.md#1-职责与状态)，本层不再声明一份会漂移的伪接口。

**约束：**
- 不使用 Go `plugin` 包，不把第三方代码加载进 Runtime 进程
- 本版本只在 Runtime 启动阶段加载插件，不支持运行时热插拔
- Unix 使用 Unix Socket；Windows 7 使用回环 TCP + 启动 nonce；v1 不支持远程 Plugin endpoint
- Plugin RPC 与面向客户端的 Remote API 相互独立

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
  │  Auth → 路由 → Handler → Agent.Manager.HandleTurn
  │
  ▼
Agent
  │
  │  校验 Agent/Session 归属 → Session.Manager.RunTurn
  │
  ▼
Session RunTurn FIFO callback
  │
  │  turn.AppendUser（同步 snapshot）→ 选择 Provider/Model
  │  → 检索 Memory → 冻结 canonical Tool snapshot 与 turn-local alias 投影
  │  → 深拷贝并投影 Provider wire ChatRequest → 解析 Effective Context Config
  │
  ▼
Context Manager
  │
  │  估算完整请求 → hybrid/truncate/reject
  │  → 确认 input tokens 不超过输入预算
  │
  ▼
Provider Layer
  │
  │  转换为厂商格式 → 调用 API → 转换为统一格式
  │  → 原样返回含 Provider alias 的 ChatResponse / ChatChunk
  │
  ▼
Agent (Tool Loop，仍在同一 RunTurn callback)
  │
  │  如果 LLM 返回 Tool Call：
  │  → 完整聚合并按冻结映射一次性反查为 canonical name
  │  → 全批校验成功后并行执行 Tool → turn.Append 原子提交完整 Tool unit
  │  → 重新 Build → 再次调用 LLM
  │  循环直到 LLM 返回最终文本
  │
  ▼
session.Turn
  │
  │  turn.Append final assistant → 持久化 Session
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
LLM 返回完整 Provider Tool Call
  │
  ▼
Agent 按 turn-local 映射全批反查 canonical name
  │
  ▼
Tool Executor
  │
  │  1. 权限检查（Agent 是否有权调用该 Tool）
  │  2. 参数校验（JSON Schema）
  │  3. 执行 Tool
  │  4. 按 tools.max_result_tokens 限制结果
  │
  ▼
Agent 持有的 session.Turn
  │
  │  原子追加完整 assistant(tool_calls) + tool results
  │
  ▼
Agent → turn.Snapshot → Context Manager.Build → 再次调用 LLM
```

### 4.3 Skill 调用流程

```text
Runtime 启动绑定
  │
  ▼
Skill Manager
  │
  │  加载、校验依赖并冻结每个 Agent 的 ResolvedSkill
  │
  ▼
每个 turn：Agent 投影全部已绑定 Skill Prompt
  │
  ▼
Context Manager.Build → 正常 Provider/Tool loop
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
| HTTP Server / Router | 标准库 `net/http` + `github.com/gorilla/mux` | Go 1.20 可用，显式 method/path route metadata |
| WebSocket | `github.com/gorilla/websocket` | 与 `net/http` 集成成熟，支持 Go 1.20 |
| 存储 | SQLite (modernc.org/sqlite) | 纯 Go 实现，零 CGO，Windows 友好 |
| 配置 | YAML (gopkg.in/yaml.v3) | 人类友好，生态成熟 |
| 配置解码 | github.com/mitchellh/mapstructure | presence-aware 覆盖、未知字段与 duration hook |
| JWT | github.com/golang-jwt/jwt/v5 | 固定算法并完整校验标准 Claims，避免手写验签 |
| 日志 | `slog` 兼容 API | Go 1.20 构建使用固定版本的 `golang.org/x/exp/slog`；未来升级到标准库 `log/slog` |
| CLI | 标准库 `flag` | Runtime 不需要子命令框架 |
| 构建 | Makefile + Go cross-compile | 简单直接 |

**关键约束：**
- Go 工具链固定为 1.20.x；这是支持 Windows 7 的最后一个 Go 主版本
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

### 接口兼容

- 向已有 Go interface 新增方法同样是破坏性变更；可选能力使用独立小接口，既有接口变更需升模块 major 或提供适配层
- Plugin wire contract 只按 Protobuf 字段兼容规则演进；删除/复用字段或改变语义必须提升 RPC major

---

## 9. 开发路线图

### Phase 0：架构设计 ✅ 已完成

- [x] 项目 README
- [x] 目录结构设计
- [x] 整体架构设计文档
- [x] Remote API 设计文档（11 个文件）
- [x] Provider 层设计文档
- [x] Tool 系统设计文档（13 个文件）
- [x] Skill 系统设计文档（9 个文件）
- [x] MCP 设计文档（10 个文件）
- [x] Memory 设计文档（10 个文件）
- [x] Session 设计文档（11 个文件）
- [x] Context 设计文档（7 个文件）
- [x] Planner 设计文档（10 个文件）
- [x] 配置系统设计文档（11 个文件）
- [x] Storage 设计文档（7 个文件）
- [x] Auth 设计文档（7 个文件）
- [x] Plugin 设计文档（10 个文件）
- [x] 开发路线图

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

### ADR-006: Plugin 统一采用进程外 RPC

- **决策**：Plugin 以独立进程运行，通过版本化 gRPC 协议与 Runtime 通信
- **理由**：隔离崩溃、避免 Go ABI 耦合，并覆盖 Windows 7；不支持运行时热插拔以降低状态复杂度

---

*最后更新: 2025-07-17*
