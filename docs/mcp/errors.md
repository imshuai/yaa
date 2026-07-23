# MCP 错误处理与降级策略

> 文档路径: `docs/mcp/errors.md`
> 上级: [`README.md`](README.md)

---

## 1. 错误分类

```go
var (
    ErrMCPConfig          = errors.New("invalid mcp config")
    ErrMCPConnRefused     = errors.New("mcp connection refused")
    ErrMCPConnTimeout     = errors.New("mcp connection timeout")
    ErrMCPAuthFailed      = errors.New("mcp upstream authentication failed")
    ErrMCPTransportClosed = errors.New("mcp transport closed")
    ErrMCPTransportWrite  = errors.New("mcp transport write failed")
    ErrMCPProtocolError   = errors.New("mcp protocol error")
    ErrMCPInvalidParams   = errors.New("invalid mcp parameters")
    ErrMCPToolNotFound    = errors.New("mcp tool not found")
    ErrMCPToolExecFailed  = errors.New("mcp tool execution failed")
    ErrMCPToolTimeout     = errors.New("mcp tool timeout")
    ErrMCPUnsupportedContent = errors.New("unsupported mcp content")
    ErrMCPUnavailable     = errors.New("mcp server unavailable")
)
```

| 错误 | 阶段 | 恢复动作 | 默认行为 |
|------|------|----------|----------|
| `ErrMCPConfig` | 配置校验 | 无 | 拒绝应用该 Server 配置 |
| `ErrMCPConnRefused` | 拨号 | 按配置重建连接 | 原调用失败 |
| `ErrMCPConnTimeout` | 拨号 | 按配置重建连接 | 原调用失败 |
| `ErrMCPAuthFailed` | 上游鉴权 | 无 | 状态置为 `error`，等待配置修复 |
| `ErrMCPTransportClosed` | 传输 | 仅重建连接 | Proxy unavailable；原调用不重试 |
| `ErrMCPTransportWrite` | 传输 | 仅重建连接 | 结果不确定；原调用不重试 |
| `ErrMCPProtocolError` | initialize/消息解析 | 无 | 关闭连接，状态置为 `error` |
| `ErrMCPInvalidParams` | Tool 调用 | 无 | 返回调用方 |
| `ErrMCPToolNotFound` | Tool 调用 | reconciliation | 返回调用方；定义漂移则保持 unavailable并要求重启 |
| `ErrMCPToolExecFailed` | Tool 执行 | 无 | 返回安全错误内容 |
| `ErrMCPToolTimeout` | Tool 执行 | 无 | 取消 context；不重放已发送请求 |
| `ErrMCPUnsupportedContent` | Tool result 转换 | 无 | 原调用失败；连接保持可用 |
| `ErrMCPUnavailable` | Proxy 调用 | 连接管理器可独立重连 | 提示 Server 当前不可用 |

配置校验错误必须携带字段路径，例如 `mcp.servers[1].url: required for streamable_http`；不再定义与 Config 模块重复的 `ErrNameRequired` 等零散 sentinel。

## 2. JSON-RPC 映射

| 场景 | JSON-RPC code / result |
|------|------------------------|
| JSON 解析失败 | `-32700 Parse error` |
| batch、缺失 `jsonrpc` 等非法 envelope | `-32600 Invalid Request` |
| 未实现 method（含 Resource/Prompt） | `-32601 Method not found` |
| 未知 Tool、非法 cursor、参数 schema 不匹配 | `-32602 Invalid params` |
| Tool 已找到但业务执行失败 | 正常 result，`isError: true` |

外部 Server 的 `-32000..-32099` 是 server-defined 范围。Client 保留原始 `code`、message 和最多 16 KiB、经过脱敏的 data 供受控诊断，但 typed error 的 `Error()` 只返回稳定文本。所有上游 `-32602` 统一映射 `ErrMCPInvalidParams`；不能解析 message 猜测是 unknown Tool。`ErrMCPToolNotFound` 只用于 Runtime 本地 catalog 查找。其他 server-defined code 映射 `ErrMCPToolExecFailed`，不能假设 `-32001` 固定表示过载。

## 3. 状态转换

```text
disconnected
  → connecting
  → connected
  → error
  → connecting  (允许重连时)
```

- `auto_start: false`：保持 `disconnected`。
- 配置、鉴权或协议错误：保持 `error`，不循环重试。
- 暂时连接/传输错误：由 Manager 为该 upstream 创建新 Client，最多按 `mcp.reconnect.max_attempts` 重试。
- 断线时保留该 Server 已注册的稳定 Tool Proxy，仅把 atomic client handle 置空。
- 重连并完成分页 `tools/list` 后，只有 Tool 名称、description 和 input schema 与既有快照精确一致才原子替换 handle；差异保持 `error` 并要求重启。

## 4. 重试与幂等

默认重连参数：初始 1s、指数退避、最大 60s、最多 3 次。重连总是重新执行 initialize 和完整分页 `tools/list`。

已发送请求不会因 transport 重连自动 replay，也不存在 `CallOptions.Idempotent` 例外。写入是否到达远端无法可靠判断，因此断线、写入失败或 timeout 后都把本次调用作为结果不确定错误返回；重连只重新执行 initialize 和 `tools/list`，业务 `arguments` 中不得注入 `_timeout_ms` 等非 Tool schema 字段。MCP Proxy 必须把这类错误标记为 Tool Manager 不可重试。

## 5. 降级

| 故障 | Runtime 行为 |
|------|--------------|
| 单个上游 Server 不可用 | 保留稳定 Proxy 并返回 `ErrMCPUnavailable`，其他 Tool 和 Agent 继续工作 |
| 所有上游 Server 不可用 | 保留内置 Tool；已有 MCP Proxy 返回 `ErrMCPUnavailable` |
| 协议版本不兼容 | 关闭连接并提示升级，不静默降级 |
| Tool 返回 `isError=true` | 保留 MCP 业务结果供调用方判断，不伪装为 transport error |

## 6. Remote API 边界

Remote MCP API 只有两个只读状态端点，不直接执行 Tool。MCP Proxy 在 Agent turn 中失败时由统一 Conversation/Remote 错误映射处理：真实 MCP 上游连接、鉴权或协议失败使用 `502 / 50201`；caller deadline 优先使用 `504 / 50401`，客户端取消后通常不再写响应。MCP wire 本身始终使用 JSON-RPC，不套 REST envelope。

---

*最后更新: 2025-07-17*
