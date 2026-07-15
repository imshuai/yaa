# Yet Another Agent

> 一个现代化、可扩展、长期运行的 Agent Runtime。

**Yet Another Agent**（简称 **Yaa!**）是一个使用 Go 编写的 Agent Runtime。

Yaa! 不提供聊天界面，也不是一个命令行工具。

它作为一个长期运行的后台服务，统一管理 Agent 生命周期、任务调度、会话、上下文、LLM、Tools、Skills、Memory、MCP 等能力，并通过统一的 **Remote API** 对外提供服务。

任何客户端都可以连接到同一个 Yaa! 实例，共享能力、配置与状态。

例如：

- Web UI
- Windows 桌面程序
- VS Code 插件
- JetBrains 插件
- Android / iOS
- CLI
- Home Assistant
- 第三方程序

Yaa! 是整个系统的运行时（Runtime），而不是其中某一个客户端。

---

# 为什么是 Yaa!

Yaa! 希望成为 Agent 的基础设施，而不是另一个聊天软件。

今天，大多数 Agent 工具都围绕某一个模型、某一个客户端或某一种交互方式构建。

Yaa! 选择相反的方向。

LLM、Tool、Skill、Memory、MCP 都只是 Runtime 提供的能力，而不是 Runtime 本身。

Agent 应该拥有统一、稳定、开放的运行时，让不同模型、不同客户端、不同工具能够自由组合，而不是彼此绑定。

---

# 设计原则

Yaa! 坚持以下设计原则。

## Agent First

Agent 是核心。

LLM 只是 Agent 能力的一部分，而不是系统中心。

Runtime 不绑定任何模型，也不依赖任何模型。

---

## Runtime First

Runtime 与客户端彻底解耦。

Yaa! 负责：

- 生命周期
- Session
- Context
- Task
- Planner
- Memory
- Tool
- Skill
- MCP
- Provider

UI、CLI、IDE 插件、移动端都只是客户端。

---

## Remote API First

所有能力均通过统一 Remote API 暴露。

默认支持：

- HTTP API
- WebSocket
- Server-Sent Events (SSE)

未来可扩展：

- gRPC
- Named Pipe
- Unix Domain Socket
- QUIC
- stdio（MCP）

协议可以扩展，接口保持统一。

---

## Provider Independent

Yaa! 不绑定任何模型厂商。

支持但不限于：

- OpenAI
- Claude
- Gemini
- DeepSeek
- Qwen
- Ollama
- LM Studio
- Azure OpenAI
- OpenRouter

新增 Provider 不应影响 Runtime。

---

## Tool First

所有 Tool 都遵循统一接口。

Tool 可以独立开发、安装、升级、授权与配置。

Runtime 自动发现并加载 Tool。

---

## Skill Oriented

复杂能力应抽象为 Skill，而不是不断堆积 Prompt。

Skill 可以组合多个 Tool，形成更高层能力。

---

## Native MCP

Yaa! 原生支持 MCP。

既可以作为 MCP Client，也可以作为 MCP Server。

---

## Embedding Friendly

Yaa! 可以：

- 独立运行
- 嵌入 Go 项目
- 作为后台服务
- 作为容器运行

同一套 Runtime，多种部署方式。

---

## Windows First

优先保证 Windows 7 x64 的兼容性。

无需安装：

- Python
- Node.js
- .NET Runtime

默认编译为单一可执行文件：

```
yaa.exe
```

同时支持：

- Windows
- Linux
- macOS

---

## Configuration over Code

优先通过配置扩展系统，而不是修改代码。

Provider、Tool、Skill、Prompt、权限等均应支持配置化。

---

## Backward Compatibility

稳定性优先于功能数量。

除重大架构调整外，应尽可能保持：

- Remote API
- 配置文件
- 插件接口
- Tool 接口

向后兼容。

---

# 架构

```text
                  Client

 Desktop  WebUI  Mobile  IDE  CLI

              Remote API

                  │

           +-------------+
           | Yaa! Runtime|
           +-------------+

 Planner  Session  Context  Task

 Tool  Skill  Memory  MCP

      Provider Layer

 OpenAI Claude Ollama ...
```

---

# 当前状态

Yaa! 当前仍处于架构设计阶段。

整个项目采用 **Documentation-Driven Development（文档驱动开发）**。

在完成整体架构设计之前，不会进入正式开发阶段。
