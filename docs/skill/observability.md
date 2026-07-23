# Skill 可观测性

> 上级: [Skill 系统设计](README.md)

---

## 1. 日志

使用项目统一的 `slog` 兼容 logger。允许记录 Skill name、status、version、依赖数量、AgentID、duration 和稳定 error class；禁止记录 Prompt、options value、绝对路径、资源正文或 Secret。

| 事件 | level | 必需字段 |
|------|-------|----------|
| `skill.load.completed` | info | loaded, disabled, duration_ms |
| `skill.load.failed` | error | package_name?, error_class, duration_ms |
| `skill.resolve.completed` | debug | agent_id, count |
| `skill.resolve.failed` | error | agent_id, error_class |

一次 `Load` 最多产生一条 completed 或 failed 总结；逐包 debug 日志默认关闭。底层 cause 可以作为结构化 error 写入受控服务端日志，但不进入 Remote response。

## 2. 指标

| 指标 | 类型 | labels | 说明 |
|------|------|--------|------|
| `yaa_skill_current` | Gauge | `status` | loaded/disabled 数量 |
| `yaa_skill_load_total` | Counter | `result` | Manager Load 结果 |
| `yaa_skill_load_duration_seconds` | Histogram | — | 启动加载耗时 |
| `yaa_skill_resolve_total` | Counter | `result` | ResolveForAgent 次数 |
| `yaa_skill_resolved_count` | Histogram | — | 每次投影 Skill 数量 |

Skill name、AgentID、路径、错误文本和 option key 不作为 label。需要定位单个 Agent/Skill 时使用日志或两个只读 Remote GET。

## 3. Health 与 Remote

Skill health 只有：

- `ready`：Manager 已 all-or-nothing 构造完成；
- `not_ready`：尚未加载或 Load 失败。

Health check 只读取状态，不重新扫描文件。v1 不把 Skill 状态发布到 event bus，不定义 Skill SSE、ConversationFrame 或管理事件；Remote 只提供 `GET /api/v1/skills` 和 `GET /api/v1/skills/:name`。

## 4. 最小测试

1. 日志和指标中没有 Prompt、options value、绝对路径或 Secret。
2. 成功/失败 Load 各只增加一个对应 counter。
3. metric labels 不包含 Skill/Agent ID。
4. 文件在运行期变化不产生 reload 日志或事件。

---

*最后更新: 2026-07-22*
