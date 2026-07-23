# Plugin 可观测性

> 文档路径: `docs/plugin/observability.md`
> 上级: [`README.md`](README.md)
> 依赖: [`manager.md`](manager.md)、[`architecture.md`](../architecture.md)

---

## 1. 生命周期日志

Manager 按统一事件名记录 Manifest、进程、RPC 和能力代理状态。日志必须带 `plugin_id`、`version`、`protocol_version` 和 `attempt`；不得记录配置中的 Secret。

| 事件 | 级别 | 触发时机 |
|------|------|----------|
| `plugin.discovered` | DEBUG | Manifest 通过基础解析 |
| `plugin.starting` | INFO | 准备启动子进程 |
| `plugin.handshake_failed` | ERROR | RPC 握手失败 |
| `plugin.ready` | INFO | Init 成功且 Proxy 注册完成 |
| `plugin.health_changed` | WARN/INFO | 健康等级改变 |
| `plugin.process_exit` | ERROR/INFO | unexpected 退出为 ERROR，Runtime 关闭期为 INFO |
| `plugin.stopped` | INFO | 正常关闭完成 |

```go
logger.Info("plugin.ready",
    "plugin_id", id,
    "version", manifest.Version,
    "protocol_version", manifest.ProtocolVersion,
    "capability_count", len(capabilities),
)
```

## 2. 健康报告

```go
type PluginHealth struct {
    ID          string    `json:"id"`
    Version     string    `json:"version"`
    State       string    `json:"state"` // starting / ready / error / stopped
    Level       string    `json:"level"` // healthy / degraded / unhealthy / unknown
    LastChecked time.Time `json:"last_checked"`
    Uptime      time.Duration `json:"uptime"`
    LastError   string    `json:"last_error,omitempty"`
}
```

健康检查调用插件 RPC `Health`；如果 RPC 超时，只把健康级别标记为 `degraded`，Entry 仍保持 `ready`。`PluginHealth.Level` 和 `LastChecked` 分别来自 `Entry.Health.Level` 与 `Entry.Health.Timestamp` snapshot；monitor 在 Manager `mu` 写锁下替换 snapshot，对外汇总在 `mu.RLock` 下复制后再序列化，RPC 与序列化过程都不持锁。自动重启只由 unexpected process exit 触发。

## 3. 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `yaa_plugin_start_total` | Counter | `plugin`, `result` | 启动尝试次数 |
| `yaa_plugin_start_duration_seconds` | Histogram | `plugin` | 启动到 Ready 的耗时 |
| `yaa_plugin_active` | Gauge | — | 当前 Ready 插件数 |
| `yaa_plugin_rpc_total` | Counter | `plugin`, `method`, `result` | RPC 调用次数 |
| `yaa_plugin_rpc_duration_seconds` | Histogram | `plugin`, `method` | RPC 延迟 |
| `yaa_plugin_process_exit_total` | Counter | `plugin`, `code` | 子进程退出次数 |
| `yaa_plugin_error_total` | Counter | `plugin`, `kind` | Manifest/协议/调用错误 |

指标的 label 不包含用户输入、Token 或完整错误文本，避免高基数和敏感信息泄漏。

## 4. 事件边界

Plugin 事件先写入 Runtime 内部事件总线并汇总到 Runtime health。当前 Remote API/Tool 文档没有 Plugin 专用公共契约，因此不得声称存在 `plugin_list`、`plugin_health` 或未登记的事件 endpoint；未来新增时必须先补齐索引、认证和断线语义。

---

*最后更新: 2025-07-17*
