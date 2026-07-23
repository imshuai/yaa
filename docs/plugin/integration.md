# Plugin 集成设计

> Plugin 与 Tool / Skill / Runtime 的集成方案
> 依赖: [`architecture.md`](../architecture.md) §3.7-§3.15、[`tool/manager.md`](../tool/manager.md)、[`skill/manager.md`](../skill/manager.md)

---

## 1. 集成边界

Plugin 运行在独立进程中，所有集成都经过 Plugin RPC。Runtime 侧只创建 Proxy，不把内部 Go interface、指针或数据库连接传给插件。

```text
Plugin Process
  └─ Tool capability ──RPC──▶ Tool Proxy ──▶ Tool Manager
```

## 2. Tool 集成

插件 Manifest 声明 Tool 名称和 JSON Schema。握手完成后，Tool Manager 为每个声明创建 Proxy：

```text
Ready(capabilities)
  → 校验 type/name/description/schema 集合及名称唯一性
  → Register(PluginToolProxy)
  → Agent 的 tools 白名单可引用该名称
```

调用时 Proxy 显式接收 `tool.ExecutionScope`，并把 `AgentID`、`SessionID`、JSON 参数和 request ID 写入 typed ToolRequest；超时或取消由 Runtime 关闭对应 RPC context。

```go
// 位于 internal/plugin；plugin 依赖 tool，tool 不反向依赖 plugin。
type PluginToolProxy struct {
    pluginID   string
    name       string
    description string
    schema     json.RawMessage
    handle     *ProxyHandle
}

func NewPluginToolProxy(
    pluginID string,
    descriptor CapabilityDescriptor,
    handle *ProxyHandle,
) (*PluginToolProxy, error) {
    schema, err := json.Marshal(descriptor.Schema)
    if err != nil {
        return nil, fmt.Errorf("%w: invalid tool schema", ErrPluginProtocolIncompatible)
    }
    return &PluginToolProxy{
        pluginID: pluginID, name: descriptor.Name,
        description: descriptor.Description,
        schema: append(json.RawMessage(nil), schema...), handle: handle,
    }, nil
}

func (p *PluginToolProxy) Name() string                { return p.name }
func (p *PluginToolProxy) Description() string         { return p.description }
func (p *PluginToolProxy) Parameters() json.RawMessage { return append(json.RawMessage(nil), p.schema...) }

func (p *PluginToolProxy) Execute(
    ctx context.Context,
    scope tool.ExecutionScope,
    params map[string]any,
) (tool.ToolResult, error) {
    client, err := p.handle.Load()
    if err != nil {
        return tool.ToolResult{}, err
    }
    requestID := newRequestID()
    response, err := client.InvokeTool(ctx, ToolRequest{
        PluginID: p.pluginID, RequestID: requestID,
        AgentID: scope.AgentID, SessionID: scope.SessionID,
        Name: p.name, Arguments: params,
    })
    if err != nil {
        return tool.ToolResult{}, mapGRPCError(ctx, err)
    }
    result, err := mapToolResponse(requestID, response)
    if errors.Is(err, ErrPluginProtocolIncompatible) {
        // 只回收仍由本 Proxy 发布的 Client；若 CAS 失败，生命周期 owner 已接管。
        if p.handle.Invalidate(client) {
            _ = client.Terminate()
        }
    }
    return result, err
}
```

`newRequestID` 使用 Runtime 统一的不可预测 ID helper；`mapToolResponse` 先校验回显 ID 和 outcome 恰有一项，再按 [errors.md](errors.md#2-rpc-错误载荷) 的唯一表转换。Result 的 content/is_error/meta 深拷贝为 `tool.ToolResult`；Error 分支返回 typed hard error。错 ID、缺失/多个 outcome、`UNSPECIFIED` 或未知 enum 都是协议违规：Proxy 先用 CAS 使当前 handle unavailable，再由唯一 owner `RPCClient.Terminate()` 同步关闭 transport、Kill/Wait 并清理 endpoint。CAS 失败表示 Stop/monitor 已接管该 Client，不能误杀之后发布的新 Client。进程退出随后由 monitor 按有限重启策略处理。Proxy 不从 params 推导 identity，也不把 scope 注入 arguments。

## 3. Skill 依赖

Skill 本身不由 Plugin 提供。Skill 可以声明依赖 Plugin 提供的 Tool 名称，但 Skill Manager 不负责启动插件；依赖检查只读取 Runtime 已注册的 Proxy。被 Agent/Skill 引用的 Proxy 不存在、disabled 或不可用时，`skill.Load` 返回 `ErrSkillToolUnavailable` 并阻止 Runtime Ready；Skill 不增加 `Error` 状态，也不隐式启动新进程。

## 4. 生命周期与顺序

```text
Config → Storage → Provider → Memory → Tool builtins
       → Plugin Discover → dependency order
       → Plugin Start/Handshake/Init
       → Register Plugin Proxies → MCP Prepare/Register Proxies
       → Skill Load → Config Activate(binding) → MCP Activate(local Serve)
       → Session Restore → Context
       → Agent（含 per-Agent Planner）→ Auth → API → Config Watcher → Ready
```

Runtime 关闭时先把 stable Proxy handle 置为 unavailable，再调用 `Stop`、等待进程退出（超时则 Kill 后仍 Wait）、注销 Proxy并清理 IPC 端点。`StopAll(ctx)` 的 caller deadline 只限制本次等待；即使它先返回 deadline error，Runtime 仍须在关闭 Tool Manager 和退出主进程前等待 Plugin Manager 的 `Done()`，并调用 `WaitStopped()` 取得最终 teardown 结果。配置变化不执行热插拔，下一次启动才重新建立连接。

## 5. 安全边界

- Manifest `entry` 必须位于配置的 Plugin 搜索目录内，禁止目录穿越。
- 只允许 Runtime 启动的本机进程：Unix Socket 或 Windows loopback TCP；v1 不接受远程 endpoint。
- Plugin 配置中的 Secret 在日志、健康响应和错误载荷中一律脱敏。
- Tool 请求继承 Agent 的执行 scope，不因跨进程而绕过 Agent Tool allowlist。

## 6. 故障与降级

| 故障 | Runtime 行为 |
|------|--------------|
| Plugin 进程启动失败 | 标记该 Plugin `error`，继续处理其他 Plugin；最终 binding 若引用其 Tool 则阻止 Ready |
| 握手/协议不兼容 | 停止进程，不重试，提示升级 |
| 单次 Tool RPC 超时 | 取消 RPC，返回统一超时错误 |
| Tool 响应错 ID、非法 outcome 或未知 enum | 原子置空当前 handle 并 `Terminate` Client；monitor 按运行中退出策略处理 |
| 进程运行中退出 | Proxy 变为 unavailable；有限次退避重启，超过上限后保持注册但不可用 |

## 7. 配置示例

```yaml
plugins:
  paths: ["./plugins"]
  auto_start: true
  entries:
    - id: weather
      enabled: true
      config:
        api_key: "${WEATHER_API_KEY}"

agents:
  - id: default
    provider: openai
    tools: [weather]
```

---

*最后更新: 2025-07-17*
