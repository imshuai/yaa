# Skill Registry — 来源、安装、版本管理

> 文档路径: `docs/skill/registry.md`
> 上级: `docs/skill/README.md` §5

---

## 5. Skill Registry

### 5.1 Skill 来源

Yaa! 支持四种 Skill 来源：

| 来源 | 标识 | 说明 | 示例 |
|------|------|------|------|
| **本地** | `local` | 文件系统中的 Skill 目录 | `/path/to/skills/web-scraper` |
| **Git** | `git` | Git 仓库 URL | `https://github.com/user/skill-repo.git` |
| **Registry** | `registry` | Yaa! Skill Registry（类似 npm） | `registry://web-scraper` |
| **内置** | `builtin` | Yaa! 自带的 Skill | `builtin:default` |

### 5.2 安装流程

```go
// InstallOptions 安装选项。
type InstallOptions struct {
    Source      string   // 来源: "local" | "git" | "registry"
    Name        string   // Skill 名称（registry 模式下指定）
    Version     string   // 指定版本（registry 模式，默认 "latest"）
    AutoBind    bool     // 自动绑定到当前 Agent
    Force       bool     // 覆盖已存在的同名 Skill
    Destination string   // 安装目标目录（默认 skills 目录）
}

// Install 安装 Skill。
func (m *Manager) Install(source string, opts InstallOptions) (*SkillEntry, error) {
    switch opts.Source {
    case "local":
        return m.installFromLocal(source, opts)
    case "git":
        return m.installFromGit(source, opts)
    case "registry":
        return m.installFromRegistry(source, opts)
    default:
        return nil, fmt.Errorf("unknown source type: %s", opts.Source)
    }
}
```

### 5.3 本地安装

```text
installFromLocal("/path/to/my-skill"):
  │
  ├─ 1. 验证路径存在且包含 SKILL.md
  │
  ├─ 2. 解析 SKILL.md 获取 Skill 名称
  │
  ├─ 3. 检查目标目录是否已存在同名 Skill
  │     └─ 存在且 !Force → 返回 ErrSkillAlreadyExists
  │
  ├─ 4. 复制目录到 skills 安装目录
  │     └─ cp -r /path/to/my-skill → skills/my-skill
  │
  ├─ 5. 调用 Load() 加载 Skill
  │
  ├─ 6. AutoBind 时绑定到 Agent
  │
  └─ 7. 返回 SkillEntry
```

### 5.4 Git 安装

```text
installFromGit("https://github.com/user/yaa-skill-web-scraper.git"):
  │
  ├─ 1. 解析 Git URL
  │     ├─ 提取仓库名 → web-scraper
  │     └─ 支持指定子目录: URL#subdir
  │         e.g. https://github.com/user/skills-repo.git#web-scraper
  │
  ├─ 2. 克隆到临时目录
  │     └─ git clone --depth 1 <url> /tmp/yaa-skill-xxxxx
  │     └─ 支持指定分支/tag: URL@branch
  │         e.g. https://github.com/user/repo.git@v1.2.0
  │
  ├─ 3. 定位 SKILL.md
  │     ├─ 根目录有 SKILL.md → 整个仓库就是一个 Skill
  │     └─ 指定了 #subdir → 使用子目录
  │
  ├─ 4. 验证 + 解析 Skill 名称
  │
  ├─ 5. 复制到 skills 安装目录
  │
  ├─ 6. 清理临时目录
  │
  ├─ 7. Load() + AutoBind
  │
  └─ 8. 返回 SkillEntry
```

**支持的 Git URL 格式：**

| 格式 | 说明 |
|------|------|
| `https://github.com/user/repo.git` | HTTPS 克隆 |
| `git@github.com:user/repo.git` | SSH 克隆 |
| `https://...git#subdir` | 仓库子目录 |
| `https://...git@v1.0.0` | 指定 tag/branch |
| `https://...git@main#subdir` | 组合使用 |

### 5.5 Registry 安装

Yaa! Skill Registry 是一个在线 Skill 仓库（类似 npm registry）：

```text
installFromRegistry("web-scraper", opts{Version: "1.2.0"}):
  │
  ├─ 1. 查询 Registry
  │     └─ GET https://registry.yaa.dev/skills/web-scraper/versions/1.2.0
  │     └─ 返回: 下载 URL + SHA256 + 元信息
  │
  ├─ 2. 下载 Skill 包
  │     └─ GET <download_url> → /tmp/yaa-skill-web-scraper-1.2.0.tar.gz
  │
  ├─ 3. 验证完整性
  │     └─ SHA256 校验
  │
  ├─ 4. 解压到临时目录
  │     └─ tar -xzf ...tar.gz -C /tmp/yaa-skill-xxxxx
  │
  ├─ 5. 验证 SKILL.md
  │
  ├─ 6. 复制到 skills 安装目录
  │     └─ skills/web-scraper/
  │
  ├─ 7. 记录安装信息
  │     └─ .yaa-skill.json（版本、来源、安装时间）
  │
  ├─ 8. Load() + AutoBind
  │
  └─ 9. 返回 SkillEntry
```

