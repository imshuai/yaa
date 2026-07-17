# Skill 设计决策

> 文档路径: `docs/skill/decisions.md`
> 上级: `docs/skill/README.md` §9

---

## 9. 设计决策

### SD-001: Skill 以文件目录组织，不以代码注册

**决策：** Skill 以文件系统目录 + SKILL.md 的形式组织，而非通过代码 `Register()` 注册。

**理由：**
- 文件目录天然支持热更新和动态安装
- 非开发者也能编写和安装 Skill（只需写 Markdown）
- 与 PicoClaw Skill 格式兼容，便于迁移
- 代码注册方式适合内置 Tool，不适合可分发的能力包

**影响：** Skill Manager 需要文件扫描和解析逻辑。

---

### SD-002: Skill 的核心是 Prompt，不是代码

**决策：** Skill 的核心能力来自 SKILL.md 中的 Prompt（领域知识），而非可执行代码。

**理由：**
- LLM 已具备通用能力，Skill 只需提供领域知识和工作流指引
- Prompt 驱动的方式跨平台、跨语言
- 降低 Skill 开发门槛
- 代码（脚本）是可选的辅助资源，不是 Skill 的必需部分

**影响：** Skill 不直接执行逻辑，而是通过 Prompt 引导 LLM 调用 Tool。

---

### SD-003: 三级渐进式加载

**决策：** Skill 采用三级加载：Metadata（始终在 Context）→ Body（触发时加载）→ Resources（按需加载）。

**理由：**
- Context 窗口是稀缺资源
- 大部分 Skill 在一次对话中不会被使用
- 即使被使用，也不需要全部资源同时在 Context 中

**影响：** Agent 需要实现 Skill 触发检测和按需加载逻辑。

---

### SD-004: Skill 触发使用 Function Call 模式

**决策：** 默认使用 Function Call 模式（将 Skill 作为 `use_skill` Function 暴露给 LLM），对不支持的 Provider 回退到文本匹配。

**理由：**
- Function Call 准确率远高于文本匹配
- 避免误触发（LLM 提到 Skill 名称但并非要使用）
- 标准 Provider 接口，无需特殊处理

**影响：** 需要处理 Provider 能力差异，实现回退逻辑。

---

### SD-005: Skill 声明 Tool 依赖但不直接调用

**决策：** Skill 在 frontmatter 中声明 `tools` 依赖，但不直接调用 Tool。Tool 调用由 LLM 在 Skill Prompt 指导下自主决策。

**理由：**
- 保持 LLM 的灵活性（可适应不同情况选择不同 Tool）
- Skill 不需要实现 Tool 调用逻辑
- 与 Agent Loop 自然集成

**影响：** Skill 的 `tools` 字段主要用于权限检查和 Tool 可用性保证。

---

### SD-006: 支持 Skill 嵌套但限制深度

**决策：** Skill 可声明依赖其他 Skill（`skills` 字段），最大嵌套深度默认 3 层，禁止循环依赖。

**理由：**
- 嵌套允许 Skill 组合，构建复杂工作流
- 限制深度防止无限递归和 Context 爆炸
- 循环依赖在加载时检测

**影响：** Skill Manager 需要依赖解析和循环检测逻辑。

---

### SD-007: Skill 专属 Tool 支持声明式和编译式两种

**决策：** Skill 的 `tools/` 目录支持两种 Tool：声明式（JSON 定义 + Shell 执行）和编译式（Go plugin）。

**理由：**
- 声明式 Tool 无需编译，适合简单封装（调用脚本/命令）
- 编译式 Tool 性能好，适合复杂逻辑
- 两种方式共存，覆盖不同需求

**影响：** Tool Manager 需要支持声明式 Tool 的解析和执行。

---

### SD-008: Registry 安装支持但不是必需

**决策：** Yaa! 提供 Registry 服务规划，但 Skill 安装不强制依赖 Registry。本地和 Git 来源始终可用。

**理由：**
- Registry 是增值服务，不是核心功能
- 私有部署可能无法访问外部 Registry
- 本地安装满足基本需求

**影响：** Registry 相关功能可独立部署和配置。

---

### SD-009: Skill Prompt 注入为 System Message

**决策：** Skill Body 加载后，以 System Message 的形式注入 Context，而非 User Message。

**理由：**
- System Message 表明这是"指令"而非"对话"
- LLM 对 System Message 有更高的遵从度
- 不污染 User Message 的语义

