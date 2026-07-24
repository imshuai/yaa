# 实施进度

## 当前阶段

Phase 1：核心骨架。

## 已完成

- Phase 0 文档基线已完成。
- 配置迁移检查清单已与权威迁移契约对齐。
- Go 1.20 模块、最小入口、Makefile 和 CI 已建立。
- 配置版本解析已实现并覆盖严格格式、比较和错误边界。
- 显式配置迁移图已实现，拒绝降级、重复起点和缺失路径。
- 配置环境变量已支持单次展开、默认值、递归 Map/Slice 和路径错误。
- 配置格式检测与 YAML/JSON/TOML raw Map 解析已实现。
- 完整配置 DTO 与序列化标签已按各模块权威文档实现。
- 已明确 `tools.builtin` 的 14 个 v1 配置键及 `file` 共享配置组语义。
- 已补充 File Tool `timeout: 0` 继承全局超时的 canonical 说明。
- 根配置及各子系统 canonical 默认值已实现，所有容器按调用独立初始化。
- 配置文件中的数组和动态 Map 元素默认值已实现，并统一规范化 TOML 的对象数组。
- Presence-aware typed decode 已实现：保留缺失字段、整体替换切片、按 key 合并 Map，并严格处理 null、duration、标量转换、未知字段和完整错误路径。
- Session、Memory、Context 与 Planner 的 presence-aware policy resolver 已实现，显式 `false`/`0` 可覆盖上层值。
- Validator 契约已补齐所有静态 helper、稳定错误、NaN、inactive descriptor、Skill option 编码及静态/binding 两阶段边界。
- 基础配置 Validator 已实现：聚合结构化错误、校验根与 Agent effective policy，并保持配置只读。

- 配置文件路径发现已实现：按 `--config` → `YAA_CONFIG_PATH` → 当前目录/`~/.yaa`/`/etc/yaa` 顺序、目录内 `.yaml→.yml→.toml→.json`，只接受普通文件、返回词法绝对路径，权限/I/O 错误保留 cause，全部未命中返回空串；显式或环境路径无效返回 `ErrConfigFileNotFound`。
- 统一配置加载器 `config.Load` 已实现：`Default()` → 解析 → `migrateRaw`（版本检测+迁移）→ 环境变量展开 → `ApplyElementDefaults` → `DecodeInto` → `applyFlags` → `Validate`，默认路径全未命中时用纯默认值启动并输出 warning。
- 命令行参数覆盖 `applyFlags`/`setByPath` 已实现：按点路径反射设标量叶字段，支持 string/bool/int/uint/float/duration，拒绝数组/动态 Map/非标量/未注册路径。
- `golang.org/x/exp/slog` 已按架构契约引入为固定版本（兼容 Go 1.20）。
- `loader_test.go` 覆盖路径发现优先级、显式路径不存在、目录非普通文件、默认配置启动、完整管线与环境变量展开、flag 覆盖与非标量/未知字段拒绝。

- 统一 REST `Envelope`、错误 writer（`writeError`，HTTP 状态+两位子码）、成功 writer、request-id middleware 与生成器已实现：复用入站 `X-Request-ID` 否则生成 `req_` 前缀有序 ID，响应回写 `X-Request-ID` 与 envelope `request_id`。
- Remote API HTTP Server 骨架已实现：`NewServer`、`Start`/`Shutdown`、`recoverMiddleware`、方法校验与未匹配路由 40401。
- `/api/v1/health` 与 `/api/v1/version` 端点已实现：health 按 `HealthProvider` 返回 200/code0/data（ready=true，degraded 也 200）或 503/code50301/data=null；version 返回 ldflags 可注入的 version/git_commit/build_time/go_version。
- 已确认 `golang.org/x/exp/slog` 的 `Logger.Error(msg, err, args...)` 与标准 `log/slog` 签名不同，后续迁移需注意。
- `api_test.go` 覆盖 envelope、X-Request-ID 复用/生成、health ready/degraded/unready、version 四字段、405/404、未启动 Shutdown。

