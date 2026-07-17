# Skill 错误处理与可观测性

> 文档路径: `docs/skill/errors.md`
> 上级: `docs/skill/README.md` §7-8

---

## 7. 错误处理

### 7.1 错误分类

| 错误类型 | 说明 | 处理方式 |
|---------|------|---------|
| `ErrSkillNotFound` | Skill 不存在 | 返回给 LLM，LLM 可换一种方式 |
| `ErrSkillAlreadyExists` | 安装时同名 Skill 已存在 | 返回给调用方，提示 Force 选项 |
| `ErrSkillDisabled` | Skill 已禁用 | 返回给 LLM |
| `ErrSkillPermissionDenied` | Agent 无权使用该 Skill | 返回给 LLM |
| `ErrSkillDependencyMissing` | 依赖的 Skill 或 Tool 不存在 | 标记 Skill 为 Error 状态 |
| `ErrSkillCircularDependency` | 循环依赖 | 加载时报错 |
| `ErrSkillLoadFailed` | SKILL.md 解析失败 | 记录错误日志，跳过该 Skill |
| `ErrSkillExecutionTimeout` | Skill 执行超时 | 取消执行，返回超时信息 |
| `ErrSkillInstallFailed` | 安装失败（下载/解压/校验） | 回滚，返回错误详情 |

### 7.2 错误传递

```text
Skill 错误 → Skill Manager → Agent → LLM
                                    │
                                    ├─ 可恢复错误 → LLM 调整策略重试
                                    └─ 不可恢复错误 → LLM 告知用户
```

**Agent 层错误处理：**

```go
func (a *Agent) activateSkill(skillName string) error {
    // 1. 查找 Skill
    entry, err := a.skillMgr.Get(skillName)
    if err != nil {
        // ErrSkillNotFound → 告知 LLM 该 Skill 不存在
        return fmt.Errorf("skill '%s' not found: %w", skillName, err)
    }

    // 2. 权限检查
    if !a.skillMgr.CheckPermission(a.id, skillName) {
        // ErrSkillPermissionDenied → 告知 LLM 无权使用
        return ErrSkillPermissionDenied
    }

    // 3. 状态检查
    if entry.Status != SkillStatusLoaded {
        // ErrSkillDisabled → 告知 LLM Skill 不可用
        return fmt.Errorf("skill '%s' is not loaded (status: %v)", skillName, entry.Status)
    }

    // 4. 确保 Tool 可用
    if err := a.skillMgr.EnsureTools(skillName); err != nil {
        // ErrSkillDependencyMissing → 告知 LLM 依赖缺失
        return fmt.Errorf("skill '%s' has missing dependencies: %w", skillName, err)
    }

    // 5. 加载 Prompt
    prompt, err := a.skillMgr.GetPrompt(skillName)
    if err != nil {
        return fmt.Errorf("failed to load skill prompt: %w", err)
    }

    // 6. 注入 Context
    a.context.AppendMessage(provider.Message{
        Role:    "system",
        Content: fmt.Sprintf("## Active Skill: %s\n\n%s", skillName, prompt),
    })

    return nil
}
```

### 7.3 重试策略

Skill 本身不直接执行代码，它通过 Prompt 引导 LLM 调用 Tool。因此重试发生在 Tool 层面。

但 Skill Manager 对以下操作有重试：

| 操作 | 重试条件 | 重试次数 |
|------|---------|---------|
| 加载 Skill | 文件解析失败 | 不重试 |
| 安装 Skill | 下载失败 | 3 次（指数退避） |
| 热更新 | 解析失败 | 不重试 |
| Tool 注册 | 依赖未就绪 | 5 次（1s 间隔） |

---

## 8. 可观测性

### 8.1 日志

```go
// Skill 相关日志事件
m.logger.Info("skill loaded",
    "name", entry.Skill.Name,
    "version", entry.Skill.Version,
    "source", entry.Source,
    "tools", entry.Tools,
)

m.logger.Info("skill triggered",
    "skill", skillName,
    "agent", agentID,
    "session", sessionID,
)

m.logger.Info("skill body injected",
    "skill", skillName,
    "tokens", estimatedTokens,
    "active_skills", activeCount,
)

m.logger.Info("skill installed",
    "name", name,
    "source", source,
    "version", version,
)

m.logger.Warn("skill load failed",
    "path", path,
    "error", err,
)

m.logger.Error("skill circular dependency",
    "skill", name,
    "chain", dependencyChain,
)
```

### 8.2 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `skill_total` | Gauge | source, status | 已注册 Skill 总数 |
| `skill_trigger_total` | Counter | skill, agent | Skill 触发次数 |
| `skill_active` | Gauge | agent | 当前激活 Skill 数 |
| `skill_body_tokens` | Gauge | skill | Skill Body 占用 Token 数 |
| `skill_install_total` | Counter | source | 安装次数 |
| `skill_install_failed_total` | Counter | source, reason | 安装失败次数 |
| `skill_uninstall_total` | Counter | - | 卸载次数 |
| `skill_reload_total` | Counter | skill | 热更新次数 |
| `skill_load_duration` | Histogram | - | Skill 加载耗时 |

### 8.3 Remote API 事件

Skill 状态变化通过 Remote API SSE 推送事件：

| 事件 | 触发时机 | Payload |
|------|---------|---------|
| `skill.loaded` | Skill 加载完成 | name, version, source |
| `skill.triggered` | Skill 被 Agent 触发 | name, agent_id, session_id |
| `skill.completed` | Skill 任务完成 | name, agent_id, session_id, duration |
| `skill.installed` | Skill 安装完成 | name, version, source |
| `skill.uninstalled` | Skill 卸载完成 | name |
| `skill.enabled` | Skill 被启用 | name |
| `skill.disabled` | Skill 被禁用 | name |
| `skill.reloaded` | Skill 热更新完成 | name, old_version, new_version |
| `skill.error` | Skill 发生错误 | name, error, type |

**SSE 示例：**

```json
{
  "event": "skill.installed",
  "data": {
    "name": "web-scraper",
    "version": "1.2.0",
    "source": "registry",
    "installed_at": "2026-07-16T10:30:00Z"
  }
}
```

### 8.4 内置 Tool 集成

Skill 系统与 Tool 系统的内视工具集成：

| Tool | 对应 Skill 数据 |
|------|----------------|
| `skill_list` | 列出所有已安装 Skill |
| `skill_install` | 通过 Agent 安装 Skill |
| `skill_uninstall` | 通过 Agent 卸载 Skill |
| `skill_enable` | 启用 Skill |
| `skill_disable` | 禁用 Skill |

详见 `docs/tool/introspection.md` §6.5.7 和 §6.6。
