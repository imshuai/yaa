# MCP 集成设计

> 文档路径: `docs/mcp/integration.md`
> 上级: [`README.md`](README.md)

---

## 1. Tool 集成

MCP Client 完成 `tools/list` 后，将每个 Tool 转换为 Yaa! Tool Definition，并注册带命名空间的 Proxy：

```text
MCP Server: search
MCP Tool:   query
Yaa! Tool:  mcp.search.query
```

同一上游的所有 Proxy 共享一个稳定 handle：

```go
import (
    "context"
    "encoding/json"
    "strings"
    "sync/atomic"
    "time"
)

type ProxyHandle struct {
    client atomic.Pointer[Client]
}

type MCPToolProxy struct {
    server     string
    remoteName string
    schema     json.RawMessage
    timeout    time.Duration // 0 表示只使用 Tool Manager 的 caller deadline
    handle     *ProxyHandle
}

func (p *MCPToolProxy) Execute(ctx context.Context, scope tool.ExecutionScope, params map[string]any) (tool.ToolResult, error) {
    client := p.handle.client.Load()
    if client == nil {
        return tool.ToolResult{}, ErrMCPUnavailable
    }
    callCtx := ctx
    stopTimeout := func() {}
    if p.timeout > 0 {
        var cancel context.CancelCauseFunc
        callCtx, cancel = context.WithCancelCause(ctx)
        timer := time.AfterFunc(p.timeout, func() {
            cancel(ErrMCPToolTimeout)
        })
        stopTimeout = func() {
            timer.Stop()
            cancel(nil)
        }
    }
    defer stopTimeout()
    result, err := client.CallTool(callCtx, p.remoteName, params)
    if ctx.Err() != nil {
        return tool.ToolResult{}, context.Cause(ctx)
    }
    if callCtx.Err() != nil {
        return tool.ToolResult{}, context.Cause(callCtx)
    }
    return toToolResult(result, err)
}

func toToolResult(result *CallToolResult, err error) (tool.ToolResult, error) {
    if err != nil {
        return tool.ToolResult{}, err
    }
    if result == nil {
        return tool.ToolResult{}, ErrMCPProtocolError
    }
    parts := make([]string, 0, len(result.Content))
    total := 0
    for _, content := range result.Content {
        if content.Type != "text" {
            return tool.ToolResult{}, ErrMCPUnsupportedContent
        }
        total += len(content.Text)
        if total > 4<<20 {
            return tool.ToolResult{}, ErrMCPProtocolError
        }
        parts = append(parts, content.Text)
    }
    return tool.ToolResult{
        Content: strings.Join(parts, "\n"),
        IsError: result.IsError,
    }, nil
}

func toMCPResult(result tool.ToolResult) *CallToolResult {
    return &CallToolResult{
        Content: []Content{{Type: "text", Text: result.Content}},
        IsError: result.IsError,
    }
}
```

Proxy 保存 Server 名称、远端 Tool 名称、不可变 Schema 和可选 MCP hard timeout；参数校验在调用前执行，结果统一转换为 `tool.ToolResult`。Go 1.20 没有 `context.WithTimeoutCause`，因此非零 hard cap 使用 `WithCancelCause` + `time.AfterFunc`；返回前先检查 caller、再检查 child cause，cleanup 最后停止 timer 并调用 `cancel(nil)`。空 content 映射为空文本；多个 text block 按 wire 顺序以单个换行连接；`isError` 原样保留；内部 Meta 不上 wire。v1 Client 遇到 image/audio/resource block 返回 `ErrMCPUnsupportedContent`，但不关闭一条语法合法的连接。反向映射始终产生一个 text block，即使 Content 为空。首次发现成功后 Proxy 注册一次；暂时断线对共享 handle 执行 `Store(nil)`，不从 Tool Manager 注销。`scope` 只用于 Yaa! 权限/审计，不得塞进远端 Tool arguments。

## 2. Agent / Session 集成

