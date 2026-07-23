# Skill 系统设计

> 依赖: [Agent/Runtime 架构](../architecture.md)、[Tool](../tool/README.md)、[Context](../context/README.md)

---

## 1. v1 边界

Skill 是启动时从文件系统加载的领域 Prompt 包。它声明说明文本、依赖的 Tool/Skill 和静态 options；Agent 把获准 Skill 的完整 Prompt 加入当前 Provider 请求。Skill 本身不执行 Provider、Tool 或 Session 操作，也不保存对话状态。

v1 明确不提供：

- 运行时 install、uninstall、enable、disable 或 reload；
- Registry/Git 下载、自动更新或文件 watcher；
- 通过自由文本匹配 LLM 回复来“激活” Skill；
- Skill 专属可执行 Tool、独立重试器、调度器或状态持久化；
- Skill mutation Remote API、管理 Tool 或全局 SSE。

运维方在 Runtime 启动前部署 Skill 目录；目录、配置或内容变化需要重启。

## 2. 包格式

每个 `skills.dir` 直接子目录是一个候选包，必须包含 `SKILL.md`：

```text
skills/
└── web-scraper/
    ├── SKILL.md
    ├── scripts/
    ├── references/
    └── assets/
```

`scripts`、`references` 和 `assets` 只是资源。脚本只有在 Agent 已获准使用 Shell/File Tool 且 LLM 显式调用时才执行；Skill Manager 不扫描或自动运行它们。

`SKILL.md` 使用 YAML frontmatter，后接 Prompt body：

```markdown
---
name: web-scraper
description: Extract structured data from web pages.
version: "1.0.0"
author: example
tools: [http, file_write]
skills: []
options:
  max_pages: 20
---

# Web Scraper

Use the HTTP tool to fetch each page, validate the response, and write only
the requested structured output.
```

| 字段 | 类型 | 必填 | 规则 |
|------|------|:----:|------|
| `name` | string | 是 | 与目录名相同；`^[a-z0-9][a-z0-9-]{0,63}$` |
| `description` | string | 是 | 非空，用于只读列表和 Prompt 标题 |
| `version` | string | 否 | 非空时为 SemVer |
| `author` | string | 否 | 展示字段 |
| `tools` | []string | 否 | 已注册 Tool 名称的精确依赖 |
| `skills` | []string | 否 | 其他 Skill 名称；必须无环 |
| `options` | object | 否 | JSON-compatible 顶层配置 |

未知 frontmatter 字段、重复列表项、空 body、YAML alias、非字符串 map key 或非 JSON-compatible option 都拒绝。单个 `SKILL.md` 最大 1 MiB；description 最大 4096 UTF-8 bytes，Prompt body 最大 256 KiB，每类依赖最多 64 个。固定限制不增加配置项。

## 3. 配置与 Agent 绑定

- 根 `skills.dir` 决定唯一扫描目录。
- `skills.per_skill.<name>.enabled=false` 使该 Skill 不可用于任何 Agent。
- root 与 Agent options 按 [配置契约](config.md) 合并并在启动时冻结。
- `agents[].skills` 是精确 allowlist；空数组表示该 Agent 不使用 Skill。
- Skill 的全部递归依赖也必须 enabled，并在同一 Agent allowlist 中。
- Skill 声明的每个 Tool 必须已注册，并被该 Agent 的 Tool allowlist 允许。

任一 Agent 引用缺失、disabled、error 或越权依赖时，启动绑定校验失败；不得在运行中静默删除依赖。

## 4. 请求集成

每个 turn 开始时，Agent 从同一 Config snapshot 解析 Skill 集合。依赖按拓扑顺序排在使用者之前，同层按 name 升序；每个 Skill 生成一个受保护的 system message，包含 name、合并后的 options JSON 和原始 body。消息只进入本次 Context Build 输入，不写入 Session snapshot。

Skill 不增加另一层 LLM 或 Tool retry。Provider 重试、Tool 执行、Context 预算和 Session 原子提交继续由各自模块唯一负责。完整流程见 [调用契约](invocation.md)。

## 5. 文档索引

| 文件 | 内容 |
|------|------|
| [manager.md](manager.md) | 加载、状态、依赖与并发 |
| [invocation.md](invocation.md) | Agent/Context/Tool 集成 |
| [config.md](config.md) | root 与 Agent option 合并 |
| [registry.md](registry.md) | 部署边界；v1 无运行时 Registry |
| [errors.md](errors.md) | 稳定错误与传播 |
| [observability.md](observability.md) | 日志、指标与 Remote 边界 |
| [decisions.md](decisions.md) | 已确定决策 |
| [checklist.md](checklist.md) | 实现与验证清单 |

---

*最后更新: 2026-07-22*
