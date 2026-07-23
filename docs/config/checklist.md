# 配置系统实现检查清单

> 文档路径: `docs/config/checklist.md`
> 上级: `docs/config/README.md`

---

## 配置加载

- [ ] `Config` 顶层结构体定义（Runtime, Agents, Providers, MCP, Tools, Skills, Memory, Session, Context, Planner, Plugins, Log）
- [ ] `Loader` 结构体定义（paths, format, envResolver, cliFlags, logger）
- [ ] `Load()` 统一入口方法
- [ ] 配置文件路径发现（`--config` 显式指定 → 默认搜索路径）
- [ ] 默认搜索路径（`./yaa.yaml` → `~/.yaa/yaa.yaml` → `/etc/yaa/yaa.yaml`）
- [ ] 单配置文件语义（命中第一个文件后停止查找，不叠加 `conf.d`）
- [ ] 加载管线顺序：`Default()` → 配置文件迁移/环境变量展开 → `ApplyElementDefaults(raw)` → typed decode → 命令行参数覆盖
- [ ] 配置文件不存在时使用纯默认值启动（warn 日志，不 fatal）
- [ ] 加载失败时明确报错（文件解析错误 vs 校验错误）

## 环境变量

- [ ] `${VAR_NAME}` 语法解析（配置文件值中的占位符）
- [ ] `${VAR_NAME:-default}` 默认值语法支持
- [ ] 环境变量缺失且无 `:-default` 时统一返回 `ErrConfigEnvVarMissing`
- [ ] 敏感字段强制环境变量来源（API Key、Token 等不在配置文件中明文存储）
- [ ] 环境变量展开在配置文件解析后、校验前执行
- [ ] 展开结果类型转换（字符串 → int / bool / duration）

## 校验

- [ ] 无状态 `Validator` 结构体定义
- [ ] `Validate()` 统一校验入口
- [ ] 必填字段校验（缺失时 fatal）
- [ ] 字段类型校验（int / bool / string / duration / url / filepath）
- [ ] 枚举值校验（如 `log.level` 仅允许 debug/info/warn/error）
- [ ] 范围校验（如 `min/max`、`port` 1-65535）
- [ ] 依赖关系校验（如非回环监听必须启用认证）
- [ ] 语义校验（如 `max_agents` > 0、`timeout` > 0）
- [ ] 校验错误聚合返回（收集所有错误，一次性报告）
- [ ] 校验错误消息包含字段路径（如 `runtime.api.http.port: invalid value`）
- [ ] Agent `model` 非空；内置 Provider 的 `base_url` 解码后是非空绝对 HTTP(S) URL

## 热更新

- [ ] `Watcher.Run(ctx)` 监听目录并统一拥有 fsnotify/timer 生命周期
- [ ] `config.Load` 只读取一次初始文件；`ReloadManager` 保存同一 snapshot，并在 catalog 建立后由 `Activate` 完成 binding 校验
- [ ] `Reload() (ReloadResult, error)` 作为 watcher/Tool 唯一入口
- [ ] fsnotify 文件变更事件监听（Write / Create / Rename）
- [ ] 防抖机制（debounce interval，避免频繁触发）
- [ ] 变更后重新加载 → 校验 → 原子替换
- [ ] 校验失败时保留旧配置（拒绝变更，记录 error 日志）
- [ ] 原子替换（`atomic.Value` 存储 Effective Config）
- [ ] 组件在每次操作开始时从 `Current()` 复制所需 hot-reload 字段
- [ ] 热更新粒度控制（部分字段不可热更新，需重启）
- [ ] reload 结果/失败只记录脱敏结构化日志，不增加无路由支撑的全局 SSE
- [ ] `config_reload` Tool 与文件 watcher 共用唯一 Reload 流程；Remote API 不注册 reload 路由

## 脱敏视图

- [ ] `config.RedactedView(*Config) (any, error)` 是 Remote 与 Config Tool 的唯一实现
- [ ] 已知 Secret、MCP headers/env 和开放 Map 递归 fail-closed；不修改输入 snapshot
- [ ] `config_query` 在完整脱敏后再解析 path，任何脱敏失败都不得返回原值
- [ ] Remote 与 Tool 对同一 snapshot 的完整视图深度相等

## 多格式

- [ ] YAML 解析（主格式，`gopkg.in/yaml.v3`）
- [ ] TOML 解析（`github.com/BurntSushi/toml`）
- [ ] JSON 解析（标准库 `encoding/json`）
- [ ] 格式自动检测（文件扩展名：`.yaml`/`.yml`/`.toml`/`.json`）
- [ ] 无扩展名时按 YAML 解析；需要其他格式时必须使用扩展名
- [ ] 统一中间表示（`map[string]any`）后解码到 `Config` 结构体
- [ ] 格式转换工具（`yaa config convert --to yaml`，开发者工具）
- [ ] 格式间语义等价性测试

## 迁移

- [ ] 配置版本字段（`config_version: "1.0"`）
- [ ] 版本检测与迁移触发（加载时比较文件版本与当前版本）
- [ ] 迁移函数注册表（`[]Migration` 显式版本边，拒绝重复起点与隐式路径）
- [ ] 按显式迁移边逐步执行，不推测 `nextVersion` 或跳过缺失路径
- [ ] 仅显式迁移 CLI 在写回前备份原配置文件（`.bak` 后缀）；启动加载不写盘
- [ ] 显式迁移 CLI 写回成功后更新版本号；启动加载只更新内存中的 raw Map
- [ ] CLI 写回失败时保留原文件；启动加载迁移失败不启动 Runtime
- [ ] 废弃字段警告（字段已废弃但仍可读，warn 日志提示替代方案）
- [ ] 移除字段报错（字段已移除，fatal 并提示迁移）
- [ ] 迁移日志记录（from version → to version，变更明细）

## 默认值

- [ ] `Default()` 函数 — 返回内置默认配置
- [ ] 默认值注入时机（先以 `Default()` 建立根基底，再在 typed decode 前应用元素默认）
- [ ] 通过 presence-aware Map 区分缺失与显式 `false`/`0`/`[]`，显式值不得被默认值覆盖
- [ ] `Default()` 递归初始化根结构；`ApplyElementDefaults(raw)` 在 typed decode 前为每个新切片/动态 Map 元素逐项注入缺失默认值
- [ ] 最小元素用例覆盖 Agent `max_tokens=4096`、Provider timeout/retry/type URL、Token `roles=[viewer]`、MCP `transport/auto_start` 和 Skill `enabled=true`
- [ ] 元素默认注入不覆盖显式 `false`、`0`、`""`、`[]`、`{}` 或 `null`
- [ ] 切片/Map 字段默认值初始化（避免 nil panic）
- [ ] Duration 类型默认值（如 `timeout: 30s`）
- [ ] 默认值文档同步（reference.md 中标注 `默认: xxx`）
- [ ] `yaa config defaults` 命令 — 输出完整默认配置（开发者工具）

## 错误处理

- [ ] `ErrConfigFileNotFound`
- [ ] `ErrConfigParseFailed`
- [ ] `ErrConfigValidationFailed`
- [ ] `ErrConfigEnvVarMissing`
- [ ] `ErrConfigMigrationFailed`
- [ ] `ErrConfigHotReloadFailed`
- [ ] `ErrConfigNotActive`
- [ ] `ErrConfigFormatUnsupported`
- [ ] 错误消息包含上下文（文件路径 + 字段路径 + 原因）

---

*最后更新: 2025-07-17*
