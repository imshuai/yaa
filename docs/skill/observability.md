# Skill 可观测性（补充）

> 文档路径: `docs/skill/observability.md`
> 上级: `docs/skill/README.md` §8

---

本文档补充 Skill 可观测性的实现细节。基础日志、指标和事件定义见 [errors.md](errors.md) §8。

---

## 8.5 Skill 追踪

### 8.5.1 调用链追踪

当 Agent 使用 Skill 时，整个调用链应可追踪：

```text
Trace: session-abc123, turn-5
  │
  ├─ span: skill.trigger
  │   ├─ skill: "web-scraper"
  │   ├─ agent: "web-agent"
  │   ├─ duration: 2ms
  │   │
  │   ├─ span: skill.load_body
  │   │   └─ tokens: 3200
  │   │
  │   ├─ span: skill.ensure_tools
  │   │   └─ tools: ["http", "file_write", "shell"]
  │   │
  │   └─ span: skill.inject_prompt
  │       └─ active_skills: 1
  │
  ├─ span: tool.execute (http)
  │   └─ ...
  │
  ├─ span: tool.execute (shell)
  │   └─ ...
  │
  └─ span: skill.complete
      └─ total_duration: 45s
```

### 8.5.2 Skill 使用统计

Skill Manager 维护运行时统计：

```go
// SkillStats 是 Skill 的运行时统计。
type SkillStats struct {
    TriggerCount    int           // 触发次数
    LastTriggered   time.Time     // 最后触发时间
    TotalDuration   time.Duration // 总执行时长
    AvgDuration     time.Duration // 平均执行时长
    ErrorCount      int           // 错误次数
    SuccessRate     float64       // 成功率
    ActiveAgents    int           // 当前使用的 Agent 数
}

// GetStats 获取 Skill 统计信息。
func (m *Manager) GetStats(name string) (*SkillStats, error)

// GetAllStats 获取所有 Skill 的统计。
func (m *Manager) GetAllStats() map[string]*SkillStats
```

### 8.5.3 Context 占用监控

```go
// ContextUsage 报告 Skill 在 Context 中的占用情况。
type ContextUsage struct {
    SkillName       string
    MetadataTokens  int   // Level 1 占用
    BodyTokens      int   // Level 2 占用
    ResourceTokens  int   // Level 3 占用（按需加载的参考文档等）
    TotalTokens     int   // 合计
    ContextPercent  float64 // 占 Context 窗口的百分比
}
```

当 Skill 占用超过阈值时发出告警：

```yaml
skills:
  observability:
    context_usage_warn: 20    # 单个 Skill 占 Context 20% 时告警
    total_usage_warn: 50      # 所有 Skill 合计占 50% 时告警
```

---

## 8.6 健康检查

```go
// HealthCheck 检查 Skill 系统健康状态。
func (m *Manager) HealthCheck() SkillHealthReport {
    report := SkillHealthReport{
        TotalSkills:    len(m.skills),
        Loaded:         0,
        Disabled:       0,
        Error:          0,
        Details:        make([]SkillHealth, 0),
    }

    for name, entry := range m.skills {
        health := SkillHealth{
            Name:    name,
            Status:  entry.Status,
            Version: entry.Skill.Version,
        }

        switch entry.Status {
        case SkillStatusLoaded:
            report.Loaded++
            // 检查依赖
            if err := m.EnsureTools(name); err != nil {
                health.Warnings = append(health.Warnings,
                    fmt.Sprintf("tool dependency issue: %v", err))
            }
        case SkillStatusDisabled:
            report.Disabled++
        case SkillStatusError:
            report.Error++
        }

        report.Details = append(report.Details, health)
    }

    return report
}
```

**健康检查结果示例：**

```json
{
  "total_skills": 5,
  "loaded": 3,
  "disabled": 1,
  "error": 1,
  "details": [
    {"name": "web-scraper", "status": "loaded", "version": "1.2.0"},
    {"name": "data-analyzer", "status": "loaded", "version": "0.8.0"},
    {"name": "pdf-tools", "status": "loaded", "version": "1.0.0"},
    {"name": "legacy-connector", "status": "disabled", "version": "0.3.0"},
    {"name": "broken-skill", "status": "error", "version": "",
     "warnings": ["SKILL.md parse error: invalid frontmatter"]}
  ]
}
```