- 最小 Runtime 生命周期已实现：`runtime.New`（拒绝 nil config）→ `Start`（启动 API、标记 Ready、记录 components.api）→ `Ready`/`Health`/`UptimeSeconds` → `Shutdown`（先原子 Not Ready 再按逆序关 API，错误用 `errors.Join`）；启动失败按逆序 `rollback`。
- Runtime 实现 `api.HealthProvider`，health 接入 Ready 状态；`APIAddr()` 暴露实际监听地址。
- `cmd/yaa` 入口已接入：`--config` flag → `config.Load` → `runtime.New` → `signal.NotifyContext(Interrupt, SIGTERM)` 等 ctx → 按 `http.write_timeout` 限期 `Shutdown`。
- Makefile `build` 已通过 ldflags 注入 `api.Version/GitCommit/BuildTime`。
- Runtime 真实二进制冒烟通过：`/api/v1/health` 200/code0/data、`/api/v1/version` 含 ldflags 值、未匹配路由 404、kill 后正常退出。
- `runtime_test.go` 覆盖 nil config、Start/Ready/Health/components、Shutdown 清除 Ready、启动前 not ready、端到端真实 HTTP /health。

- 根 Storage 抽象已实现：`storage.Storage` 接口（Get/Set/Delete/Has/Keys/Close）、错误集（NotFound/Closed/InvalidKey/InvalidTTL/InvalidPath/ValueTooLarge）、MaxValueBytes 上限、共享 `validateKey`/`validateValue`/`expiresAt` 校验。
- Memory 后端已实现：注入 Clock、60s batch 1000 清理 worker、惰性隐藏过期值、Set/Get 拷贝 bytes、Close sync.Once 幂等并等 worker、Close 后返回 ErrClosed；`memory_test.go` 13 用例覆盖拷贝/Keys 排序/TTL 隐藏与负值/多参数拒绝/超大 value/幂等 Close/cleanup。
- SQLite 后端已实现（`modernc.org/sqlite` v1.28.0，纯 Go 无 CGO）：`root_kv`+`root_storage_schema_version` 表、WAL+busy_timeout、单连接、atomic upsert 保留 created_at、Get/Has/Keys 按 expiry 过滤、前缀 `substr` 字面匹配（`%`/`_`/Unicode 不被当通配）、cleanup batch 1000、migration 与目录创建失败阻止 Ready、Close 幂等；`sqlite_test.go` 覆盖 upsert/特殊字符前缀/TTL 过滤/cleanup/超大/空路径/迁移建表/工厂。
- `storage.New` 工厂按 `type` 分发 sqlite/memory，未知类型拒绝。
- Runtime 已接入 Storage：Start 顺序 Storage→API（失败逆序 rollback），Health `components.storage` 反映后端（SQLite=ready、memory=degraded 且 status=degraded、ready=true），Shutdown 先 Not Ready 再 API 再 Storage。
- 发现文档缺口并修复：`docs/storage/alternatives.md` §4 与 `checklist.md` 要求 health 报告 `durable=false`，但 `remote-api/system.md` 的 HealthData schema 无该字段；统一为通过 `components.storage` 状态表达，不增 schema 顶层字段，避免契约漂移。
- 真实二进制冒烟：SQLite 后端 health healthy+storage ready 且 `yaa.db`+WAL 落地；memory 后端 health degraded+durable=false 日志；两者优雅退出。

- 日志系统已实现 `internal/logging`：按 `config.LogConfig`（level/format/output）创建 slog.Logger，level 映射 debug/info/warn/error、format text/json、output stderr/stdout/文件路径（append），返回 closer 用于文件句柄关闭；`SetDefault` 同时设 `slog.Default`。
- `cmd/yaa` 已接入日志：`logging.SetDefault(cfg.Log)` 替换裸 `slog.Default`，进程退出前关闭日志句柄。
- `logging_test.go` 覆盖级别过滤（error 级抑制 info）、JSON 格式可解析、text 格式 attr、文件输出与 closer、非法 level 拒绝、debug 级打印、SetDefault；7 用例全过。
- 真实二进制冒烟：json+warn+文件输出 → 日志为可解析 JSON 且 INFO 行被级别过滤、stdout 为空（日志入文件）。
- 再次确认 `golang.org/x/exp/slog` 的 `Logger.Error(msg, err, args...)` 与标准库签名不同，所有 Error 调用已按 `Error(msg, err, args...)` 处理并留记。

## 下一步

- 接日志系统（slog handler/级别控制按 config.log）并接入 Runtime；随后开始 Phase2 Provider 层（OpenAI Chat/Stream/Models）。

每个可独立验收的功能完成后单独提交并推送到 `gitea/main`。
