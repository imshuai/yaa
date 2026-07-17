# Skill 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/skill/` (原计划单文件 `docs/skill.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.8, `docs/tool/` 全系列

---

## 1. 概述

### 1.1 什么是 Skill

Skill 是 Yaa! 中**比 Tool 更高层的能力抽象**。

| 层级 | 抽象 | 类比 |
|------|------|------|
| Tool | 原子操作（Shell、HTTP、File） | 系统调用 |
| **Skill** | **多步骤工作流 + 领域知识 + Tool 编排** | **应用程序** |
| Agent | 人格 + Skill/Tool 集合 + Memory | 用户会话 |

一个 Skill 封装了：
- **领域知识** — 针对特定任务的专业指令（Prompt）
- **Tool 编排** — 声明依赖哪些 Tool，以及如何组合使用
- **可选资源** — 脚本、参考文档、模板等
- **配置** — 可调参数（超时、重试、路径等）

### 1.2 设计理念

Yaa! 的 Skill 系统借鉴 PicoClaw 的 Skill 设计，但做了关键扩展：

| 特性 | PicoClaw | Yaa! |
|------|----------|------|
| 组织形式 | 文件目录 + SKILL.md | 相同 |
| 加载方式 | 启动扫描 | 相同 + 运行时动态安装 |
| 触发方式 | LLM 根据 description 决定 | 相同 + 显式调用 |
| Tool 绑定 | 隐式（Skill 引导 LLM 调用 Tool） | **显式声明 + 自动注册 Skill 专属 Tool** |
| 可安装 | 手动放置 | **Registry + Git URL + 本地路径** |
| 运行时管理 | 无 | **install / uninstall / enable / disable** |
| 嵌套调用 | 不支持 | **支持 Skill 嵌套** |
| 生命周期 | 静态 | **动态（安装→启用→禁用→卸载）** |

### 1.3 核心原则

1. **Prompt First** — Skill 的核心是 Prompt（领域知识），不是代码
2. **Progressive Disclosure** — 三级加载，节省 Context
3. **Tool Composition** — Skill 编排已有 Tool，不重复造轮子
4. **Self-Contained** — 每个 Skill 是自包含的目录
5. **Hot-Pluggable** — 运行时可安装/卸载/启用/禁用
6. **Nestable** — Skill 可调用其他 Skill

---

## 2. Skill 目录结构

### 2.1 标准 Skill 包

```text
skills/
└── web-scraper/                    # Skill 名称 = 目录名
    ├── SKILL.md                    # 必需：Skill 定义文件
    ├── config.yaml                 # 可选：Skill 配置
    ├── scripts/                    # 可选：脚本资源
    │   ├── parse_html.py
    │   └── extract_tables.sh
    ├── references/                 # 可选：参考文档
    │   ├── selectors.md
    │   └── api_docs.md
    ├── assets/                     # 可选：静态资源
    │   └── template.json
    └── tools/                      # 可选：Skill 专属 Tool
        ├── custom_parser.go        # Go 源码（需编译）
        └── custom_parser.so        # 或预编译插件
```

### 2.2 SKILL.md 格式

```markdown
---
name: web-scraper
description: >
  Web scraping skill. Use when the user asks to extract data from
  web pages, scrape tables, or collect structured data from URLs.
  Supports HTML parsing, CSS selectors, and table extraction.
version: "1.0.0"
author: "iDodev"
tools:
  - http
  - file_write
  - shell
options:
  timeout: 60
  max_pages: 50
---

# Web Scraper

## 工作流程

1. 使用 `http` Tool 获取目标 URL 的 HTML
2. 调用 `scripts/parse_html.py` 解析 HTML
3. 提取表格数据并格式化
4. 使用 `file_write` 保存结果

## 使用示例

...详细指令...
```

### 2.3 Frontmatter 字段

| 字段 | 类型 | 必需 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | Skill 唯一标识，小写中划线 |
| `description` | string | ✅ | Skill 描述，LLM 依据此决定是否触发 |
| `version` | string | ❌ | 语义化版本号 |
| `author` | string | ❌ | 作者 |
| `tools` | []string | ❌ | 依赖的 Tool 列表 |
| `skills` | []string | ❌ | 依赖的其他 Skill（嵌套） |
| `options` | map | ❌ | Skill 级配置参数 |
| `auto_load` | bool | ❌ | 启动时是否自动加载（默认 true） |

### 2.4 三级渐进式加载

```text
Level 1: Metadata（始终在 Context 中，~100 words）
  ┌──────────────────────────────────────┐
  │ name: "web-scraper"                   │
  │ description: "Web scraping skill..."  │
  │ ← LLM 读取此信息决定是否使用该 Skill  │
  └──────────────────────────────────────┘
                    │
                    ▼ 触发后加载
Level 2: SKILL.md Body（触发时加载，<5k words）
  ┌──────────────────────────────────────┐
  │ # Web Scraper                         │
  │ ## 工作流程                           │
  │ ## 使用示例                           │
  │ ← Agent 获得完整工作指令              │
  └──────────────────────────────────────┘
                    │
                    ▼ 按需加载
Level 3: Bundled Resources（按需加载，无限制）
  ┌──────────────────────────────────────┐
  │ scripts/parse_html.py    ← 执行     │
  │ references/selectors.md  ← 读取     │
  │ assets/template.json     ← 复制     │
  └──────────────────────────────────────┘
```

**加载成本对比：**

| 级别 | Token 开销 | 时机 |
|------|-----------|------|
| Level 1 | ~100 tokens | Runtime 启动时，始终在 Context |
| Level 2 | 1,000-5,000 tokens | Skill 被触发时 |
| Level 3 | 按需 | Agent 判断需要时 |

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [manager.md](manager.md) | Skill Manager — 加载、注册、生命周期管理、嵌套调用 |
| [invocation.md](invocation.md) | Skill 调用流程 — 触发机制、Prompt 注入、Tool 编排、Agent Loop 集成 |
| [registry.md](registry.md) | Skill Registry — 来源、安装、卸载、版本管理 |
| [config.md](config.md) | 配置参考 — 全局配置、Agent 级别、Skill 级别 |
| [errors.md](errors.md) | 错误处理与重试策略 |
| [observability.md](observability.md) | 可观测性 — 日志、指标、Remote API 事件 |
| [decisions.md](decisions.md) | 设计决策（SD-001 ~ SD-NNN） |
| [checklist.md](checklist.md) | 实现检查清单 |
