# Skill 配置参考

> 文档路径: `docs/skill/config.md`
> 上级: `docs/skill/README.md` §6

---

## 6. 配置参考

### 6.1 全局配置

```yaml
# config.yaml

skills:
  # ── 基础配置 ──
  dir: "./skills"                     # Skill 安装目录
  auto_load: true                     # 启动时自动加载所有 Skill
  auto_update: false                  # 自动更新已安装 Skill
  update_check_interval: 24h          # 更新检查间隔

  # ── 加载控制 ──
  max_active: 3                       # 同时激活的最大 Skill 数
  max_body_tokens: 8000               # 同时加载的 Skill Body 总 Token 上限
  overflow_strategy: "lru"            # 超出策略: lru | reject

  # ── 嵌套控制 ──
  max_nesting_depth: 3                # Skill 嵌套最大深度
  allow_circular: false               # 是否允许循环依赖（不推荐）

  # ── 热更新 ──
  hot_reload: true                    # 启用热更新
  watch_files: false                  # 文件变更自动 reload（fsnotify）

  # ── Registry 配置 ──
  registry:
    url: "https://registry.yaa.dev"   # Registry 地址
    token: ""                         # 认证 token
    cache_dir: "/tmp/yaa-skill-cache" # 下载缓存目录

  # ── 单个 Skill 覆盖 ──
  per_skill:
    web-scraper:
      enabled: true
      timeout: 120s
      auto_update: true
      update_channel: "stable"
      options:
        max_pages: 100                # 覆盖 frontmatter 中的 options
        user_agent: "Yaa!/1.0"

    data-analyzer:
      enabled: false                  # 禁用
```

### 6.2 Agent 级别配置

```yaml
agents:
  - id: "web-agent"
    name: "Web Agent"
    # ── Skill 权限 ──
    skills: ["web-scraper", "data-analyzer"]
    # 或细粒度控制:
    # skills:
    #   allow: ["web-scraper", "data-analyzer"]
    #   deny: ["data-pipeline"]

    # ── Skill 级别覆盖 ──
    skills_config:
      web-scraper:
        timeout: 60s
        options:
          max_pages: 50               # Agent 级别覆盖全局配置

  - id: "full-agent"
    skills: []                         # 可用所有已加载 Skill
```

### 6.3 Skill 级别配置

Skill 自身的配置通过三种方式合并（优先级递增）：

```text
1. SKILL.md frontmatter 的 options 字段
   ↓ 被覆盖
2. config.yaml 的 skills.per_skill.<name>.options
   ↓ 被覆盖
3. Agent 配置的 skills_config.<name>.options
```

**合并示例：**

```yaml
# SKILL.md frontmatter
options:
  max_pages: 50          # 默认值
  user_agent: "Yaa!/1.0"
  timeout: 30

# config.yaml (全局)
skills:
  per_skill:
    web-scraper:
      options:
        max_pages: 100   # 覆盖默认值
        retry: 3         # 新增

# Agent 配置
agents:
  - skills_config:
      web-scraper:
        options:
          max_pages: 30   # 再次覆盖
          timeout: 60     # 覆盖

# 最终合并结果:
# max_pages: 30           ← Agent 级
# user_agent: "Yaa!/1.0"  ← frontmatter
# timeout: 60             ← Agent 级
# retry: 3                ← 全局级
```

### 6.4 Skill 配置项汇总

| 配置项 | 级别 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | 全局/Agent | true | 是否启用 |
| `timeout` | 全局/Agent | 300s | Skill 执行超时 |
| `max_retry` | 全局/Agent | 0 | 重试次数 |
| `auto_load` | frontmatter | true | 启动时自动加载 |
| `auto_update` | 全局 | false | 自动更新 |
| `update_channel` | 全局 | stable | 更新通道 |
| `options.*` | 多级 | - | Skill 特有参数 |

### 6.5 配置合并实现

```go
// mergeConfig 合并多级配置。
func mergeConfig(skill *Skill, global SkillConfig, agent map[string]any) SkillConfig {
    cfg := SkillConfig{
        Enabled:  true,
        Timeout:  300 * time.Second,
        MaxRetry: 0,
        Options:  make(map[string]any),
    }

    // 1. frontmatter options
    for k, v := range skill.Options {
        cfg.Options[k] = v
    }

    // 2. 全局 per_skill 覆盖
    if global.Enabled != nil {
        cfg.Enabled = *global.Enabled
    }
    if global.Timeout != 0 {
        cfg.Timeout = global.Timeout
    }
    for k, v := range global.Options {
        cfg.Options[k] = v
    }

    // 3. Agent 级别覆盖
    if agentEnabled, ok := agent["enabled"]; ok {
        cfg.Enabled = agentEnabled.(bool)
    }
    if agentTimeout, ok := agent["timeout"]; ok {
        cfg.Timeout = parseDuration(agentTimeout)
    }
    if agentOpts, ok := agent["options"]; ok {
        for k, v := range agentOpts.(map[string]any) {
            cfg.Options[k] = v
        }
    }

    return cfg
}
```
