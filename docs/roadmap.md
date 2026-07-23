# Yaa! 开发路线图

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/roadmap.md`

---

## 1. 概述

Yaa! 采用分阶段迭代开发策略，从架构设计到生产化共 6 个阶段。

```text
Phase 0          Phase 1          Phase 2          Phase 3          Phase 4          Phase 5
架构设计  ──►   核心骨架  ──►   Agent 核心  ──►  能力扩展  ──►   生态建设  ──►   生产化
 (文档)         (可运行)        (可对话)        (有能力)        (可扩展)        (可生产)
```

---

## 2. 各阶段详细规划

### Phase 0：架构设计 ✅ 已完成

**目标：** 完成所有模块的设计文档，确保架构合理、接口清晰。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| 项目 README | 项目定位、设计理念、当前状态 | ✅ 已完成 |
| 目录结构设计 | directory.md，完整目录树 | ✅ 已完成 |
| 整体架构设计 | architecture.md，15 个核心模块 | ✅ 已完成 |
| Remote API 设计 | 11 个文件（含 INDEX.md） | ✅ 已完成 |
| Provider 层设计 | provider.md | ✅ 已完成 |
| Tool 系统设计 | 13 个文件 | ✅ 已完成 |
| Skill 系统设计 | 9 个文件 | ✅ 已完成 |
| Memory 系统设计 | 10 个文件 | ✅ 已完成 |
| Session 系统设计 | 11 个文件 | ✅ 已完成 |
| Context 系统设计 | 7 个文件 | ✅ 已完成 |
| Planner 系统设计 | 10 个文件 | ✅ 已完成 |
| Config 系统设计 | 11 个文件 | ✅ 已完成 |
| Storage 系统设计 | 7 个文件 | ✅ 已完成 |
| Auth 系统设计 | 7 个文件 | ✅ 已完成 |
| MCP 系统设计 | 10 个文件 | ✅ 已完成 |
| Plugin 系统设计 | 10 个文件 | ✅ 已完成 |
| 开发路线图 | roadmap.md（本文件） | ✅ 已完成 |

文档数量以仓库实时统计为准，不作为架构验收条件；Phase 0 的验收依据是本表所列契约和全量文档门禁。

---

### Phase 1：核心骨架

**目标：** 项目可编译、可启动，基础服务可用。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| 项目初始化 | go.mod、Makefile、.gitignore、CI | 1 天 |
| Runtime 生命周期 | start/stop/graceful shutdown | 2 天 |
| Config 加载与校验 | YAML/JSON/TOML 多格式、环境变量、校验 | 3 天 |
| Storage 层 | SQLite 实现、KV 接口 | 2 天 |
| 基础 HTTP Server | 路由、中间件、JSON 响应 | 2 天 |
| 健康检查端点 | `/api/v1/health`（包含 `ready` 状态，未就绪返回 503） | 0.5 天 |
| 日志系统 | 结构化日志、级别控制 | 1 天 |

**里程碑：** `yaa` 可启动，`/api/v1/health` 返回统一 envelope。

---

### Phase 2：Agent 核心

**目标：** 可以与 LLM 进行对话（流式 + 非流式）。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| Provider 层 + OpenAI | Chat/StreamChat/Models 接口 | 3 天 |
| Session 管理 | 创建/关闭/持久化/加载 | 3 天 |
| Context 窗口管理 | 构建/截断/压缩 | 3 天 |
| Agent 生命周期 | 创建/启动/停止 | 2 天 |
| 基本对话流程 | 非流式 POST /messages | 2 天 |
| 流式对话 | SSE + WebSocket | 3 天 |
| 更多 Provider | Claude、Gemini、Ollama | 3 天 |

**里程碑：** 通过 API 与 LLM 完成一轮完整对话。

---

### Phase 3：能力扩展

**目标：** Agent 具备工具调用、技能、记忆、规划能力。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| Tool 系统 + 内置 Tool | 注册/执行/Tool Loop、shell/http/file | 5 天 |
| Skill 系统 | 启动加载、依赖绑定、Prompt 注入 | 4 天 |
| Memory 系统 | Agent long-term ContentStore、TTL、exact cosine（可选） | 5 天 |
| Planner | Plan/Step/Executor、LLM 驱动规划 | 4 天 |
| Auth 认证 | Token/JWT、RBAC | 3 天 |

**里程碑：** Agent 可调用 Tool、使用 Skill、记忆跨 Session。

---

### Phase 4：生态建设

**目标：** 支持 MCP、Plugin，可扩展生态。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| MCP Client | stdio/Streamable HTTP/legacy SSE、Tool 映射 | 5 天 |
| MCP Server | 暴露 Yaa! Tool 为 MCP Tool | 3 天 |
| 更多 Provider | DeepSeek、Qwen、Azure、OpenRouter | 3 天 |
| Plugin 系统 | Protobuf IDL/SDK、进程外 RPC、握手与生命周期 | 5 天 |
| 客户端 SDK | pkg/remoteapi Go SDK | 3 天 |
| Docker 部署 | Dockerfile + docker-compose | 2 天 |

**里程碑：** 可连接外部 MCP Server，可加载第三方插件。

Phase 编号表示交付顺序，不是 Runtime 初始化顺序。Phase 4 接入完成后，Plugin/MCP Tool Proxy 必须先注册，Skill 才执行最终依赖 binding；完整顺序以 [architecture.md](architecture.md#31-runtime) 为准。

---

### Phase 5：生产化

**目标：** 生产环境可用，稳定可靠。

| 子任务 | 验收标准 | 预估周期 |
|--------|----------|----------|
| 配置热更新 | 文件监听、可热更新字段原子重载；Plugin 等启动期字段提示重启 | 3 天 |
| 优雅关闭 | 信号处理、连接清理、超时 | 2 天 |
| 监控与指标 | Prometheus 指标、健康面板 | 3 天 |
| 性能优化 | 连接池、缓存、并发调优 | 5 天 |
| Windows 7 兼容 | 编译测试、API 兼容、降级 | 3 天 |
| 文档完善 | API 文档、用户指南、示例 | 3 天 |

**里程碑：** 生产环境稳定运行 7 天无重启。

---

## 3. 里程碑总览

| 里程碑 | 阶段 | 交付物 | 预估时间 |
|--------|------|--------|----------|
| M0: 文档完成 | Phase 0 | 全部设计文档 | ✅ |
| M1: 可启动 | Phase 1 | yaa 二进制 + `/api/v1/health` | +2 周 |
| M2: 可对话 | Phase 2 | 流式对话 API | +3 周 |
| M3: 有能力 | Phase 3 | Tool/Skill/Memory/Planner | +4 周 |
| M4: 可扩展 | Phase 4 | MCP/Plugin/SDK | +4 周 |
| M5: 可生产 | Phase 5 | 监控/稳定/Windows | +3 周 |

---

## 4. 优先级矩阵

```text
              高紧急                    低紧急
         ┌─────────────────┬─────────────────┐
  高重要 │  Phase 1 核心   │  Phase 4 生态   │
         │  Phase 2 对话   │                 │
         ├─────────────────┼─────────────────┤
  低重要 │  Phase 3 扩展   │  Phase 5 优化   │
         │  Phase 3 Auth   │  Windows 7 兼容 │
         └─────────────────┴─────────────────┘
