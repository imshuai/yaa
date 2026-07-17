# Context 配置参考

> 文档路径: `docs/context/config-ref.md`
> 上级: `docs/context/README.md` §6

---

## 6. 配置参考

### 6.1 全局配置

```yaml
# config.yaml

context:
  # ── 窗口与预算 ──
  max_tokens: 128000                 # Context 窗口上限（与模型对齐）
  reserved_tokens: 4096              # 为回复保留的 Token 数
  system_prompt_tokens: 2048         # 系统提示词 Token 预算
  tool_output_tokens: 16384          # Tool 输出 Token 预算
  skill_body_tokens: 8000            # Skill Body Token 预算
  history_tokens: 0                   # 历史消息预算（0 = 自动计算剩余）

  # ── 构建策略 ──
  strategy: "priority"                # 构建策略: priority | fifo | weighted
  overflow_strategy: "compress"      # 超出策略: compress | truncate | reject

  # ── 压缩配置 ──
  compression:
    enabled: true                     # 启用自动压缩
    threshold: 0.85                   # 触发阈值（占 max_tokens 的比例）
    target_ratio: 0.60                # 压缩后目标占比
    min_messages_before_compress: 6   # 最少消息数才触发压缩
    preserve_recent: 3                # 压缩时保留最近 N 条消息不压缩
    method: "summarize"               # 压缩方式: summarize | truncate | rolling

  # ── 分段配置 ──
  segments:
    system:
      priority: 100                   # 优先级（越高越不容易被裁剪）
      compressible: false             # 系统提示词不压缩
    tools:
      priority: 90
      compressible: false
    skills:
      priority: 80
      compressible: true
      max_tokens: 8000
    history:
      priority: 50
      compressible: true
    tool_output:
      priority: 70
      compressible: true
      max_per_message: 4096           # 单条 Tool 输出上限
```

### 6.2 Agent 级别配置

```yaml
agents:
  - id: "research-agent"
    context:
      max_tokens: 200000              # 覆盖全局窗口上限
      strategy: "weighted"            # 覆盖构建策略
      compression:
        threshold: 0.80               # 更早触发压缩
        preserve_recent: 5            # 保留更多近期消息

  - id: "light-agent"
    context:
      max_tokens: 8192
      compression:
        enabled: false                # 小窗口不压缩
```

### 6.3 策略说明

| 策略 | 说明 | 适用场景 |
|------|------|---------|
| `priority` | 按 segment priority 从高到低填充，低优先级先被裁剪 | 通用场景（默认） |
| `fifo` | 严格先进先出，超出时裁剪最旧消息 | 简单对话 |
| `weighted` | 按权重比例分配各段 Token 预算 | 多 Skill / 多 Tool 场景 |

### 6.4 Token 预算分配

Token 预算按以下顺序分配：

```text
max_tokens
  ├─ reserved_tokens          (固定扣除)
  ├─ system_prompt_tokens      (固定扣除)
  ├─ tool_output_tokens         (固定扣除)
  ├─ skill_body_tokens         (固定扣除)
  └─ history_tokens            (剩余 = max - 上述总和)
```

**预算计算实现：**

```go
// CalcBudget 计算 Context 各段 Token 预算。
func CalcBudget(cfg ContextConfig) TokenBudget {
    budget := TokenBudget{
        System:  cfg.SystemPromptTokens,
        Tools:   cfg.ToolOutputTokens,
        Skills:  cfg.SkillBodyTokens,
        Reserve: cfg.ReservedTokens,
    }

    used := budget.System + budget.Tools + budget.Skills + budget.Reserve
    remaining := cfg.MaxTokens - used

    if cfg.HistoryTokens > 0 {
        budget.History = min(cfg.HistoryTokens, remaining)
    } else {
        budget.History = remaining // 自动分配剩余
    }

    if budget.History < 0 {
        budget.History = 0 // 预算超限，触发降级
    }

    return budget
}
```

### 6.5 压缩阈值行为

| 触发条件 | 行为 |
|---------|------|
| 总 Token < `threshold × max_tokens` | 正常构建，不压缩 |
| 总 Token ≥ `threshold × max_tokens` | 触发压缩，目标降至 `target_ratio × max_tokens` |
| 消息数 < `min_messages_before_compress` | 跳过压缩，改用 `overflow_strategy` |
| 压缩后仍超限 | 执行 `overflow_strategy`（truncate / reject） |

### 6.6 配置项汇总

| 配置项 | 级别 | 默认值 | 说明 |
|--------|------|--------|------|
| `max_tokens` | 全局/Agent | 128000 | Context 窗口上限 |
| `reserved_tokens` | 全局 | 4096 | 回复保留 Token |
| `strategy` | 全局/Agent | priority | 构建策略 |
| `overflow_strategy` | 全局 | compress | 超出策略 |
| `compression.enabled` | 全局 | true | 启用压缩 |
| `compression.threshold` | 全局 | 0.85 | 压缩触发阈值 |
| `compression.target_ratio` | 全局 | 0.60 | 压缩目标占比 |
| `compression.preserve_recent` | 全局 | 3 | 保留最近消息数 |
| `compression.method` | 全局 | summarize | 压缩方式 |
| `segments.*.priority` | 全局 | - | 段优先级 |
| `segments.*.compressible` | 全局 | true | 是否可压缩 |