**影响：** Context 中可能出现多个 System Message（原始 + 每个 Skill 一个）。

---

### SD-010: Skill Prompt 注入后默认保留

**决策：** Skill Prompt 注入 Context 后默认保留（不自动移除），支持后续追问。

**理由：**
- 用户可能在同一对话中多次使用同一 Skill
- 移除后再次加载浪费 Token
- 可通过配置 `auto_cleanup` 改为完成后自动移除

**影响：** 多轮对话中 Context 可能积累多个 Skill Prompt，需要 Token 预算控制。

---

### SD-011: 嵌套 Skill 的 Body 不自动加载

**决策：** 当 Skill A 依赖 Skill B 时，只自动注入 B 的 Metadata（Level 1），B 的 Body（Level 2）由 Agent 按需触发。

**理由：**
- 避免一次性加载所有依赖 Skill 的 Body，导致 Context 爆炸
- 尊重渐进式加载原则
- Agent（LLM）能根据实际情况判断是否需要子 Skill 的详细指令

**影响：** 嵌套 Skill 的协作依赖 LLM 的自主判断。

---

### SD-012: Skill 版本使用语义化版本号

**决策：** Skill 使用语义化版本号（SemVer）`MAJOR.MINOR.PATCH`。

**理由：**
- 行业标准，开发者熟悉
- 支持版本范围匹配
- 自动更新可基于版本号判断是否需要升级

**影响：** Registry 和安装记录需要版本比较逻辑。

---

### SD-013: Skill 安装支持回滚

**决策：** Skill 更新时保留旧版本备份，更新失败可回滚到上一个版本。

**理由：**
- 更新可能引入不兼容变更
- 自动更新失败不应导致 Skill 不可用
- 回滚是安全网

**影响：** 安装目录需要管理多个版本，或保留 `.bak` 备份。

---

## 10. 模块关系

```text
┌──────────────────────────────────────────────────────────────┐
│                           Agent                                │
│                                                                │
│  ┌──────────┐    ┌────────────┐    ┌─────────────────┐       │
│  │ Context   │◄───│ Skill Mgr  │───►│  Tool Manager    │       │
│  │ Manager   │    │            │    │                  │       │
│  └──────────┘    └─────┬──────┘    └────────┬─────────┘       │
│                        │                     │                  │
│               ┌────────▼────────┐    ┌──────▼──────┐          │
│               │ Skill Registry  │    │ Tool 实例    │          │
│               │ (Git/Local/     │    │              │          │
│               │  Registry)      │    │ 通用执行类   │          │
│               └─────────────────┘    │ 配置管理类   │          │
│                                     │ 内视管理类   │          │
│               ┌─────────────────┐    │              │          │
│               │ Skill 目录       │    │ Skill 专属   │          │
│               │ ┌─────────────┐ │    │ Tool (声明式 │          │
│               │ │ SKILL.md    │ │    │  /编译式)    │          │
│               │ │ scripts/    │ │    └──────────────┘          │
│               │ │ references/ │ │                               │
│               │ │ assets/     │ │                               │
│               │ │ tools/      │ │                               │
│               │ └─────────────┘ │                               │
│               └─────────────────┘                               │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐     │
│  │  Provider                                               │     │
│  │  ┌──────────────┐  ┌──────────────┐                  │     │
│  │  │ use_skill    │  │ ToolDef[]    │                  │     │
│  │  │ (Function)   │  │ (from Skill  │                  │     │
│  │  └──────────────┘  │  tools)      │                  │     │
│  │                     └──────────────┘                  │     │
│  └──────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────┘

依赖方向:
  Agent → Skill Manager (触发、加载 Prompt)
  Skill Manager → Tool Manager (确保 Tool 可用、注册专属 Tool)
  Skill Manager → Skill Registry (安装/卸载)
  Skill Manager → Context Manager (Prompt 注入)
  Skill Manager → Provider (use_skill Function)
  Skill Registry → Git / Local FS / Registry Server (来源)
  Skill 专属 Tool → Tool Manager (注册)
```

**依赖关系：**
- Skill Manager 依赖 Tool Manager（Tool 可用性检查、专属 Tool 注册）
- Skill Manager 不依赖 Provider 实现，但通过 Agent 间接使用 Provider 的 Function Calling
- Skill 目录是纯文件系统结构，不依赖任何运行时组件
- Skill Registry 依赖网络和文件系统，但不依赖 Runtime 核心
