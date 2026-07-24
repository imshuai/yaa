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

## 下一步

- 接入第 2 阶段的运行时生命周期（Runtime start/stop）与 `/api/v1/health` 健康检查端点。

每个可独立验收的功能完成后单独提交并推送到 `gitea/main`。
