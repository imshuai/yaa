# Skill 调用流程

> 文档路径: `docs/skill/invocation.md`
> 上级: `docs/skill/README.md` §4

---

## 4. Skill 调用流程

### 4.1 触发机制

Skill 的触发完全由 **LLM 自主决定**，基于 Skill 的 `description` 字段。

```text
用户消息: "帮我抓取这个网页的表格数据: https://example.com/data"
  │
  ▼
Agent 构造 LLM 请求
  │
  ├─ System Prompt 包含所有可用 Skill 的 Metadata（Level 1）
  │   ┌──────────────────────────────────────────────┐
  │   │ ## Available Skills                            │
  │   │                                                │
  │   │ - web-scraper: Web scraping skill. Use when   │
  │   │   the user asks to extract data from web      │
  │   │   pages, scrape tables, or collect structured │
  │   │   data from URLs.                              │
  │   │                                                │
  │   │ - data-analyzer: Data analysis skill. Use when│
  │   │   the user needs to analyze, visualize, or     │
  │   │   transform data.                              │
  │   └──────────────────────────────────────────────┘
  │
  ▼
LLM 返回响应
  │
  ├─ FinishReason = "stop"
  │   └─ Content: "我来使用 web-scraper 技能帮你处理..."
  │   └─ LLM 在回复中表明要使用某个 Skill
  │
  └─ Agent 检测到 Skill 触发意图
      └─ 加载 Skill Level 2（SKILL.md body）
```

### 4.2 Skill Metadata 注入

Agent 在构造 System Prompt 时，将所有可用 Skill 的 Metadata 注入：

```go
// Agent 构建 System Prompt 时的 Skill Metadata 注入。
func (a *Agent) buildSkillMetadata() string {
    skills := a.skillMgr.ListForAgent(a.id)
    if len(skills) == 0 {
        return ""
    }

    var sb strings.Builder
    sb.WriteString("\n\n## Available Skills\n\n")
    sb.WriteString("You can use the following skills when appropriate.\n")
    sb.WriteString("Mention the skill name in your response to activate it.\n\n")

    for _, entry := range skills {
        sb.WriteString(fmt.Sprintf("- **%s**: %s\n",
            entry.Skill.Name,
            entry.Skill.Description,
        ))
    }

    return sb.String()
}
```

**注入位置：** System Prompt 末尾，Tool 列表之前。

**Token 预算：** 每个 Skill ~100 tokens，10 个 Skill ~1,000 tokens。

### 4.3 Prompt 注入流程

当 LLM 决定使用某 Skill 后，Agent 加载 Skill Body 并注入 Context：

```text
LLM 回复: "我将使用 web-scraper 技能来抓取网页数据。"
  │
  ▼
Agent 检测 Skill 触发
  │
  ├─ 1. 匹配 Skill 名称
  │     └─ 从 LLM 回复中提取 Skill 名称
  │     └─ 在已注册 Skill 列表中查找
  │     └─ 未找到 → 继续正常对话（LLM 可能在"思考"）
  │
  ├─ 2. 权限检查
  │     └─ CheckPermission(agentID, skillName)
  │     └─ 无权限 → Agent 回复"无权使用该 Skill"
  │
  ├─ 3. 加载 Skill Body（Level 2）
  │     └─ SkillManager.GetPrompt(skillName)
  │     └─ 获取 SKILL.md body 内容
  │
  ├─ 4. 确保 Tool 可用
  │     └─ SkillManager.EnsureTools(skillName)
  │     └─ 检查依赖 Tool 是否已注册
  │     └─ 注册 Skill 专属 Tool（如果有且未注册）
  │
  ├─ 5. 注入 Skill Prompt 到 Context
  │     └─ 作为 System Message 追加到 Context
  │     └─ 格式见下文
  │
  ├─ 6. 继续对话
  │     └─ Agent 在 Skill Prompt 指导下调用 Tool
  │     └─ 标准 Tool Loop（见 tool/manager.md §4）
  │
  └─ 7. Skill 结束
      └─ LLM 表明任务完成
      └─ 可选：从 Context 中移除 Skill Prompt（节省 Token）
      └─ 或保留（支持后续追问）
```

**Skill Prompt 注入格式：**

```text
<system>
You are a helpful assistant.

...原始 System Prompt...

## Available Skills
- web-scraper: Web scraping skill...
- data-analyzer: Data analysis skill...
</system>

<user>
帮我抓取这个网页的表格数据: https://example.com/data
</user>

<assistant>
我来使用 web-scraper 技能帮你处理。
</assistant>

<system>                              ← Skill Prompt 注入
## Active Skill: web-scraper

# Web Scraper

## 工作流程
1. 使用 `http` Tool 获取目标 URL 的 HTML
2. 调用 `scripts/parse_html.py` 解析 HTML
3. 提取表格数据并格式化
4. 使用 `file_write` 保存结果

## 使用示例
...
</system>
```

### 4.4 Tool 编排

Skill 的 Prompt 中包含 Tool 使用指令，但 **Tool 调用仍由 LLM 自主决策**。

Skill 不直接调用 Tool，而是通过 Prompt 引导 LLM：

