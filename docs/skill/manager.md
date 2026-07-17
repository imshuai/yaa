# Skill Manager

> 文档路径: `docs/skill/manager.md`
> 上级: `docs/skill/README.md` §3

---

## 3. Skill Manager

### 3.1 职责

Skill Manager 是 Skill 系统的中枢，负责：

1. **扫描加载** — 启动时扫描 skills 目录，加载所有 Skill
2. **注册管理** — 维护 Skill 注册表（名称 → Skill 实例）
3. **生命周期** — 安装、卸载、启用、禁用
4. **权限控制** — Agent 级别的 Skill 白名单/黑名单
5. **Tool 绑定** — 确保 Skill 依赖的 Tool 可用
6. **嵌套调用** — 处理 Skill 间依赖关系
7. **配置管理** — 合并全局 / Agent / Skill 级配置

### 3.2 核心接口

```go
// Manager 管理 Skill 的加载、注册和生命周期。
type Manager struct {
    skills   map[string]*SkillEntry    // name → Skill 条目
    configs  map[string]SkillConfig    // name → 运行时配置
    registry *Registry                 // Skill Registry
    toolMgr  *tool.Manager             // Tool Manager 引用
    logger   *slog.Logger
    mu       sync.RWMutex
}

// SkillEntry 是已加载的 Skill 实例。
type SkillEntry struct {
    Skill       *Skill          // Skill 定义
    Status      SkillStatus     // 加载状态
    Source      string          // "builtin" | "local" | "git" | "registry"
    Path        string          // Skill 目录绝对路径
    LoadedAt    time.Time       // 加载时间
    Tools       []string       // 已注册的专属 Tool 名称
    Config      SkillConfig    // 合并后的运行时配置
}

// SkillStatus 表示 Skill 的生命周期状态。
type SkillStatus int

const (
    SkillStatusRegistered  SkillStatus = iota  // 已注册，未加载
    SkillStatusLoaded                          // 已加载，可用
    SkillStatusDisabled                        // 已禁用
    SkillStatusError                           // 加载失败
    SkillStatusUninstalled                     // 已卸载
)

// Skill 是 Skill 的静态定义（从 SKILL.md 解析）。
type Skill struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    Version     string            `yaml:"version"`
    Author      string            `yaml:"author"`
    Tools       []string          `yaml:"tools"`
    Skills      []string          `yaml:"skills"`       // 依赖的其他 Skill
    Options     map[string]any    `yaml:"options"`
    Prompt      string            // SKILL.md body 内容
    Path        string            // Skill 目录路径
}

// SkillConfig 是 Skill 的运行时配置。
type SkillConfig struct {
    Enabled   bool              `yaml:"enabled"`
    Timeout   time.Duration     `yaml:"timeout"`
    MaxRetry  int               `yaml:"max_retry"`
    Options   map[string]any    `yaml:"options"`    // 覆盖 frontmatter 中的 options
}
```

### 3.3 主要方法

```go
// LoadAll 启动时扫描并加载所有 Skill。
func (m *Manager) LoadAll(skillsDir string) error

// Load 加载单个 Skill。
func (m *Manager) Load(path string) (*SkillEntry, error)

// Unload 卸载 Skill（从注册表移除，可选删除文件）。
func (m *Manager) Unload(name string, keepFiles bool) error

// Enable 启用已禁用的 Skill。
func (m *Manager) Enable(name string) error

// Disable 禁用已启用的 Skill。
func (m *Manager) Disable(name string) error

// Get 查找 Skill。
func (m *Manager) Get(name string) (*SkillEntry, error)

// List 列出所有已注册的 Skill。
func (m *Manager) List() []SkillEntry

// ListForAgent 列出 Agent 可用的 Skill（应用权限过滤）。
func (m *Manager) ListForAgent(agentID string) []SkillEntry

// CheckPermission 检查 Agent 是否有权使用某 Skill。
func (m *Manager) CheckPermission(agentID, skillName string) bool

// EnsureTools 确保 Skill 依赖的 Tool 已注册且可用。
func (m *Manager) EnsureTools(skillName string) error

// ResolveDependencies 解析 Skill 的嵌套依赖（递归）。
func (m *Manager) ResolveDependencies(skillName string) ([]string, error)

// GetPrompt 获取 Skill 的完整 Prompt（SKILL.md body）。
func (m *Manager) GetPrompt(skillName string) (string, error)

// Install 从 Registry/Git/本地安装 Skill。
func (m *Manager) Install(source string, opts InstallOptions) (*SkillEntry, error)

// Uninstall 卸载并删除 Skill。
func (m *Manager) Uninstall(name string, opts UninstallOptions) error

// Reload 重新加载 Skill（热更新）。
func (m *Manager) Reload(name string) error
```