```

---

## 5. 技术债务清单

| 编号 | 技术债 | 来源阶段 | 处理阶段 | 优先级 |
|------|--------|----------|----------|--------|
| DEBT-001 | Token 计算可能不精确 | Phase 2 | Phase 5 | 中 |
| DEBT-002 | exact cosine 的大规模性能需要实测 | Phase 3 | Phase 5 | 中 |
| DEBT-003 | Plugin RPC 协议与多平台 IPC 验证 | Phase 4 | Phase 5 | 高 |
| DEBT-004 | SSE 断线重连机制需完善 | Phase 2 | Phase 5 | 低 |
| DEBT-005 | Config 热更新与运行状态一致性 | Phase 1 | Phase 5 | 中 |

---

## 6. Windows 7 兼容性计划

Yaa! 的设计原则之一是 **Windows First**，需确保在 Windows 7 上可用。

| 关注点 | 策略 | 验证方式 |
|--------|------|----------|
| Go 版本 | 固定 Go 1.20.x（最后支持 Windows 7 的主版本） | `go build` 交叉编译 + Windows 7 实机测试 |
| SQLite | modernc.org/sqlite（纯 Go，无 CGO） | 单元测试 |
| 文件路径 | 使用 `filepath.Join`，不硬编码 `/` | 路径测试 |
| 信号处理 | Windows 无 SIGTERM，用 `os.Interrupt` | 集成测试 |
| Plugin | 统一采用进程外插件；Windows 7 使用回环 TCP IPC | 进程启动与 RPC 集成测试 |
| 网络库 | 避免使用 Windows 不支持的 syscall | 编译检查 |

---

## 7. 版本发布计划

| 版本 | 阶段 | 主要特性 |
|------|------|----------|
| v0.1.0 | Phase 1 | 可启动、健康检查、基础配置 |
| v0.2.0 | Phase 2 | 对话能力、流式输出、多 Provider |
| v0.3.0 | Phase 3 | Tool/Skill/Memory/Planner/Auth |
| v0.4.0 | Phase 4 | MCP Client/Server、Plugin、SDK |
| v1.0.0 | Phase 5 | 生产化、监控、Windows 7 兼容、文档完善 |

**版本规则：**
- 遵循 Semantic Versioning
- v1.0.0 前允许 breaking changes
- 每个版本附带 CHANGELOG
- 发布前通过全部测试

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Plugin 子进程或 RPC 异常 | 扩展能力不可用 | 进程隔离、健康检查、退避重启 |
| Token 估算低于实际请求 | Provider 拒绝 Context | Provider 按完整请求返回精确值或保守上界；失败则不发送 |
| MCP 协议演进 | 兼容性问题 | 版本协商，保持向后兼容 |
| LLM API 变更 | Provider 适配 | 接口抽象层，快速适配 |

---

*最后更新: 2025-07-17*