```text
Skill Prompt 说:
  "1. 使用 http Tool 获取 URL 的 HTML"
  ↓
LLM 生成 Tool Call:
  { "name": "http", "arguments": {"method": "GET", "url": "https://example.com/data"} }
  ↓
Tool Manager 执行 → 返回 HTML
  ↓
LLM 根据 Skill Prompt 继续下一步:
  "2. 调用 scripts/parse_html.py 解析 HTML"
  ↓
LLM 生成 Tool Call:
  { "name": "shell", "arguments": {"command": "python3 .../scripts/parse_html.py --input ..."} }
  ↓
... 继续直到完成
```

**Skill 对 Tool 的影响：**

| 层面 | 说明 |
|------|------|
| Tool 可用性 | Skill 的 `tools` 字段声明的 Tool 会被确保可用 |
| Tool 参数引导 | Skill Prompt 中包含 Tool 使用示例和参数建议 |
| Tool 执行顺序 | Skill Prompt 中的工作流程引导 LLM 按序调用 |
| Tool 结果处理 | Skill Prompt 指导 LLM 如何处理 Tool 返回的数据 |

### 4.5 与 Agent Loop 的集成

```text
Agent Loop（完整流程）:
  │
  ├─ 1. 构建 Context
  │     ├─ System Prompt（含 Skill Metadata）
  │     ├─ 历史对话
  │     └─ 当前用户消息
  │
  ├─ 2. 调用 LLM
  │     └─ Provider.Chat(ctx, request)
  │
  ├─ 3. 检查响应
  │     ├─ FinishReason = "stop" + 无 Skill 触发 → 返回给用户
  │     ├─ FinishReason = "stop" + 有 Skill 触发 → 进入 Skill 流程
  │     └─ FinishReason = "tool_calls" → 执行 Tool（标准流程）
  │
  ├─ 4. Skill 流程
  │     ├─ a. 加载 Skill Body（Level 2）
  │     ├─ b. 确保 Tool 可用
  │     ├─ c. 注入 Skill Prompt
  │     └─ d. 回到步骤 2（下一轮 LLM 调用，此时 Context 中有 Skill Prompt）
  │
  ├─ 5. Tool 执行（在 Skill 指导下）
  │     ├─ LLM 生成 Tool Call
  │     ├─ Tool Manager 执行
  │     ├─ 结果注入 Context
  │     └─ 回到步骤 2（继续 Skill 工作流）
  │
  └─ 6. Skill 完成
        ├─ LLM 表明任务完成
        ├─ 可选移除 Skill Prompt
        └─ 返回结果给用户
```

### 4.6 多 Skill 并行

LLM 可以在一轮中触发多个 Skill：

```text
用户: "抓取这个网页的数据，然后分析一下趋势"

LLM 回复: "我将使用 web-scraper 抓取数据，然后用 data-analyzer 分析趋势。"

Agent 处理:
  ├─ 加载 web-scraper Skill Body → 注入 Context
  ├─ 加载 data-analyzer Skill Body → 注入 Context
  └─ 两个 Skill 的 Prompt 同时在 Context 中
     └─ LLM 根据 web-scraper 的指令先抓取
     └─ 然后根据 data-analyzer 的指令分析
```

**多 Skill Token 预算：**

同时激活的 Skill Body 总量不应超过 Context 窗口的 30%。

```yaml
# 配置
skills:
  max_active: 3                    # 同时激活的最大 Skill 数
  max_body_tokens: 8000            # 同时加载的 Skill Body 总 Token 上限
  overflow_strategy: "lru"         # 超出时策略: lru（淘汰最久未用）| reject（拒绝新加载）
```

### 4.7 Skill 触发检测

Agent 如何判断 LLM 是否触发了 Skill：

```go
// detectSkillTrigger 从 LLM 回复中检测 Skill 触发意图。
func (a *Agent) detectSkillTrigger(content string) []string {
    var triggered []string
    available := a.skillMgr.ListForAgent(a.id)

    for _, entry := range available {
        // 简单匹配：回复中包含 Skill 名称
        if strings.Contains(strings.ToLower(content),
            strings.ToLower(entry.Skill.Name)) {
            triggered = append(triggered, entry.Skill.Name)
        }
    }

    return triggered
}
```

**进阶匹配策略（可选）：**

| 策略 | 说明 | 准确率 |
|------|------|--------|
| 名称匹配 | 回复中包含 Skill 名称 | 中 |
| 名称 + 别名 | 支持 Skill 声明别名 | 中高 |
| LLM Structured Output | LLM 返回结构化的 Skill 选择 | 高 |
| Provider Function Call | 将 Skill 也作为 Function 暴露给 LLM | 最高 |

**Function Call 模式（推荐）：**

将 Skill 作为特殊的 Function 暴露给 LLM：

```json
{
  "type": "function",
  "function": {
    "name": "use_skill",
    "description": "Activate a skill to handle the current task",
    "parameters": {
      "type": "object",
      "properties": {
        "skill_name": {
          "type": "string",
          "enum": ["web-scraper", "data-analyzer", "data-pipeline"]
        }
      },
      "required": ["skill_name"]
    }
  }
}
```

LLM 通过标准的 Function Calling 机制触发 Skill，无需文本匹配，准确率最高。

**两种模式对比：**

| 维度 | 文本匹配 | Function Call |
|------|---------|---------------|
| 实现复杂度 | 低 | 中 |
| 准确率 | 中 | 高 |
| Token 开销 | 低（仅 Metadata） | 中（额外 Function 定义） |
| Provider 兼容性 | 全部 | 需支持 Function Calling |
| 可回退 | 是 | 可回退到文本匹配 |

Yaa! 默认使用 Function Call 模式，对不支持的 Provider 回退到文本匹配。