### 3.4 加载流程

```text
启动时 LoadAll(skillsDir):
  │
  ├─ 1. 扫描 skillsDir 下的所有子目录
  │     └─ 每个子目录视为一个 Skill 包
  │     └─ 检查 SKILL.md 是否存在 → 不存在则跳过并记录警告
  │
  ├─ 2. 对每个 Skill 目录：
  │     ├─ a. 解析 SKILL.md frontmatter → Skill 定义
  │     ├─ b. 读取 SKILL.md body → Skill.Prompt
  │     ├─ c. 检查 name 唯一性 → 重复则记录错误并跳过
  │     ├─ d. 合并配置（frontmatter options ← config.yaml ← 全局配置）
  │     ├─ e. 注册到 skills map
  │     └─ f. 标记状态为 SkillStatusLoaded
  │
  ├─ 3. 处理 Skill 依赖
  │     └─ 对每个 Skill，检查 skills 字段声明的依赖
  │        ├─ 依赖存在 → OK
  │        └─ 依赖缺失 → 标记 SkillStatusError，记录错误
  │
  ├─ 4. 处理 Tool 绑定
  │     └─ 对每个已加载 Skill：
  │        ├─ 检查 tools 字段声明的依赖 Tool
  │        ├─ Tool 已注册 → OK
  │        ├─ Tool 未注册但 Skill 有专属 Tool (tools/) → 注册专属 Tool
  │        └─ Tool 未注册且无专属 → 标记警告，Skill 仍可用但 LLM 调用时会报错
  │
  ├─ 5. 应用 auto_load 配置
  │     └─ auto_load=false 的 Skill 标记为 SkillStatusDisabled
  │
  └─ 6. 应用 Agent 权限配置
        └─ 读取 agents[].skills 配置，记录白名单/黑名单
```

### 3.5 Skill 专属 Tool

Skill 可以携带自己的 Tool，放在 `tools/` 子目录中：

```text
skills/
└── pdf-tools/
    ├── SKILL.md
    └── tools/
        ├── pdf_merge.go          # Go 源码
        ├── pdf_merge.so          # 预编译插件
        └── pdf_split.py          # Python 脚本（通过 Shell Tool 执行）
```

**加载方式：**

| 文件类型 | 加载方式 |
|---------|---------|
| `*.so` | Go plugin，通过 `plugin.Open()` 加载，调用 `Register()` 注册 |
| `*.go` | 需编译为 `.so`，启动时检测并编译（需 Go 工具链） |
| `*.py` / `*.sh` | 不直接注册为 Tool，而是通过 Shell Tool 执行 |
| `*.json` | Tool 定义文件（声明式 Tool，见下文） |

**声明式 Tool（JSON）：**

```json
{
  "name": "pdf_merge",
  "description": "Merge multiple PDF files into one",
  "parameters": {
    "type": "object",
    "properties": {
      "input_files": {"type": "array", "items": {"type": "string"}},
      "output_file": {"type": "string"}
    },
    "required": ["input_files", "output_file"]
  },
  "exec": {
    "type": "shell",
    "command": "python3 {{skill_path}}/scripts/merge.py --input {{input_files}} --output {{output_file}}"
  }
}
```

声明式 Tool 由 Tool Manager 的通用执行引擎处理，无需编译。

### 3.6 嵌套调用

Skill 可以声明依赖其他 Skill：

