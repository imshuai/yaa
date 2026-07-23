# Plugin 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/plugin/`
> 依赖: `docs/architecture.md` §3.15、[`decisions.md`](decisions.md)

---

## 1. 定位

Plugin 是 Yaa! 的进程外扩展机制。每个 Plugin 是独立可执行进程，Runtime 在启动阶段读取 `plugin.yaml`、建立本地 gRPC 连接并注册能力代理；运行期间不加载 Go 符号，也不支持热插拔。

| 扩展方式 | 适用场景 | 运行边界 |
|----------|----------|----------|
| 内置 Tool / Provider | 官方高频能力 | 静态编译进 Runtime |
| Skill | Prompt、脚本和 Tool 编排 | 文件加载 |
| MCP | 标准协议接入外部 Tool | MCP transport |
| Plugin | 第三方 Tool | 独立进程 + Plugin RPC |

核心约束：

1. Plugin 只通过版本化 RPC 与 Runtime 通信。
2. Runtime 只持有进程句柄、RPC Client 和能力 Proxy，不持有第三方 Go 对象。
3. 单个 Plugin 失败只影响其能力并继续处理其他 Plugin；最终 Agent/Skill binding 引用该能力时启动失败。
4. Plugin 配置只来自主配置的 `plugins` 节点，修改后需重启 Runtime。

## 2. 启动协议

```text
Discover(plugin.yaml)
  → Validate(manifest, entry, dependencies)
  → Start process
  → Dial local gRPC endpoint
  → Handshake(runtime_protocol, expected_plugin_id) / verify startup_nonce echo
  → Init(config)
  → Ready(capabilities)
  → Register capability proxies
```

生命周期 RPC 为 `Handshake`、`Init`、`Ready`、`Health` 和 `Stop`。当前文档阶段以 [`interface.md`](interface.md) 的完整内嵌 IDL 为唯一权威；实现时先将其落到 `api/plugin/v1/plugin.proto`，届时该文件接管唯一 wire contract，`pkg/pluginrpc` 只包含适配器和生成代码。

## 3. Manifest 与能力

每个 Plugin 目录必须包含 `plugin.yaml` 和同目录下的可执行文件：

```yaml
id: weather
version: 0.1.0
protocol_version: "1"
entry: yaa-plugin-weather
default_enabled: true
provides:
  - type: tool
    name: weather
    description: Query current weather by city
    schema:
      type: object
      properties:
        city: {type: string}
      required: [city]
config_schema:
  type: object
  properties:
    api_key: {type: string}
```

`provides` 是 typed list；每项包含 `type`、`name`、`description` 和 `schema`。v1 接受的 `type` 只有 `tool`。Skill 由 SKILL.md 加载；Hook/Middleware、Provider 和 Memory 都不使用 v1 Plugin RPC。Manifest 声明必须与 `Ready` 返回的 type/name/description/schema 集合精确一致，否则 Plugin 进入 `error` 状态。

## 4. Runtime 侧结构

Runtime 结构以 [`loader.md`](loader.md) 的 `PluginDescriptor`/`RPCClient` 和 [`manager.md`](manager.md) 的 `Entry`/`PluginState` 为唯一契约。Entry 只通过冻结的 `Descriptor.Manifest` 读取 Manifest，不保存第二份副本；配置中的 `enabled` 不是运行时状态，显式 `true`/`false` 优先于 `default_enabled`，未声明条目时才使用 Manifest 默认值。

## 5. 能力代理

Runtime 为每个 Tool capability 创建 `PluginToolProxy` 并注册到 Tool Manager。v1 不提供其他 Plugin capability。

Plugin 不接收 Runtime 指针、Manager、数据库连接或内部 interface。调用只传递可序列化请求、响应、错误和取消信号。

## 6. 配置

```yaml
plugins:
  paths: ["./plugins"]
  auto_start: true
  health_interval: 30s
  restart:
    enabled: true
    max_attempts: 3
    backoff: 1s
  entries:
    - id: weather
      enabled: true
      config:
        api_key: "${WEATHER_API_KEY}"
```

完整字段、默认值和覆盖规则见 [`config-ref.md`](config-ref.md)。`auto_start: false` 时只发现和校验 Manifest，不启动任何 Plugin 进程。

## 7. 故障与关闭

| 场景 | 行为 |
|------|------|
| Manifest、entry 或协议不合法 | 不启动该 Plugin，状态为 `error` |
| 初始启动、Dial、Handshake、Init 或 Ready 失败 | 本次只尝试一次，清理后进入 `error` 并写 StartupReport |
| 运行中进程退出 | 已注册 Proxy 变为 unavailable，有限次重启并原子替换 client；耗尽后保持 `error` |
| Runtime 关闭 | Proxy 先 unavailable，再 Stop、Wait（超时 Kill+Wait）、注销、清理 IPC |

运行期间不提供 install、uninstall、enable、disable 或 reload 操作。配置或二进制变化只在下一次 Runtime 启动时生效。

## 8. 兼容性

Plugin 业务版本使用 SemVer；wire 兼容性由握手的 `protocol_version` 和 Manifest/Ready capability 的 `type/name/description/schema` 精确校验共同决定，v1 不做 capability 降级协商：

| 变更 | 规则 |
|------|------|
| 新增可选消息字段 | 保持协议版本兼容 |
| 新增可忽略的 Protobuf 字段 | 保持 major |
| 新增 capability type | 升级 RPC major；v1 没有 capability 降级协商 |
| 修改字段语义、删除字段或 RPC | RPC major 递增 |
| 不兼容版本组合 | 拒绝启动，不做 Go interface 类型断言 |

## 9. 文档索引

| 文件 | 内容 |
|------|------|
| [interface.md](interface.md) | Plugin RPC、能力描述和兼容策略 |
| [manager.md](manager.md) | 发现、依赖排序、进程状态和关闭流程 |
| [loader.md](loader.md) | Manifest、文件校验、进程启动和 IPC |
| [integration.md](integration.md) | Tool 集成以及 Skill 依赖关系 |
| [config-ref.md](config-ref.md) | 完整配置 schema |
| [errors.md](errors.md) | 错误分类与恢复策略 |
| [observability.md](observability.md) | 日志、指标和健康信息 |
| [decisions.md](decisions.md) | 架构决策 |
| [checklist.md](checklist.md) | 实现验收清单 |

---

*最后更新: 2025-07-17*
