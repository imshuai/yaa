# Skill 配置契约

> 上级: [Skill 系统设计](README.md)
> 根字段 owner: [Config reference](../config/reference.md)

---

## 1. 类型

`SkillsConfig`/`SkillItemConfig` 与 `AgentConfig`/`AgentSkillConfig` 只在 [Config reference 的 Skill 节点](../config/reference.md#7-skills-节点)和 [Agent 节点](../config/reference.md#3-agents-节点)定义。本文件不维护重复 Go struct；这里只规定 Skill Manager 如何解释这些 canonical 字段。

`skills.dir` 默认 `"./skills"`，相对主配置文件目录解析。`per_skill` 和 `skills_config` 默认空 map。一个 `per_skill.<name>` entry 出现但省略 `enabled` 时由 defaulting 阶段补为 true；strict decode 后不依赖 Go bool 零值猜测 presence。

## 2. 示例

```yaml
skills:
  dir: "./skills"
  per_skill:
    web-scraper:
      enabled: true
      options:
        max_pages: 50
    internal-admin:
      enabled: false

agents:
  - id: web-agent
    provider: openai
    model: gpt-4o
    tools: [http, file_write]
    skills: [web-scraper]
    skills_config:
      web-scraper:
        options:
          max_pages: 20
```

`agents[].skills` 是精确 allowlist；空数组表示不注入任何 Skill。它不是 deny/allow union，也不接受 object 形式。`skills_config` 的每个 key 必须同时出现在该 Agent 的 `skills` 中。

## 3. Options 合并

对每个 Agent/Skill，按以下优先级做顶层 shallow merge，后者覆盖同名 key：

```text
SKILL.md frontmatter options
  <- skills.per_skill.<name>.options
  <- agents[].skills_config.<name>.options
```

不递归 merge object，不拼接 array，不把 `null` 解释为删除；`null` 是普通 JSON value。合并完成后深拷贝并冻结，标准 JSON 编码不得超过 64 KiB。

Options 只能包含 JSON-compatible scalar、array 和 string-keyed object，不接受 NaN/Infinity、YAML timestamp/tag/alias、函数或循环引用。它们会进入 Provider system prompt，因此不得保存凭据。Validator 递归规范化 key（Unicode case-fold，`-` 转 `_`），并拒绝以下 exact key：

```text
api_key, password, secret, token, access_token, refresh_token,
authorization, cookie, set_cookie, private_key, client_secret
```

凭据必须放在 Provider、Tool、MCP 或 Plugin 的专用 Secret 字段中，不能借 Skill options 绕过脱敏边界。

## 4. 校验与重启

Config 基础校验负责类型、未知字段、路径和 option 编码；Skill Manager 加载后执行第二阶段 binding：

1. `per_skill` 和 `skills_config` name 必须对应已加载目录；
2. Agent allowlist 不得引用 disabled Skill；
3. 递归 Skill 依赖必须也在 Agent allowlist；
4. Tool 依赖必须已注册、enabled 且被 Agent 允许；
5. 合并后的 option 大小和敏感 key 检查通过。

`skills.dir`、`per_skill`、`agents[].skills` 和 `agents[].skills_config` 全部 restart-required。ReloadManager 发现这些变化时整批不发布 candidate，返回 `ReloadResult{RestartRequired:true}`。

---

*最后更新: 2026-07-22*
