# Plugin 实现检查清单

> 文档路径: `docs/plugin/checklist.md`
> 上级: `docs/plugin/README.md` §11

---

## 11. 实现检查清单

### Plugin 接口

- [ ] `Plugin` 接口定义（Name, Version, Init, Shutdown, Type）
- [ ] `PluginMeta` 结构体定义（Name, Version, Author, Description, Type, Dependencies）
- [ ] `PluginType` 枚举（Tool, Provider, Skill, Hook, Middleware）
- [ ] `PluginEntry` 结构体定义（Plugin, Status, Source, Path, LoadedAt, Config）
- [ ] `PluginStatus` 枚举（Registered, Loaded, Disabled, Error, Uninstalled）
- [ ] `ToolPlugin` 接口定义（RegisterTools）
- [ ] `ProviderPlugin` 接口定义（RegisterProvider）
- [ ] `HookPlugin` 接口定义（RegisterHooks）
- [ ] 插件符号导出约定（`PluginInstance` 变量）

### Plugin Manager

- [ ] `Manager` 结构体定义（plugins map, configs map, logger, mu）
- [ ] `LoadAll()` — 扫描插件目录并加载所有 Plugin
- [ ] `Load()` — 加载单个 Plugin 文件
- [ ] `Unload()` — 卸载 Plugin（注销注册的资源）
- [ ] `Enable()` — 启用已禁用的 Plugin
- [ ] `Disable()` — 禁用已启用的 Plugin
- [ ] `Get()` — 查找 Plugin
- [ ] `List()` — 列出所有已注册 Plugin
- [ ] `ListByType()` — 按类型过滤 Plugin
- [ ] `Reload()` — 热更新 Plugin
- [ ] `HealthCheck()` — 插件健康检查
- [ ] `GetStats()` — 获取 Plugin 运行时统计

### Plugin Loader

- [ ] `.so` 文件扫描逻辑（遍历 pluginsDir）
- [ ] `plugin.Open()` 加载 Go 共享库
- [ ] `Lookup("PluginInstance")` 符号查找
- [ ] 插件元数据提取（Name, Version, Type）
- [ ] 插件类型校验（Type 是否合法）
- [ ] 插件名称唯一性检查
- [ ] 加载失败处理（记录错误，跳过，不中断其他 Plugin）
- [ ] 临时文件清理（下载安装场景）

### 加载/卸载流程

- [ ] 插件目录存在性检查
- [ ] 插件文件权限校验
- [ ] `Init()` 调用（传入 Runtime 上下文）
- [ ] Tool 类型插件 → 注册 Tool 到 Tool Manager
- [ ] Provider 类型插件 → 注册 Provider 到 Provider Manager
- [ ] Hook 类型插件 → 注册 Hook 到对应 Hook 点
- [ ] Middleware 类型插件 → 注册中间件到处理链
- [ ] 卸载时资源清理（注销 Tool / Provider / Hook）
- [ ] `Shutdown()` 调用（优雅停止）
- [ ] 卸载失败回滚（恢复已注销的资源）

### 依赖解析

- [ ] `dependencies` 字段解析（插件间依赖）
- [ ] 依赖缺失时的错误处理
- [ ] 循环依赖检测
- [ ] 加载顺序拓扑排序
- [ ] Go 版本兼容性检查（编译器版本与 Runtime 版本匹配）
- [ ] 依赖的第三方库版本检查

### 隔离与安全

- [ ] 插件 panic 恢复（recover + 记录日志）
- [ ] 插件超时控制（Init / Shutdown 超时）
- [ ] 插件沙箱限制（可选，限制可访问的系统资源）
- [ ] 插件签名验证（可选，验证 `.so` 文件来源）
- [ ] 插件白名单/黑名单配置
- [ ] 插件崩溃自动隔离（连续失败后禁用）

### 配置

- [ ] 全局 Plugin 配置（`plugins.*` in config.yaml）
- [ ] per_plugin 覆盖（`plugins.per_plugin.<name>.*`）
- [ ] 插件目录配置（`plugins.dir`）
- [ ] auto_load 配置处理
- [ ] 插件级别 Options 透传（`plugins.per_plugin.<name>.options`）
- [ ] 配置合并逻辑（全局 → per_plugin → 插件内部默认值）

### 集成

- [ ] 与 Tool Manager 集成（Tool 类型插件注册 Tool）
- [ ] 与 Provider Manager 集成（Provider 类型插件注册 Provider）
- [ ] 与 Skill Manager 集成（Skill 专属 Tool 的 Go plugin 加载）
- [ ] 与 Hook 系统集成（Hook 类型插件注册生命周期钩子）
- [ ] Remote API: `GET /api/v1/plugins` — 列出所有 Plugin
- [ ] Remote API: `GET /api/v1/plugins/:name` — 获取 Plugin 详情
- [ ] Remote API: `POST /api/v1/plugins/:name/enable` — 启用 Plugin
- [ ] Remote API: `POST /api/v1/plugins/:name/disable` — 禁用 Plugin
- [ ] Remote API: `POST /api/v1/plugins/:name/reload` — 热更新 Plugin
- [ ] SSE 事件: `plugin.loaded` / `plugin.unloaded`
- [ ] SSE 事件: `plugin.enabled` / `plugin.disabled`
- [ ] SSE 事件: `plugin.reloaded` / `plugin.error`
- [ ] 指标: `plugin_total` (Gauge)
- [ ] 指标: `plugin_load_duration` (Histogram)
- [ ] 指标: `plugin_error_total` (Counter)
- [ ] 内置 Tool: `plugin_list` / `plugin_enable` / `plugin_disable`
- [ ] fsnotify 文件监听（可选，热更新）
