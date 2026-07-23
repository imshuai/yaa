# Plugin 设计决策

> 文档路径: `docs/plugin/decisions.md`
> 上级: [README.md](README.md)

---

## PG-001: 只采用进程外 Plugin

**决策：** 第三方 Plugin 是独立可执行进程；Runtime 不使用 Go `plugin` 包、不加载 `.so`/DLL、不接收第三方 Go object。

**理由：** 覆盖 Windows 7、隔离崩溃、避免 Go ABI/依赖版本耦合，并允许跨语言 SDK。官方高频能力仍可静态编译成内置 Tool/Provider，但不属于 Plugin 系统。

## PG-002: 启动期加载，不热插拔

**决策：** Runtime 启动时完成发现、依赖排序、进程启动和 Proxy 注册。运行期间没有 install/uninstall/enable/disable/reload；配置或二进制变更在下次 Runtime 启动生效。

**理由：** 避免在途请求、依赖图和版本切换的状态复杂度。运行中 unexpected process exit 只做同版本有限次重启，不改变 Manifest/配置。

## PG-003: 本机进程级隔离

**决策：** 每个 Plugin 是独立 OS 进程。Unix 使用 Unix Socket；Windows 7 使用带启动 nonce 的 loopback TCP。MVP 不接受远程 Plugin endpoint。

**理由：** 本机进程边界清晰，且配置/Loader 能完整描述。远程模式需要额外 TLS、身份认证和威胁模型，进入需求后另行设计。

## PG-004: 单向 gRPC 拨号

**决策：** Runtime gRPC Client 拨号 Plugin gRPC Server。当前文档阶段的唯一权威 IDL 是 [`interface.md`](interface.md) 的完整 proto 代码块；实现首先将其落到目标路径 `api/plugin/v1/plugin.proto`，此后该文件成为唯一 wire contract。Plugin 不反向连接 Runtime，也不访问 internal package。

**影响：** 所有能力通过序列化 RPC 和 Runtime 侧 Proxy 暴露；取消、deadline 和 request ID 由 gRPC 传播。

## PG-005: RPC major + 业务 SemVer

**决策：** `protocol_version` 是 RPC major 字符串，v1 只接受精确 `"1"`。同一 major 只新增可忽略的 Protobuf 字段；新增 capability type 或其他破坏性 wire 变更升级 major。Plugin `version`、`requires_runtime` 和 dependency range 使用 SemVer。

**理由：** 不引入未定义的 minor/range 协商，Handshake 行为可直接实现。

## PG-006: 主配置是唯一 Runtime 配置源

**决策：** Plugin Runtime 配置只来自 `yaa.yaml plugins.entries[].config`，由 Config Loader 展开环境变量，Runtime 按 Manifest `config_schema` 校验后传给 Init。Plugin 自行应用未配置字段的默认值。

**影响：** JSON Schema `default` 只是 annotation；Runtime 不猜测或注入 Plugin 默认值。所有 `plugins.*` 变更需要重启。

## PG-007: Manifest typed capabilities

**决策：** `plugin.yaml` 声明身份、业务/RPC 版本、entry、依赖、config schema 和 typed `provides[]`。v1 capability 只有 `tool`；Skill 由 SKILL.md 加载，Hook/Middleware、Provider 和 Memory 不属于 v1 Plugin RPC。

**影响：** Manifest `provides[]` 必须与 Ready 响应的 type/name/description/schema 集合精确一致，之后 Runtime 才注册 Proxy。

## PG-008: 失败隔离与重启

**决策：** 单个 Plugin 初始启动失败只尝试一次并进入 non-fatal StartupReport。只有已 Ready 进程运行中退出时 Proxy 才立即返回 unavailable，并按配置有限重启；重连成功后原子替换 stable Proxy client handle。Health 超时只标记 degraded，不触发 Kill。

**影响：** 重启窗口中的请求会失败且不自动 replay；依赖 Plugin 不级联重启。

## 模块关系

```text
Config Manager ── config.PluginsConfig ──▶ Plugin Manager
                                             │
                    Manifest parser/Loader ◀─┤
                                             │
                         Runtime gRPC Client ─┼── local IPC ─▶ Plugin gRPC Server
                                             │
                                 Tool Proxy
                                             │
                                  Tool Manager
```

依赖方向固定为：

- Plugin Manager 读取 Config DTO，但 Config 包不导入 Plugin 模块。
- Loader/Manager 使用 `pkg/pluginrpc` 生成类型；Plugin SDK 不导入 `internal/*`。
- Runtime 各 Manager 只持有 Proxy，不持有 Plugin 实现对象。
- Skill Manager 可依赖已经注册的 Tool Proxy，不参与 Plugin 生命周期。

---

*最后更新: 2025-07-17*
