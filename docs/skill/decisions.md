# Skill 设计决策

> 上级: [Skill 系统设计](README.md)

---

## SK-001: Skill 是 Prompt 包

Skill 不拥有执行器。Agent 把 Prompt 放入候选 Provider request；Tool、Provider、Context 和 Session 继续拥有各自行为。

## SK-002: 启动期 all-or-nothing

Runtime 只在启动时扫描 `skills.dir`，验证全部包、依赖和 Agent binding 后一次发布不可变 Manager。运行期文件、配置和 Skill 集合不热更新。

## SK-003: 显式 Agent allowlist

`agents[].skills` 是精确 allowlist，空数组表示不使用 Skill。递归 Skill 依赖也必须显式列出，避免一个获准 Skill 隐式扩大 Agent 能力。

## SK-004: Tool 权限不继承

Skill 的 `tools` 只是依赖声明，不能注册、启用或授权 Tool。Agent Tool allowlist 是唯一执行权限边界。

## SK-005: 静态完整 Prompt

每个 turn 都把配置 Skill 的完整 body 作为 protected system message。v1 不从 assistant 自由文本猜测激活意图，不引入 `use_skill` Tool、隐藏 Provider round 或 LRU Prompt cache。

## SK-006: 不实现运行时 Registry

Skill 包由部署系统在 Runtime 启动前原子放入目录。网络下载、签名信任、安装回滚、动态管理 API 和更新 watcher 留给定义完整供应链契约的后续版本。

## SK-007: Options 只做浅合并

frontmatter、root 和 Agent options 按优先级做顶层 shallow merge。递归 merge、array append 和 null-delete 会引入难以验证的隐式语义，v1 不实现。

## SK-008: 只读 Remote

Remote 只投影固定 `SkillView`，省略 path 和 options；没有 mutation route、SSE 或 Skill 管理 Tool。

---

*最后更新: 2026-07-22*