```yaml
# SKILL.md frontmatter
name: data-pipeline
description: >
  End-to-end data pipeline: scrape → transform → analyze → report.
  Use when user needs a complete data processing workflow.
tools:
  - http
  - shell
  - file_write
skills:
  - web-scraper           # 依赖 web-scraper Skill
  - data-analyzer         # 依赖 data-analyzer Skill
```

**嵌套调用机制：**

```text
Agent 使用 data-pipeline Skill
  │
  ├─ data-pipeline 的 Prompt 被注入 Context
  │   └─ Prompt 中包含调用 web-scraper 和 data-analyzer 的指令
  │
  ├─ web-scraper 的 Metadata 已在 Context 中（Level 1）
  │   └─ Agent 可以触发 web-scraper 的 Level 2 加载
  │
  └─ data-analyzer 的 Metadata 已在 Context 中（Level 1）
      └─ Agent 可以触发 data-analyzer 的 Level 2 加载
```

**关键规则：**

1. 依赖 Skill 的 **Metadata（Level 1）** 自动注入 Context
2. 依赖 Skill 的 **Body（Level 2）** 不会被自动加载，Agent 按需触发
3. 嵌套深度限制：默认最大 3 层，防止无限递归
4. 循环依赖检测：加载时检查，发现循环则报错

```go
// ResolveDependencies 递归解析依赖，检测循环。
func (m *Manager) ResolveDependencies(skillName string) ([]string, error) {
    visited := make(map[string]bool)
    var deps []string

    var resolve func(name string) error
    resolve = func(name string) error {
        if visited[name] {
            return fmt.Errorf("circular dependency detected: %s", name)
        }
        visited[name] = true

        entry, err := m.Get(name)
        if err != nil {
            return err
        }

        for _, dep := range entry.Skill.Skills {
            if err := resolve(dep); err != nil {
                return err
            }
            deps = append(deps, dep)
        }
        return nil
    }

    if err := resolve(skillName); err != nil {
        return nil, err
    }
    return deps, nil
}
```

### 3.7 权限模型

```yaml
# Agent 级别 Skill 权限
agents:
  - id: "web-agent"
    skills: ["web-scraper", "data-analyzer"]    # 白名单

  - id: "full-agent"
    skills: []                                     # 空 = 可用所有已加载 Skill

  - id: "restricted-agent"
    skills:
      allow: ["web-scraper"]
      deny: ["data-pipeline"]                      # 黑名单优先
```

**权限规则与 Tool 权限一致：**

| 配置 | 含义 |
|------|------|
| `skills: [...]` | 白名单模式 |
| `skills: []` 或未配置 | 可用所有已加载 Skill |
| `skills.deny: [...]` | 黑名单，优先级高于 allow |
| 未加载的 Skill | 不可用，无论权限配置 |

### 3.8 热更新

Skill 支持运行时热更新（Reload），无需重启 Runtime：

```go
// Reload 重新加载 Skill。
func (m *Manager) Reload(name string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    entry, exists := m.skills[name]
    if !exists {
        return ErrSkillNotFound
    }

    // 1. 重新解析 SKILL.md
    newSkill, err := parseSkillMd(entry.Path)
    if err != nil {
        return err
    }

    // 2. 如果有专属 Tool，先注销旧的
    for _, toolName := range entry.Tools {
        m.toolMgr.Unregister(toolName)
    }

    // 3. 更新 Skill 定义
    entry.Skill = newSkill
    entry.LoadedAt = time.Now()

    // 4. 重新注册专属 Tool
    if err := m.registerSkillTools(entry); err != nil {
        entry.Status = SkillStatusError
        return err
    }

    // 5. 重新解析依赖
    if _, err := m.ResolveDependencies(name); err != nil {
        entry.Status = SkillStatusError
        return err
    }

    entry.Status = SkillStatusLoaded
    m.logger.Info("skill reloaded", "name", name, "version", newSkill.Version)
    return nil
}
```

**热更新触发方式：**

| 方式 | 说明 |
|------|------|
| Remote API | `POST /api/v1/skills/:name/reload` |
| 文件监听 | 可选启用 fsnotify，文件变更时自动 reload |
| skill_reload Tool | Agent 通过内置 Tool 触发 |
| 配置 reload | `config_reload` 时顺带 reload 所有 Skill |
