# 配置系统实现检查清单

> 文档路径: `docs/config/checklist.md`
> 上级: `docs/config/README.md`

---

### 配置加载

- [ ] `Config` 顶层结构体定义（Runtime, Agents, Providers, Skills, Tools, Memory, MCP, Logging, Metrics）
- [ ] `Loader` 结构体定义（paths, format, envResolver, cliFlags, logger）
- [ ] `Load()` 统一入口方法
- [ ] 配置文件路径发现（`--config` 显式指定 → 默认搜索路径）
- [ ] 默认搜索路径（`./yaa.yaml` → `./config/yaa.yaml` → `~/.yaa/yaa.yaml` → `/etc/yaa/yaa.yaml`）
- [ ] 多文件合并加载（主配置 + `conf.d/` 目录片段）
- [ ] 加载管线顺序：默认值 → 配置文件 → 环境变量展开 → 命令行参数覆盖
- [ ] 配置文件不存在时使用纯默认值启动（warn 日志，不 fatal）
- [ ] 加载失败时明确报错（文件解析错误 vs 校验错误）

### 环境变量

- [ ] `${VAR_NAME}` 语法解析（配置文件值中的占位符）
- [ ] `${VAR_NAME:-default}` 默认值语法支持
- [ ] 环境变量缺失时的行为（空值 vs 报错，取决于字段 required 标记）
- [ ] `YAA_` 前缀环境变量自动映射（`YAA_RUNTIME_API_HTTP_ADDR` → `runtime.api.http.addr`）
- [ ] 敏感字段强制环境变量来源（API Key、Token 等不在配置文件中明文存储）
- [ ] 环境变量展开在配置文件解析后、校验前执行
- [ ] 展开结果类型转换（字符串 → int / bool / duration）

### 校验

- [ ] `Validator` 结构体定义（rules, logger）
- [ ] `Validate()` 统一校验入口
- [ ] 必填字段校验（缺失时 fatal）
- [ ] 字段类型校验（int / bool / string / duration / url / filepath）
- [ ] 枚举值校验（如 `log.level` 仅允许 debug/info/warn/error）
- [ ] 范围校验（如 `min/max`、`port` 1-65535）
- [ ] 依赖关系校验（如启用 TLS 时 `tls.cert` 和 `tls.key` 必填）
- [ ] 互斥配置校验（如 `provider` 和 `providers` 不可同时使用）
- [ ] 语义校验（如 `max_agents` > 0、`timeout` > 0）
- [ ] 校验错误聚合返回（收集所有错误，一次性报告）
- [ ] 校验错误消息包含字段路径（如 `runtime.api.http.port: invalid value`）

### 热更新

- [ ] `Watcher` 结构体定义（fsnotify Watcher, callbacks, debounceTimer, logger）
- [ ] `Watch()` 方法 — 启动文件监听
- [ ] `Stop()` 方法 — 停止监听
- [ ] `RegisterCallback()` — 注册配置变更回调
- [ ] fsnotify 文件变更事件监听（Write / Create / Rename）
- [ ] 防抖机制（debounce interval，避免频繁触发）
- [ ] 变更后重新加载 → 校验 → 原子替换
- [ ] 校验失败时保留旧配置（拒绝变更，记录 error 日志）
- [ ] 原子替换（`atomic.Value` 存储 Effective Config）
- [ ] 变更传播通知（回调通知各子系统重新读取配置）
- [ ] 热更新粒度控制（部分字段不可热更新，需重启）
- [ ] 热更新事件日志 + SSE 事件（`config.reloaded`）
- [ ] Remote API 触发热更新（`POST /api/v1/config/reload`）

### 多格式

- [ ] YAML 解析（主格式，`gopkg.in/yaml.v3`）
- [ ] TOML 解析（`github.com/BurntSushi/toml`）
- [ ] JSON 解析（标准库 `encoding/json`）
- [ ] 格式自动检测（文件扩展名：`.yaml`/`.yml`/`.toml`/`.json`）
- [ ] 无扩展名时的内容嗅探（尝试 YAML → JSON → TOML）
- [ ] 统一中间表示（`map[string]any`）后解码到 `Config` 结构体
- [ ] 格式转换工具（`yaa config convert --to yaml`，开发者工具）
- [ ] 格式间语义等价性测试

### 迁移

- [ ] 配置版本字段（`config_version: 1`）
- [ ] 版本检测与迁移触发（加载时比较文件版本与当前版本）
- [ ] 迁移函数注册表（`map[uint]MigrationFunc`）
- [ ] 逐版本迁移执行（v1 → v2 → ... → vN，不跳级）
- [ ] 迁移前自动备份原配置文件（`.bak` 后缀）
- [ ] 迁移后写回新版本号
- [ ] 迁移失败回滚（恢复备份文件）
- [ ] 废弃字段警告（字段已废弃但仍可读，warn 日志提示替代方案）
- [ ] 移除字段报错（字段已移除，fatal 并提示迁移）
- [ ] 迁移日志记录（from version → to version，变更明细）

### 默认值

- [ ] `Default()` 函数 — 返回内置默认配置
- [ ] 默认值注入时机（加载管线第 1 步，作为合并基础）
- [ ] 零值与默认值区分（`nil` / `""` / `0` 视为未设置，注入默认值）
- [ ] 嵌套结构体递归注入默认值
- [ ] 切片/Map 字段默认值初始化（避免 nil panic）
- [ ] Duration 类型默认值（如 `timeout: 30s`）
- [ ] 默认值文档同步（reference.md 中标注 `默认: xxx`）
- [ ] `yaa config defaults` 命令 — 输出完整默认配置（开发者工具）

### 错误处理

- [ ] `ErrConfigFileNotFound`
- [ ] `ErrConfigParseFailed`
- [ ] `ErrConfigValidationFailed`
- [ ] `ErrConfigEnvVarMissing`
- [ ] `ErrConfigMigrationFailed`
- [ ] `ErrConfigHotReloadFailed`
- [ ] `ErrConfigFormatUnsupported`
- [ ] 错误消息包含上下文（文件路径 + 字段路径 + 原因）

---

*最后更新: 2025-07-17*
