# Skill 实现检查清单

> 文档路径: `docs/skill/checklist.md`
> 上级: `docs/skill/README.md` §11

---

## 11. 实现检查清单

### Skill 定义与解析

- [ ] `Skill` 结构体定义（Name, Description, Version, Tools, Skills, Options, Prompt）
- [ ] `SkillEntry` 结构体定义（Skill, Status, Source, Path, LoadedAt, Tools, Config）
- [ ] `SkillStatus` 枚举（Registered, Loaded, Disabled, Error, Uninstalled）
- [ ] `SkillConfig` 结构体定义（Enabled, Timeout, MaxRetry, Options）
- [ ] SKILL.md frontmatter 解析（YAML）
- [ ] SKILL.md body 内容提取
- [ ] frontmatter 字段验证（name 必需、description 必需）
- [ ] Skill 名称格式校验（小写中划线，如 `web-scraper`）
- [ ] Skill 名称唯一性检查

### Skill Manager

- [ ] `Manager` 结构体定义（skills map, configs map, registry, toolMgr, logger, mu）
- [ ] `LoadAll()` — 扫描目录并加载所有 Skill
- [ ] `Load()` — 加载单个 Skill 目录
- [ ] `Unload()` — 卸载 Skill（从注册表移除）
- [ ] `Enable()` — 启用已禁用的 Skill
- [ ] `Disable()` — 禁用已启用的 Skill
- [ ] `Get()` — 查找 Skill
- [ ] `List()` — 列出所有已注册 Skill
- [ ] `ListForAgent()` — 列出 Agent 可用的 Skill（权限过滤）
- [ ] `CheckPermission()` — 检查 Agent 是否有权使用 Skill
- [ ] `EnsureTools()` — 确保 Skill 依赖的 Tool 可用
- [ ] `ResolveDependencies()` — 递归解析嵌套依赖
- [ ] `GetPrompt()` — 获取 Skill Body 内容
- [ ] `Reload()` — 热更新 Skill
- [ ] `GetStats()` — 获取 Skill 运行时统计
- [ ] `HealthCheck()` — 健康检查

### 加载流程

- [ ] 目录扫描逻辑（遍历 skillsDir 子目录）
- [ ] SKILL.md 存在性检查
- [ ] frontmatter 解析与验证
- [ ] Body 内容读取
- [ ] 配置合并（frontmatter → 全局 → Agent 级别）
- [ ] Tool 绑定检查（依赖 Tool 是否已注册）
- [ ] Skill 专属 Tool 注册（tools/ 目录）
- [ ] auto_load 配置处理
- [ ] Agent 权限配置读取
- [ ] 加载失败处理（记录错误，跳过，不中断其他 Skill 加载）

### 嵌套调用

- [ ] `skills` 字段依赖解析
- [ ] 循环依赖检测
- [ ] 嵌套深度限制（max_nesting_depth，默认 3）
- [ ] 依赖 Skill 的 Metadata 自动注入
- [ ] 依赖 Skill 缺失时的错误处理

### Skill 触发与调用

- [ ] Skill Metadata 注入 System Prompt
- [ ] Skill 触发检测（文本匹配模式）
- [ ] Skill 触发检测（Function Call 模式 — `use_skill` Function）
- [ ] Provider 能力检测（是否支持 Function Call）
- [ ] 不支持 Function Call 时的回退逻辑
- [ ] Skill Body 加载（Level 2）
- [ ] Skill Prompt 注入为 System Message
- [ ] Tool 可用性检查（触发时再次确认）
- [ ] 多 Skill 并行触发处理
- [ ] max_active 限制
- [ ] max_body_tokens 限制
- [ ] overflow_strategy 处理（lru / reject）

### Skill 专属 Tool

- [ ] 声明式 Tool 解析（JSON 格式）
- [ ] 声明式 Tool 执行（Shell 模板渲染 + 执行）
- [ ] 编译式 Tool 加载（Go plugin `.so`）
- [ ] `.go` 源码编译（可选，需 Go 工具链）
- [ ] 专属 Tool 注册到 Tool Manager
- [ ] 专属 Tool 注销（Skill 卸载/热更新时）

### Registry — 安装

- [ ] `Install()` 方法（统一入口）
- [ ] 本地安装（`installFromLocal`）
- [ ] Git 安装（`installFromGit`）
  - [ ] Git URL 解析（HTTPS / SSH）
  - [ ] 子目录支持（`#subdir`）
  - [ ] 分支/Tag 支持（`@branch`）
  - [ ] 临时目录克隆 + 复制 + 清理
- [ ] Registry 安装（`installFromRegistry`）
  - [ ] Registry API 查询
  - [ ] Skill 包下载
  - [ ] SHA256 校验
  - [ ] 解压 + 安装
- [ ] 安装记录文件（`.yaa-skill.json`）生成
- [ ] AutoBind 逻辑（安装后自动绑定到 Agent）
- [ ] Force 覆盖逻辑
- [ ] 安装失败回滚

### Registry — 卸载

