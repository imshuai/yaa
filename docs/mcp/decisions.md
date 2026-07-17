# MCP 设计决策

> 文档路径: `docs/mcp/decisions.md`
> 上级: `docs/mcp/README.md`

---

## 设计决策

### MC-001: MCP 作为双角色组件——Server 与 Client 并存

**决策：** Yaa! 同时实现 MCP Server 和 MCP Client 两种角色，统一由 MCP Manager 管理。

**理由：**
- 作为 **Client**，Yaa! 可连接外部 MCP Server（如 GitHub、文件系统、数据库），将其 Tool 纳入 Agent 可用工具池
- 作为 **Server**，Yaa! 可将自身 Tool / Skill / Agent 能力通过 MCP 协议暴露给外部 Client（如 Claude Desktop、其他 Agent Runtime）
- 双角色设计使 Yaa! 既能消费生态、又能贡献生态，避免成为信息孤岛
- 两种角色共享 MCP 协议层代码，减少重复实现

**影响：** MCP Manager 需要管理两类连接的生命周期，配置结构需区分 `clients` 和 `server` 两部分。

---

### MC-002: 传输层默认 stdio，可选 SSE/HTTP

**决策：** MCP Client 连接外部 Server 时默认使用 stdio 传输，同时支持 SSE（Server-Sent Events）和 Streamable HTTP 传输。MCP Server 对外暴露时默认使用 stdio，可选 SSE/HTTP。

**理由：**
- stdio 是 MCP 协议的基准传输，所有 Server 都应支持
- stdio 无需网络配置，本地启动子进程即可通信，安全性最高
- SSE/HTTP 适用于远程 Server 或需要多 Client 共享的场景
- Streamable HTTP 是 MCP 2025-03-26 规范新增传输，支持更好的连接管理

**影响：** 传输层需要抽象为接口，支持多种传输实现的动态切换。

---

### MC-003: 外部 MCP Tool 统一映射为 Yaa! Tool

**决策：** 外部 MCP Server 暴露的 Tool，经 MCP Client 发现后，统一映射为 Yaa! 内部 Tool 格式，注册到 Tool Manager。

**理由：**
- Agent 和 LLM 不需要感知 Tool 来自 MCP 还是内置，统一接口降低认知负担
- Tool Manager 的权限控制、调用日志、错误处理逻辑可复用
- 映射层负责协议转换（MCP ↔ Yaa! Tool Schema），隔离细节

**影响：** 需要 Tool Schema 转换逻辑（MCP JSON Schema → Yaa! Tool Definition），字段映射需处理命名差异。

---

### MC-004: Tool 名称使用命名空间前缀防冲突

**决策：** 外部 MCP Tool 映射到 Yaa! 时，使用 `{server_name}.{tool_name}` 格式的命名空间前缀，防止不同 Server 之间的 Tool 名称冲突。

**理由：**
- 不同 MCP Server 可能暴露同名 Tool（如多个 Server 都有 `search` Tool）
- 命名空间前缀是简单有效的去冲方案
- 前缀来源于 Server 配置名，用户可自定义

**影响：** Agent 调用外部 Tool 时需使用带前缀的全名；Tool Manager 查找逻辑需支持命名空间解析。

---

### MC-005: MCP Server 连接失败采用降级策略，不阻断启动

**决策：** MCP Client 连接外部 Server 失败时，记录错误并跳过该 Server，不阻断 Runtime 启动和其他 Server 的连接。

**理由：**
- 外部 Server 可用性不受 Yaa! 控制，不应因外部故障导致整体不可用
- Agent 可在运行中重试连接（指数退避），实现自愈
- 降级策略保证核心功能始终可用

**影响：** Agent 需要处理 Tool 不可用的情况（Tool 存在于定义但调用时连接已断开），返回友好错误信息。

---

### MC-006: MCP Server 对外暴露的 Tool 可选择性地声明

**决策：** Yaa! 作为 MCP Server 时，不强制暴露全部 Tool，用户可在配置中选择性地声明对外暴露的 Tool 列表。

**理由：**
- 安全性：内部 Tool（如文件系统操作）不应无条件暴露给外部
- 最小权限原则：Client 只应获得所需 Tool
- 灵活性：不同 Client 可通过不同 Server 实例获得不同 Tool 集

**影响：** MCP Server 配置需要 `exposed_tools` 字段，运行时根据配置过滤 Tool 列表。

---

### MC-007: MCP Resource 和 Prompt 暂不映射，优先支持 Tool

**决策：** MVP 阶段仅实现 MCP Tool 的双向映射，MCP Resource（资源）和 MCP Prompt（提示模板）暂不支持。

**理由：**
- Tool 是 MCP 三大能力中与 Agent Loop 集成最紧密的
- Resource 和 Prompt 的映射需要额外的 Context 管理和 UI 交互设计
- 优先交付核心价值，后续迭代再补充

**影响：** MCP Client 发现 Server 的 Resource 和 Prompt 时忽略不报错；MCP Server 不响应 Resource/Prompt 相关请求。

---

### MC-008: MCP Client 连接管理使用连接池 + 心跳检测

