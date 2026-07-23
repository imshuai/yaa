# Plugin 错误处理

> 文档路径: `docs/plugin/errors.md`
> 上级: [README.md](README.md)

---

## 1. 稳定错误

```go
var (
    ErrPluginNotFound             = errors.New("plugin not found")
    ErrPluginManifestInvalid      = errors.New("invalid plugin manifest")
    ErrPluginConfigInvalid        = errors.New("invalid plugin config")
    ErrPluginDependencyMissing    = errors.New("plugin dependency missing")
    ErrPluginCircularDependency   = errors.New("plugin circular dependency")
    ErrPluginRuntimeIncompatible  = errors.New("runtime version incompatible")
    ErrPluginProcessStart         = errors.New("plugin process start failed")
    ErrPluginConnectionTimeout    = errors.New("plugin connection timeout")
    ErrPluginProtocolIncompatible = errors.New("plugin protocol incompatible")
    ErrPluginInitFailed           = errors.New("plugin init failed")
    ErrPluginCapabilityConflict   = errors.New("plugin capability conflict")
    ErrPluginCallFailed           = errors.New("plugin call failed")
    ErrPluginCallTimeout          = errors.New("plugin call timeout")
    ErrPluginUnavailable          = errors.New("plugin unavailable")
    ErrPluginPermissionDenied     = errors.New("plugin permission denied")
)
```

| 错误 | 阶段 | 重试 | 行为 |
|------|------|:----:|------|
| `ErrPluginNotFound` / `ErrPluginManifestInvalid` | 发现 | 否 | 诊断；有 ID 时建立 error Entry |
| `ErrPluginConfigInvalid` | 启动前 schema | 否 | 不启动进程 |
| `ErrPluginDependencyMissing` / `ErrPluginCircularDependency` | DAG | 否 | 依赖链 error |
| `ErrPluginRuntimeIncompatible` | `requires_runtime` | 否 | 拒绝启动 |
| `ErrPluginProcessStart` | exec | 否 | StartupReport 记录 |
| `ErrPluginConnectionTimeout` | Dial | 否 | 清理进程和 endpoint |
| `ErrPluginProtocolIncompatible` | Handshake/Ready | 否 | 清理进程；提示 RPC major |
| `ErrPluginInitFailed` | Init/Ready | 否 | 清理进程 |
| `ErrPluginCapabilityConflict` | Proxy 注册 | 否 | 回滚全部本次 Proxy |
| `ErrPluginCallFailed` | 业务 RPC | 否 | 返回统一 hard error，不泄露 gRPC message/details |
| `ErrPluginCallTimeout` | 业务 RPC | 否 | 取消 context，不立即重启 |
| `ErrPluginUnavailable` | 进程退出/重启窗口 | 否 | Proxy 返回 unavailable |
| `ErrPluginPermissionDenied` | Agent 权限 | 否 | 返回统一权限错误 |

启动阶段不做隐式三次重试；只有 Runtime 运行中 unexpected process exit 才使用 `restart.*` 退避策略。这样启动耗时和错误边界可预测。

## 2. RPC 错误载荷

`InvokeTool` 的业务错误在 gRPC OK 的 `ToolResponse.error` 分支中返回；wire shape 与 IDL 一致：

```json
{
  "requestId": "call_01",
  "error": {
    "code": "TOOL_ERROR_CODE_TIMEOUT",
    "message": "plugin call timed out",
    "retryable": false
  }
}
```

`plugin_id` 位于请求中，`request_id` 由外层 `ToolResponse` 精确回显，不在 `ToolError` 重复。message 必须脱敏并限制为 512 UTF-8 bytes，不得返回堆栈、Secret、命令行或环境变量。Runtime 只按 enum 映射，绝不解析 message：

| `ToolErrorCode` | Runtime error |
|-----------------|---------------|
| `INVALID_ARGUMENT` | `tool.ErrInvalidParams` |
| `TIMEOUT` | `ErrPluginCallTimeout` |
| `UNAVAILABLE` | `ErrPluginUnavailable` |
| `INTERNAL` | `ErrPluginCallFailed` |
| `UNSPECIFIED` 或未知值 | `ErrPluginProtocolIncompatible`，使当前 handle unavailable 并 `Terminate` Client |

只有已知 code 且 `retryable=true` 才包装为 Tool Manager 的 `RetryableError`；timeout、取消、unavailable 和结果不确定错误始终不可重试。`UNSPECIFIED`/未知 enum、错 request ID 或非法 outcome 先返回 `ErrPluginProtocolIncompatible`，再由 Proxy CAS 置空当前 handle 并调用 `RPCClient.Terminate()`；若 Stop/monitor 已先接管，CAS 失败且当前调用不重复回收。由此产生的进程退出交给 monitor 的有限重启策略，耗尽后 Entry 保持 `error`。gRPC non-OK 不使用上述业务分支，其唯一映射为：caller context 已结束时返回 `context.Cause(ctx)`；`DeadlineExceeded` 映射 `ErrPluginCallTimeout`，`Unavailable` 映射 `ErrPluginUnavailable`，`PermissionDenied` 映射 `ErrPluginPermissionDenied`，其他 status 映射 `ErrPluginCallFailed`。稳定 `Error()` 不包含 gRPC message/details，原始诊断只能经大小限制和脱敏后写受控日志。

## 3. 运行中退出

```text
process exit (Runtime not stopping)
  → emit plugin.process_exit
  → ProxyHandle.Store(nil)
  → calls return ErrPluginUnavailable
  → restart.enabled ? backoff restart : state=error
  → successful Ready → ProxyHandle.Store(new client)
  → attempts exhausted → state=error
```

Runtime 正常 Stop 期间的 exit 是预期关闭，状态为 `stopped`，不触发重启。健康 RPC 超时只更新 degraded，不自动 Kill。

## 4. 版本与依赖

```yaml
id: weather
version: 1.2.0
protocol_version: "1"
requires_runtime: ">=0.1.0 <1.0.0"
dependencies:
  - id: shared-cache
    version: ">=1.0.0 <2.0.0"
    optional: false
```

`protocol_version` 必须精确等于支持的 RPC major `"1"`。Runtime SemVer range 和 Plugin 业务 SemVer 使用 parser 比较；不兼容返回 `ErrPluginRuntimeIncompatible`，不能误报为 protocol error。

---

*最后更新: 2025-07-17*