- [ ] `Uninstall()` 方法
- [ ] Agent 绑定检查（未 Force 时拒绝）
- [ ] 解绑所有 Agent
- [ ] 注销专属 Tool
- [ ] 从注册表移除
- [ ] 删除文件（可选 keep_files）
- [ ] Skill 包打包（`yaa skill pack`，开发者工具）

### 版本管理

- [ ] 语义化版本号解析与比较
- [ ] 自动更新检查（update_check_interval）
- [ ] 更新流程（下载 → 验证 → 卸载旧版 → 安装新版 → Reload）
- [ ] 更新失败回滚
- [ ] update_channel 配置（stable / beta / latest）

### 权限模型

- [ ] Agent 级别 Skill 白名单（`skills: [...]`）
- [ ] Agent 级别 Skill 黑名单（`skills.deny: [...]`）
- [ ] 白名单为空 = 可用所有已加载 Skill
- [ ] 黑名单优先于白名单
- [ ] 未加载的 Skill 不可用（无论权限配置）

### 配置

- [ ] 全局 Skill 配置（`skills.*` in config.yaml）
- [ ] per_skill 覆盖（`skills.per_skill.<name>.*`）
- [ ] Agent 级别覆盖（`agents[].skills_config.<name>.*`）
- [ ] 三级配置合并逻辑（frontmatter → 全局 → Agent）
- [ ] Registry 配置（url, token, cache_dir）
- [ ] 热更新配置（hot_reload, watch_files）

### 错误处理

- [ ] `ErrSkillNotFound`
- [ ] `ErrSkillAlreadyExists`
- [ ] `ErrSkillDisabled`
- [ ] `ErrSkillPermissionDenied`
- [ ] `ErrSkillDependencyMissing`
- [ ] `ErrSkillCircularDependency`
- [ ] `ErrSkillLoadFailed`
- [ ] `ErrSkillExecutionTimeout`
- [ ] `ErrSkillInstallFailed`
- [ ] 安装重试逻辑（下载失败，3 次指数退避）
- [ ] Tool 注册重试（依赖未就绪，5 次 1s 间隔）

### 可观测性

- [ ] Skill 加载日志
- [ ] Skill 触发日志
- [ ] Skill 安装/卸载日志
- [ ] Skill 热更新日志
- [ ] Skill 错误日志
- [ ] 指标: `skill_total` (Gauge)
- [ ] 指标: `skill_trigger_total` (Counter)
- [ ] 指标: `skill_active` (Gauge)
- [ ] 指标: `skill_body_tokens` (Gauge)
- [ ] 指标: `skill_install_total` / `skill_install_failed_total` (Counter)
- [ ] 指标: `skill_reload_total` (Counter)
- [ ] 指标: `skill_load_duration` (Histogram)
- [ ] SSE 事件: `skill.loaded`
- [ ] SSE 事件: `skill.triggered`
- [ ] SSE 事件: `skill.completed`
- [ ] SSE 事件: `skill.installed`
- [ ] SSE 事件: `skill.uninstalled`
- [ ] SSE 事件: `skill.enabled` / `skill.disabled`
- [ ] SSE 事件: `skill.reloaded`
- [ ] SSE 事件: `skill.error`
- [ ] Skill 使用统计（TriggerCount, AvgDuration, SuccessRate）
- [ ] Context 占用监控（context_usage_warn, total_usage_warn）
- [ ] 调用链追踪（span: skill.trigger, skill.load_body, skill.complete）

### Remote API

- [ ] `GET /api/v1/skills` — 列出所有 Skill
- [ ] `GET /api/v1/skills/:name` — 获取 Skill 详情
- [ ] `POST /api/v1/skills/install` — 安装 Skill
- [ ] `DELETE /api/v1/skills/:name` — 卸载 Skill
- [ ] `POST /api/v1/skills/:name/enable` — 启用 Skill
- [ ] `POST /api/v1/skills/:name/disable` — 禁用 Skill
- [ ] `POST /api/v1/skills/:name/reload` — 热更新 Skill
- [ ] `GET /api/v1/skills/:name/stats` — 获取 Skill 统计
- [ ] `GET /api/v1/skills/health` — Skill 系统健康检查
- [ ] SSE: `/api/v1/events/skills` — Skill 事件流

### 内置 Tool 集成

- [ ] `skill_list` Tool — 列出已安装 Skill
- [ ] `skill_install` Tool — Agent 触发安装
- [ ] `skill_uninstall` Tool — Agent 触发卸载
- [ ] `skill_enable` Tool — Agent 启用 Skill
- [ ] `skill_disable` Tool — Agent 禁用 Skill
- [ ] 上述 Tool 与 Tool Manager 安全策略集成

### 热更新

- [ ] `Reload()` 方法实现
- [ ] 旧专属 Tool 注销
- [ ] 新 SKILL.md 解析
- [ ] 新专属 Tool 注册
- [ ] 依赖重新解析
- [ ] fsnotify 文件监听（可选）
- [ ] Remote API 触发 reload
- [ ] `skill_reload` Tool 触发