Agent 的 `tools` 白名单引用完整名称 `mcp.<server>.<tool>`。每次 Agent 请求从当前 Tool Manager 和 Agent allowlist 投影可用 Tool；Session snapshot 不保存 Tool 集合。Server 断线后不改写历史 Tool unit，下一次调用返回 `ErrMCPUnavailable`。

## 3. Config 集成

```yaml
mcp:
  servers:
    - name: filesystem
      transport: stdio
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
      auto_start: true
```

`command` 是字符串，参数必须放在 `args` 数组；远程标准传输使用 `streamable_http`，旧版 Server 才使用 `sse`。配置字段的完整定义见 [config-ref.md](config-ref.md)。

## 4. Runtime 启动顺序

完整 Runtime 顺序中 Plugin Proxy 先于 MCP Proxy 注册，二者都先于 Skill binding；本节只展开 MCP 子流程：

```text
Config.Parse → Config.Validate
  → Tool Manager
  → MCP Manager.Prepare
  → Transport.Start
  → initialize（按 transport 选择版本）
  → tools/list(cursor)
  → Register Stable Tool Proxy
  → Skill Load
  → Config.Activate(binding)
  → MCP Manager.Activate（本地 Serve）
```

任一外部 Server 连接失败只影响对应连接；Manager 记录错误并继续启动其他 Server。若配置引用了因此未能首次注册的 Tool，统一 binding 校验会拒绝启动。配置缺失、未知 transport 或协议版本不兼容属于启动连接错误，状态为 `error`。本地 expose Server 在 `Prepare` 阶段完成配置、principal、Tool allowlist 和 listener 校验，但 `Config.Activate` 成功前不调用 `Serve`。

关闭时 Runtime 先调用 `MCP Manager.Stop(ctx)`；即使 Stop 因 caller deadline 返回，也必须继续等待 `MCP Manager.Done()`，再调用 `Stop(context.Background())` 取得缓存的最终 teardown error，之后才继续 [Runtime 既定的逆序关闭链](../architecture.md#31-runtime)。

## 5. Retry 与幂等

- Manager 对连接失败按 1s、2s、4s 退避创建新 Client；Client 自身不重连。
- 任何已经发送的 `tools/call` 都不自动重放；断线或 timeout 后向调用方返回结果不确定错误，重连只服务新请求。
- 重连成功后重新 initialize 和完整分页 tools/list；只有名称、description 和 input schema 与既有 Proxy 快照精确一致时才原子替换 handle，差异需要重启 Runtime。
- `mcp.*` 的结构性变更由文件 watcher 检测为 `restart_required`，重启后按 `auto_start` 重新连接；正在执行的请求不迁移到新连接。Remote API 不拥有 MCP 配置。

## 6. Yaa! 作为 MCP Server

Yaa! Server 只暴露 `mcp.server.exposed_tools` 中的 Tool。MVP 响应 `initialize`、`tools/list`、`tools/call` 和 `ping`；Resource/Prompt 方法返回 `-32601`。

本地 `Serve` 意外退出时 Manager 必须原子标记 unhealthy，使 `Ready()` 返回 false；不得只写日志后继续报告 Runtime Ready。`Stop` 引起的 context 取消和 transport close 不算运行期故障。

## 7. 安全

- `stdio` 子进程继承经过过滤的 `env`，不继承 Runtime 全部环境。
- 远程 HTTP 使用 TLS 时校验证书；Token 不写入 URL 日志。
- HTTP Header 值通过 `${VAR}` 注入，配置读回、日志和错误中必须脱敏。
- Streamable HTTP Server 默认只绑定 loopback。无 `Origin` 的非浏览器请求允许；请求携带 `Origin` 时必须精确命中非空 allowlist，否则返回 403。
- MCP Tool 与内置 Tool 使用相同 Agent/RBAC 检查，不因来自外部 Server 而绕过权限。
- Tool 结果中的 HTML/二进制内容按不可信数据处理并受大小限制。

---

*最后更新: 2025-07-17*