**Registry API（规划）：**

| 端点 | 方法 | 说明 |
|------|------|------|
| `/skills` | GET | 列出所有 Skill |
| `/skills/:name` | GET | 获取 Skill 详情 |
| `/skills/:name/versions` | GET | 列出所有版本 |
| `/skills/:name/versions/:version` | GET | 获取特定版本下载信息 |
| `/skills/:name/latest` | GET | 获取最新版本 |
| `/search?q=keyword` | GET | 搜索 Skill |

### 5.6 卸载流程

```go
// UninstallOptions 卸载选项。
type UninstallOptions struct {
    Force     bool   // 强制卸载（即使有 Agent 绑定）
    KeepFiles bool   // 保留文件，仅从注册表移除
    UnbindAll bool   // 解绑所有 Agent
}

// Uninstall 卸载 Skill。
func (m *Manager) Uninstall(name string, opts UninstallOptions) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    entry, exists := m.skills[name]
    if !exists {
        return ErrSkillNotFound
    }

    // 1. 检查 Agent 绑定
    if !opts.Force {
        bound := m.findBoundAgents(name)
        if len(bound) > 0 {
            return fmt.Errorf("skill %s is bound to agents: %v (use force=true)", name, bound)
        }
    }

    // 2. 解绑所有 Agent
    if opts.UnbindAll || opts.Force {
        m.unbindFromAllAgents(name)
    }

    // 3. 注销专属 Tool
    for _, toolName := range entry.Tools {
        m.toolMgr.Unregister(toolName)
    }

    // 4. 从注册表移除
    delete(m.skills, name)

    // 5. 删除文件（可选）
    if !opts.KeepFiles {
        if err := os.RemoveAll(entry.Path); err != nil {
            m.logger.Warn("failed to remove skill files", "name", name, "err", err)
        }
    }

    m.logger.Info("skill uninstalled", "name", name, "keep_files", opts.KeepFiles)
    return nil
}
```

### 5.7 版本管理

**安装记录文件：**

每个 Skill 安装后在目录中生成 `.yaa-skill.json`：

```json
{
  "name": "web-scraper",
  "version": "1.2.0",
  "source": "registry",
  "source_url": "https://registry.yaa.dev/skills/web-scraper/versions/1.2.0",
  "installed_at": "2026-07-16T10:30:00Z",
  "sha256": "a1b2c3d4...",
  "auto_update": false
}
```

**自动更新：**

```yaml
# 全局配置
skills:
  auto_update: false              # 默认关闭
  update_check_interval: 24h      # 检查间隔

  # 单个 Skill 覆盖
  per_skill:
    web-scraper:
      auto_update: true
      update_channel: "stable"    # "stable" | "beta" | "latest"
```

**更新流程：**

```text
auto_update 触发:
  │
  ├─ 1. 读取 .yaa-skill.json 获取来源和版本
  │
  ├─ 2. 查询 Registry/Git 获取最新版本
  │     └─ 比较版本号
  │
  ├─ 3. 有新版本 → 下载 + 验证
  │
  ├─ 4. 卸载旧版本（keep_files=false）
  │
  ├─ 5. 安装新版本
  │
  ├─ 6. Reload Skill
  │
  └─ 7. 通知绑定 Agent（通过 Remote API 事件）
```

### 5.8 Skill 包格式

Registry 分发的 Skill 包为 `.tar.gz` 格式：

```text
web-scraper-1.2.0.tar.gz
├── SKILL.md
├── config.yaml
├── scripts/
│   └── parse_html.py
├── references/
│   └── selectors.md
├── tools/
│   └── custom_parser.json
└── .yaa-skill.json          ← 打包时自动生成
```

**打包命令（开发者使用）：**

```bash
# 从 Skill 目录打包
yaa skill pack /path/to/web-scraper

# 输出: web-scraper-1.2.0.tar.gz
```

**发布到 Registry（开发者使用）：**

```bash
# 发布
yaa skill publish /path/to/web-scraper --registry https://registry.yaa.dev

# 需要 Registry 账号 token
```