**决策：** MCP Client 为每个外部 Server 维护一个持久连接，通过定期心跳（ping）检测连接健康状态，断线自动重连。

**理由：**
- 持久连接避免每次调用 Tool 时重新握手，降低延迟
- 心跳检测及时发现断线，避免调用时才发现不可用
- 自动重连提升鲁棒性，尤其适用于 SSE/HTTP 传输

**影响：** MCP Manager 需要实现连接状态机和重连策略（指数退避 + 最大重试次数）。

---

## 模块关系

```text
┌──────────────────────────────────────────────────────────────────┐
│                            Yaa! Runtime                            │
│                                                                    │
│  ┌──────────────────────────────────────────────────────┐        │
│  │                     MCP Manager                        │        │
│  │                                                        │        │
│  │  ┌─────────────────┐         ┌─────────────────┐      │        │
│  │  │  MCP Client Pool │         │  MCP Server Hub  │      │        │
│  │  │                  │         │                  │      │        │
│  │  │ ┌─────────────┐ │         │ ┌─────────────┐ │      │        │
│  │  │ │ Client #1   │ │         │ │ Server #1   │ │      │        │
│  │  │ │ (stdio→Git) │ │         │ │ (stdio)     │ │      │        │
│  │  │ ├─────────────┤ │         │ ├─────────────┤ │      │        │
│  │  │ │ Client #2   │ │         │ │ Server #2   │ │      │        │
│  │  │ │ (SSE→DB)    │ │         │ │ (SSE)       │ │      │        │
│  │  │ ├─────────────┤ │         │ └─────────────┘ │      │        │
│  │  │ │ Client #N   │ │         │                  │      │        │
│  │  │ │ (HTTP→...)  │ │         │  Exposed Tools   │      │        │
│  │  │ └─────────────┘ │         │  (选择性声明)    │      │        │
│  │  │                  │         │                  │      │        │
│  │  │ 心跳检测/自动重连 │         │ 多 Client 接入   │      │        │
│  │  └────────┬─────────┘         └────────┬─────────┘      │        │
│  └───────────┼───────────────────────────┼────────────────┘        │
│              │                           │                          │
│              │ Tool 映射                  │ Tool 暴露               │
│              ▼                           ▼                          │
│  ┌──────────────────┐         ┌──────────────────┐                │
│  │   Tool Manager     │         │   Tool Manager    │                │
│  │                    │         │                    │                │
│  │ ┌────────────────┐ │         │ ┌────────────────┐│                │
│  │ │ 内置 Tool       │ │         │ │ 内置 Tool       ││                │
│  │ │ MCP 映射 Tool   │◄┘         │ │ Skill 专属 Tool ││                │
│  │ │ Skill 专属 Tool │           │ │ (选择性暴露)    ││                │
│  │ └────────────────┘ │         │ └────────────────┘│                │
│  └────────────────────┘         └──────────────────┘                │
│              │                           │                          │
│              │ 统一 Tool 接口             │ 统一 Tool 接口           │
│              ▼                           ▼                          │
│  ┌──────────────────────────────────────────────────────┐          │
│  │                     Agent Loop                         │          │
│  │    Provider (LLM) ←→ Tool 调用 ←→ Context Manager     │          │
│  └──────────────────────────────────────────────────────┘          │
│                                                                    │
└──────────────────────────────────────────────────────────────────────┘

外部生态:
  ┌─────────────┐        ┌─────────────┐        ┌─────────────┐
  │ 外部 MCP     │        │ 外部 MCP     │        │ 外部 Client  │
  │ Server       │        │ Server       │        │ (Claude/    │
  │ (GitHub)     │        │ (Filesystem) │        │  其他 Agent) │
  │ stdio/SSE    │        │ stdio        │        │ SSE/HTTP    │
  └─────────────┘        └─────────────┘        └─────────────┘
       ▲                      ▲                      │
       │ stdio/SSE/HTTP       │ stdio                 │ SSE/HTTP
       └──────────────────────┴──────────────────────┘

依赖方向:
  MCP Manager → Tool Manager (Tool 映射注册 / Tool 选择性暴露)
  MCP Manager → Config (读取 clients/server 配置)
  MCP Client Pool → 外部 MCP Server (stdio/SSE/HTTP 连接)
  MCP Server Hub → 外部 MCP Client (接受连接，响应请求)
  Agent Loop → Tool Manager (统一 Tool 调用，不感知来源)
  Tool Manager → MCP Manager (MCP Tool 调用时委托给对应 Client)
```

**依赖关系：**
- MCP Manager 依赖 Tool Manager（注册映射 Tool、查询 Tool 元信息）
- MCP Manager 依赖 Config（连接配置、暴露配置）
- MCP Client Pool 依赖外部 MCP Server（通过 stdio 子进程或网络连接）
- MCP Server Hub 被外部 MCP Client 依赖（Yaa! 作为 Server 提供服务）
- Agent Loop 不直接依赖 MCP Manager，通过 Tool Manager 间接使用 MCP Tool
- MCP Manager 不依赖 Provider，协议转换在 Tool Manager 层完成
