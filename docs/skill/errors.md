# Skill 错误契约

> 上级: [Skill 系统设计](README.md)

---

## 1. Stable sentinels

```go
var (
    ErrSkillDirectoryUnavailable = errors.New("skill: directory unavailable")
    ErrSkillNotFound             = errors.New("skill: not found")
    ErrSkillInvalid              = errors.New("skill: invalid package")
    ErrSkillDuplicate            = errors.New("skill: duplicate name")
    ErrSkillDependencyMissing    = errors.New("skill: dependency missing")
    ErrSkillDependencyCycle      = errors.New("skill: dependency cycle")
    ErrSkillDisabled             = errors.New("skill: disabled")
    ErrSkillToolUnavailable      = errors.New("skill: tool unavailable")
    ErrSkillPermissionDenied     = errors.New("skill: dependency not allowed")
    ErrSkillAgentNotFound        = errors.New("skill: agent not found")
    ErrSkillOptionsInvalid       = errors.New("skill: invalid options")
)
```

底层 path/YAML/SemVer/JSON/Tool 错误用 `%w` 保留 cause；调用方用 `errors.Is` 判断稳定分类，不解析字符串。对外错误和日志不得包含 Prompt、options value、绝对路径或配置 Secret。

## 2. 启动传播

`skill.Load` 是 all-or-nothing。目录不可读、任一候选包无效、依赖图错误或任一 Agent binding 失败都会阻止 Runtime Ready；不得跳过失败包后让引用它的 Agent 静默少一个 system prompt。

| 阶段 | 稳定分类 |
|------|----------|
| 目录/文件读取 | `ErrSkillDirectoryUnavailable` 或 `ErrSkillInvalid` |
| frontmatter/body/限制 | `ErrSkillInvalid` |
| 重复 name | `ErrSkillDuplicate` |
| 缺失或循环 Skill 依赖 | `ErrSkillDependencyMissing` / `ErrSkillDependencyCycle` |
| Agent 引用 disabled Skill | `ErrSkillDisabled` |
| Tool 不存在/disabled | `ErrSkillToolUnavailable` |
| Agent allowlist 不含依赖 | `ErrSkillPermissionDenied` |
| options 类型、敏感 key 或大小 | `ErrSkillOptionsInvalid` |

Load 不重试文件读取，也不启动 watcher。运维修正文件或配置后重启。

## 3. Turn 传播

成功启动后 Manager 不再读取磁盘，`ResolveForAgent` 只读取已验证 snapshot。未知 Agent 是调用方编程/路由错误；当前 turn 返回内部错误，不能改成空 Skill 集合继续。

Skill 没有独立 execution timeout 或 retry。Context overflow、Provider、Tool、Session 和 request context 错误保持原模块的错误链；Skill 层不重分类。

## 4. Remote 映射

Skill Remote API 只有两个只读 GET：

| 条件 | HTTP / code |
|------|-------------|
| Skill name 不存在 | 404 / `40401` |
| Runtime 尚未完成 Skill Load | 503 / `50301` |
| 其他 handler 错误 | 500 / `50001` |

Disabled Skill 仍是已知资源，GET 返回 `status:"disabled"`；它不是 404。启动 Load 错误只出现在 readiness、脱敏日志和内部 health cause 中。

---

*最后更新: 2026-07-22*
