# Yaa! 目录结构

> 本文件描述项目的目标目录结构。当前项目仍处于架构设计阶段，部分目录尚未包含实际代码。

```text
yaa/
├── README.md                      # 项目介绍
├── LICENSE                        # 开源许可证
├── Makefile                       # 构建、测试、安装等快捷命令
├── go.mod                         # Go 模块定义
├── go.sum
├── .gitignore
├── .editorconfig
│
├── cmd/                           # 入口点
│   └── yaa/                       # 主程序入口
│       └── main.go                # 编译为 yaa / yaa.exe
│
├── internal/                      # 内部实现（不对外暴露）
│   │
│   ├── runtime/                   # Runtime 核心
│   │   ├── runtime.go             # Runtime 生命周期管理
│   │   ├── options.go              # Runtime 启动选项
│   │   └── manager.go              # Agent 实例管理器
│   │
│   ├── agent/                     # Agent 定义与生命周期
│   │   ├── agent.go               # Agent 接口与实现
│   │   ├── options.go             # Agent 配置选项
│   │   └── lifecycle.go           # Agent 启动/停止/重启
│   │
│   ├── session/                   # 会话管理
│   │   ├── session.go             # Session 定义
│   │   ├── manager.go             # Session 管理器
│   │   └── store.go               # Session 持久化
│   │
│   ├── context/                   # 上下文管理
│   │   ├── context.go             # Context 定义
│   │   ├── manager.go             # Context 管理器
│   │   └── window.go             # 上下文窗口策略
│   │
│   ├── task/                      # 任务调度
│   │   ├── task.go                # Task 定义
│   │   ├── scheduler.go           # 任务调度器
│   │   └── queue.go               # 任务队列
│   │
│   ├── planner/                   # 规划器
│   │   ├── planner.go             # Planner 接口
│   │   └── simple.go              # 默认简单规划器
│   │
│   ├── memory/                    # 记忆系统
│   │   ├── memory.go              # Memory 接口
│   │   ├── short_term.go          # 短期记忆
│   │   ├── long_term.go           # 长期记忆
│   │   └── store.go               # 记忆持久化
│   │
│   ├── tool/                      # Tool 系统
│   │   ├── tool.go                # Tool 接口定义
│   │   ├── registry.go            # Tool 注册与发现
│   │   ├── manager.go             # Tool 管理器
│   │   ├── executor.go            # Tool 执行器
│   │   └── builtin/               # 内置 Tool
│   │       ├── shell.go
│   │       ├── http.go
│   │       └── file.go
│   │
│   ├── skill/                     # Skill 系统
│   │   ├── skill.go               # Skill 接口定义
│   │   ├── registry.go            # Skill 注册与发现
│   │   ├── manager.go             # Skill 管理器
│   │   └── loader.go              # Skill 加载器（文件系统扫描）
│   │
│   ├── mcp/                       # MCP 支持
│   │   ├── client.go              # MCP Client
│   │   ├── server.go              # MCP Server
│   │   └── transport.go           # MCP 传输层（stdio/SSE/WS）
│   │
│   ├── provider/                  # LLM Provider 层
│   │   ├── provider.go            # Provider 接口定义
│   │   ├── manager.go            # Provider 管理器
│   │   ├── types.go              # 请求/响应类型
│   │   └── providers/            # 各厂商实现
│   │       ├── openai.go
│   │       ├── claude.go
│   │       ├── gemini.go
│   │       ├── deepseek.go
│   │       ├── qwen.go
│   │       ├── ollama.go
│   │       ├── lmstudio.go
│   │       ├── azure.go
│   │       └── openrouter.go
│   │
│   ├── config/                    # 配置系统
│   │   ├── config.go              # 配置定义与加载
│   │   ├── loader.go              # 配置文件加载（YAML/TOML/JSON）
│   │   ├── validator.go           # 配置校验
│   │   └── watcher.go            # 配置热更新
│   │
│   ├── api/                       # Remote API
│   │   ├── server.go              # API Server 统一入口
│   │   ├── router.go              # 路由注册
│   │   ├── middleware.go          # 中间件（鉴权/日志/限流）
│   │   ├── http/                  # HTTP API
│   │   │   ├── handler.go
│   │   │   └── handler_*.go
│   │   ├── ws/                    # WebSocket
│   │   │   ├── handler.go
│   │   │   └── hub.go
│   │   └── sse/                   # Server-Sent Events
│   │       └── handler.go
│   │
│   ├── auth/                      # 认证与授权
│   │   ├── auth.go                # 认证接口
│   │   ├── token.go               # Token 管理
│   │   └── policy.go              # 权限策略
│   │
│   ├── storage/                   # 存储层
│   │   ├── storage.go             # Storage 接口
│   │   ├── sqlite.go              # SQLite 实现
│   │   ├── bbolt.go               # BoltDB 实现
│   │   └── memory.go              # 内存存储（测试用）
│   │
│   ├── plugin/                    # 插件系统
│   │   ├── plugin.go              # Plugin 接口
│   │   ├── manager.go             # 插件管理器
│   │   └── loader.go              # 插件加载器
│   │
│   ├── health/                    # 健康检查
│   │   └── health.go
│   │
│   └── version/                   # 版本信息
│       └── version.go
│
├── pkg/                           # 对外暴露的公共包
│   ├── remoteapi/                 # Remote API 客户端 SDK
│   │   ├── client.go
│   │   └── types.go
│   ├── types/                     # 公共类型定义
│   │   ├── message.go
│   │   ├── agent.go
│   │   └── task.go
│   ├── errors/                    # 统一错误定义
│   │   └── errors.go
│   └── utils/                     # 工具函数
│       ├── id.go
│       ├── json.go
│       └── net.go
│
├── configs/                       # 示例配置
│   ├── default.yaml               # 默认配置
│   ├── openai.yaml                # OpenAI Provider 示例
│   └── ollama.yaml                # Ollama Provider 示例
│
├── scripts/                       # 脚本
│   ├── build.sh                   # 构建脚本
│   ├── build-windows.sh           # Windows 交叉编译
│   └── release.sh                 # 发布脚本
│
├── deployments/                   # 部署相关
│   ├── docker/
│   │   ├── Dockerfile
│   │   └── docker-compose.yml
│   └── systemd/
│       └── yaa.service
│
└── docs/                          # 设计文档
    ├── architecture.md            # 整体架构设计
    ├── directory.md               # 目录结构说明（本文件）
    ├── provider.md                # Provider 层设计
    │
    ├── remote-api/               # Remote API 设计（多文件）
    │   ├── INDEX.md              # 索引 + 概述
    │   ├── agent.md              # Agent 管理 API
    │   ├── session.md            # Session 管理 API
    │   ├── conversation.md       # 对话 API
    │   ├── tool.md               # Tool 管理 API
    │   ├── skill.md              # Skill 管理 API
    │   ├── memory.md             # Memory 管理 API
    │   ├── provider.md           # Provider 管理 API
    │   ├── mcp.md                # MCP 管理 API
    │   ├── auth.md               # 认证 API
    │   └── system.md             # 系统管理 API
    │
    ├── tool/                     # Tool 系统设计（13 files）
    │   ├── README.md             # 索引 + 设计目标 + 核心接口
    │   ├── manager.md            # Tool Manager
    │   ├── provider.md           # Tool 与 Provider 衔接
    │   ├── builtin.md            # 内置 Tool 总览
    │   ├── config-tools.md       # Config 系列工具
    │   ├── introspection.md      # 内视与管理系列工具
    │   ├── custom.md             # 自定义 Tool
    │   ├── context.md            # Tool 与 Context 交互
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── config-ref.md         # 配置参考
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── skill/                     # Skill 系统设计（9 files）
    │   ├── README.md              # 索引 + 概述 + 三级加载
    │   ├── manager.md            # Skill Manager
    │   ├── invocation.md         # Skill 调用流程
    │   ├── registry.md           # Skill Registry
    │   ├── config.md             # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── memory/                   # Memory 系统设计（10 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── architecture.md       # 三层记忆架构
    │   ├── lifecycle.md          # 记忆生命周期管理
    │   ├── storage.md            # 存储后端与向量索引
    │   ├── integration.md        # 与 Session/Context/Agent 集成
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── session/                  # Session 系统设计（11 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── lifecycle.md          # 生命周期管理
    │   ├── persistence.md        # 持久化
    │   ├── messaging.md         # 消息管理
    │   ├── concurrency.md        # 并发模型
    │   ├── integration.md        # 与 Agent/Context/Memory 集成
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── config/                    # Config 系统设计（11 files）
    │   ├── README.md             # 索引 + 概述
    │   ├── overview.md           # 设计理念 + 核心接口
    │   ├── loading.md            # 配置加载流程
    │   ├── reference.md          # 完整配置参考
    │   ├── envvar.md             # 环境变量引用机制
    │   ├── validation.md         # 配置校验与默认值
    │   ├── hot-reload.md         # 配置热更新
    │   ├── formats.md            # 多格式支持
    │   ├── migration.md          # 配置迁移与兼容
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── context/                  # Context 系统设计（7 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── manager.md            # Context Manager 详解
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── planner/                  # Planner 系统设计（10 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── planner.md            # Planner 接口与实现
    │   ├── task.md               # Task 调度系统
    │   ├── execution.md          # 计划执行流程
    │   ├── integration.md        # 与 Agent/Tool/Skill 集成
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── storage/                  # Storage 系统设计（7 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── sqlite.md             # SQLite 实现
    │   ├── alternatives.md       # BoltDB 与内存存储
    │   ├── integration.md        # 与各模块集成
    │   ├── config-ref.md        # 配置参考
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── auth/                     # Auth 系统设计（7 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── authentication.md     # 认证机制
    │   ├── authorization.md      # 授权机制（RBAC）
    │   ├── integration.md        # 与 Remote API 中间件集成
    │   ├── config-ref.md        # 配置参考
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── mcp/                     # MCP 系统设计（10 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口 + 双角色说明
    │   ├── client.md             # MCP Client（连接外部 Server、Tool 映射）
    │   ├── server.md             # MCP Server（暴露 Yaa! 能力）
    │   ├── transport.md          # 传输层（stdio/SSE/WS）
    │   ├── integration.md        # 与 Tool/Agent/Config 集成
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    ├── plugin/                  # Plugin 系统设计（10 files）
    │   ├── README.md             # 索引 + 概述 + 核心接口
    │   ├── interface.md         # Plugin 接口详解
    │   ├── manager.md            # Plugin Manager
    │   ├── loader.md             # Plugin Loader
    │   ├── integration.md        # 与各模块集成
    │   ├── config-ref.md        # 配置参考
    │   ├── errors.md             # 错误处理
    │   ├── observability.md      # 可观测性
    │   ├── decisions.md          # 设计决策 + 模块关系
    │   └── checklist.md          # 实现检查清单
    │
    └── roadmap.md                 # 开发路线图
```
