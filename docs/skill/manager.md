# Skill Manager

> 上级: [Skill 系统设计](README.md)

---

## 1. 权威类型

```go
type Status string

const (
    StatusLoaded   Status = "loaded"
    StatusDisabled Status = "disabled"
)

type Skill struct {
    Name        string         `yaml:"name"`
    Description string         `yaml:"description"`
    Version     string         `yaml:"version"`
    Author      string         `yaml:"author"`
    Tools       []string       `yaml:"tools"`
    Skills      []string       `yaml:"skills"`
    Options     map[string]any `yaml:"options"`
    Prompt      string         `yaml:"-"`
}

type Entry struct {
    Skill    Skill
    Status   Status
    Path     string
    LoadedAt time.Time
}

type ResolvedSkill struct {
    Name    string
    Options map[string]any
    Prompt  string
}

type Manager struct {
    entries   map[string]Entry
    byAgent   map[string][]ResolvedSkill
    skillsDir string
}
```

Manager 在启动完成后不可变，因此 `Get`、`List` 和 `ResolveForAgent` 可并发调用而不需要运行时写锁。所有返回值都深拷贝 slice/map；调用方不能修改内部 snapshot。

## 2. 构造与 API

```go
func Load(
    skillsCfg config.SkillsConfig,
    agents []config.AgentConfig,
    tools *tool.Manager,
) (*Manager, error)

func (m *Manager) Get(name string) (Entry, error)
func (m *Manager) List() []Entry
func (m *Manager) ResolveForAgent(agentID string) ([]ResolvedSkill, error)
```

`Load` 是唯一写入入口并且 all-or-nothing。失败时不返回半成品 Manager；运行期没有 Register、Unload、Enable、Disable、Install 或 Reload。

`List` 按 name 升序，包含 loaded 和 disabled 条目。`ResolveForAgent` 返回启动时为该 Agent 冻结的拓扑序；未知 Agent 返回 `ErrSkillAgentNotFound`。

## 3. 加载流程

1. 将 `skills.dir` 相对主配置文件目录解析并 `filepath.Clean`；目录不存在或不可读时启动失败。
2. 只枚举直接子目录，按目录名升序；symlink 包和逃逸根目录的路径拒绝。
3. 每个目录只读取 `SKILL.md`，先检查 1 MiB 文件上限，再用支持 YAML 1.2 core schema 的严格 decoder 解析单个 frontmatter document。
4. 校验 name 与目录名、字段上限、SemVer、JSON-compatible options 和列表去重；Prompt body 保留原 UTF-8 文本，只规范化 CRLF 为 LF。
5. 用临时 map 检查 name 唯一、Skill 依赖存在且无环。依赖图使用稳定 DFS；循环错误包含完整 name chain。
6. 应用 `skills.per_skill`。配置中出现文件系统不存在的 name 是启动错误；disabled 条目仍解析和展示，但不能被 Agent 引用。
7. 对每个 Agent 校验其精确 Skill allowlist、递归 Skill 依赖和 Tool 依赖。Tool 必须存在、enabled，且通过 `tool.Manager.CheckPermission(agentID, name)`。
8. 合并 options，为每个 Agent 建立 `byAgent` snapshot；全部成功后一次发布 Manager。

同一 Skill 的依赖按拓扑顺序位于使用者之前；互不依赖的节点按 name 升序。重复的递归依赖只出现一次。

## 4. 文件与资源安全

- Manager 只读取 `SKILL.md`；它不执行脚本、解析模板或自动注册 Tool。
- Prompt 和 options 是不可信输入，只作为 Provider 请求的一部分；不能解释为 Runtime 命令。
- 资源路径由已有 File/Shell Tool 的 allowlist 和路径规范化规则控制，Skill 目录本身不授予额外权限。
- `Path` 只用于进程内资源解析，不进入 Remote DTO、日志或 Provider Prompt。
- Skill options 不得保存凭据；敏感 key 规则见 [配置契约](config.md)。

## 5. 最小测试

1. 扫描顺序变化不改变 List 或 Agent Prompt 顺序。
2. 重复 name、目录/name 不同、未知字段、超限、symlink 和依赖环均使 Load 失败且不返回 Manager。
3. disabled/missing Skill、missing/disallowed Tool 使引用它的 Agent 绑定失败。
4. root/Agent options 合并后为深拷贝，不因调用方修改而变化。
5. 100 个 goroutine 并发 Get/List/Resolve 只读通过 race test。
6. 运行期文件变化不改变 Manager；重启后才读取新内容。

---

*最后更新: 2026-07-22*
