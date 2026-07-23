# Skill API

> [返回索引](INDEX.md) · canonical 模型见 [Skill Manager](../skill/manager.md)

Skill API 只读取启动时冻结的 Manager snapshot，不重新扫描文件。列表和详情使用显式 DTO，不直接序列化包含绝对路径与 options 的 `skill.Entry`。

## DTO

```go
type SkillSummary struct {
    Name        string       `json:"name"`
    Description string       `json:"description"`
    Version     string       `json:"version"`
    Status      skill.Status `json:"status"` // loaded | disabled
}

type SkillView struct {
    Name        string       `json:"name"`
    Description string       `json:"description"`
    Version     string       `json:"version"`
    Author      string       `json:"author"`
    Tools       []string     `json:"tools"`  // Skill.Tools：已注册 Tool 依赖
    Skills      []string     `json:"skills"` // Skill.Skills：Prompt 依赖
    Status      skill.Status `json:"status"`
    LoadedAt    time.Time    `json:"loaded_at"`
    Prompt      string       `json:"prompt"`
}
```

所有 slice 按 name 升序并输出空数组而不是 null；`Tools` 使用 Skill 声明的 canonical Tool name，不投影 Provider alias。可选 string 输出空串。DTO 省略 `Entry.Path` 和全部 frontmatter/root/Agent options，避免绝对路径与开放配置 map 泄露。Status 的 wire 值由 string 类型本身固定，不把 Go enum integer 输出到 JSON。

## GET /api/v1/skills

返回全部 loaded/disabled Skill 的 `SkillSummary`，按 name 升序，不分页：

```json
{
  "items": [
    {
      "name": "weather",
      "description": "Get current weather and forecasts",
      "version": "1.0.0",
      "status": "loaded"
    }
  ]
}
```

## GET /api/v1/skills/:name

返回 `SkillView`：

```json
{
  "name": "weather",
  "description": "Get current weather and forecasts",
  "version": "1.0.0",
  "author": "example",
  "tools": ["http"],
  "skills": [],
  "status": "loaded",
  "loaded_at": "2026-07-22T01:00:00Z",
  "prompt": "# Weather\n\nUse the HTTP tool..."
}
```

未知 name 返回 404 / `40401`；Manager 未 Ready 返回 503 / `50301`。Disabled Skill 是已知资源，仍返回 200 和 `status:"disabled"`。

## 调用边界

v1 没有 Skill invoke、install、uninstall、enable、disable 或 reload API。Skill Prompt 只在已知 Agent/Session turn 中按启动 Config 注入，并参与普通 Tool loop；协议见 [调用契约](../skill/invocation.md)。

---

*最后更新: 2026-07-22*
