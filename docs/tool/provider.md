# Tool 与 Provider 的名称边界

> 文档路径: `docs/tool/provider.md`
> 本文是 canonical Tool name 与 Provider-safe alias 之间转换的唯一权威契约。

---

## 1. 两种名称

Yaa! 内部只使用 **canonical Tool name**。Tool Manager 注册表、Agent/Skill/MCP
绑定、Session snapshot、Remote API、日志和实际执行都保存或传递 canonical name。
canonical name 必须是合法 UTF-8、长度为 1..256 bytes，且不能包含 Unicode
控制字符；它不受单个 Provider 的 function-name 语法限制，例如
`mcp.filesystem.read_file` 是合法 canonical name。

**Provider-safe alias** 只存在于一次 Agent turn 的 Provider wire 投影中。它必须满足：

```text
^[A-Za-z_][A-Za-z0-9_-]{0,63}$
```

alias 不能写入 Tool Manager、Session、Storage、Remote DTO、Planner capability 或 MCP
上游调用。`provider.ToolDef`、`provider.ToolCall` 和 `provider.Message` 是统一值类型，
字段处于 canonical 还是 alias 语义由它所在的边界决定：投影前和精确反查后为
canonical；发送给 Provider 的请求副本及 Provider 原始响应为 alias。

## 2. 确定性 alias 算法

算法对 canonical name 的原始 UTF-8 bytes 工作；不得 trim、Unicode normalize、
case-fold、截断或加入顺序相关 suffix：

```go
var providerSafeToolName = regexp.MustCompile(
    `^[A-Za-z_][A-Za-z0-9_-]{0,63}$`,
)

func providerToolAlias(canonical string) string {
    if providerSafeToolName.MatchString(canonical) {
        return canonical
    }
    sum := sha256.Sum256([]byte(canonical))
    encoded := base32.StdEncoding.WithPadding(base32.NoPadding).
        EncodeToString(sum[:])
    return "t_" + strings.ToLower(encoded)
}
```

unsafe canonical name 的结果固定为 `t_` 加完整 SHA-256 的 52 个小写 base32
字符，共 54 ASCII bytes。完整 digest 不截断。可逆性来自 turn-local map，不从 hash
反解 canonical name。

每次加入映射时都检查 `alias -> canonical`：若同一个 alias 已对应另一个 canonical
name，返回 `ErrToolAliasCollision`。这也覆盖一个安全 canonical name 恰好等于另一个
unsafe name 的 hash alias。碰撞是硬错误，不能通过追加数字、注册顺序或 Provider
特例消解。测试无需寻找 SHA-256 碰撞：先计算任一 unsafe canonical 的 alias，再把该
alias 本身作为第二个安全 canonical，即可稳定覆盖这一分支。

## 3. 每 turn 的不可变投影

Agent 在 turn 开始时创建一次 `ProviderToolProjection`，并在该 turn 的所有 Provider
round 中复用。输入集合是以下两者的并集：

1. `ToolManager.ListForAgent(agentID)` 中当前 enabled 且 authorized 的 definitions；
2. 当前 Session snapshot 历史中 assistant `ToolCalls[].Function.Name` 和非空 tool
   message `Name` 的 canonical name。

definitions 只含第 1 类；历史中已 disabled、unregistered 或当前未授权的 Tool 仍加入
`canonical -> alias`，使合法旧消息可以发送给 Provider，但绝不能因此恢复执行权限。
投影维护：

- 全部并集的 `canonical -> alias`，用于请求投影；
- 仅当前 definitions 的 executable `alias -> canonical`，用于响应反查。

构造按 canonical name 的 UTF-8 bytes 升序处理并检查整个并集的 alias 唯一性；输出
definitions 保持相同稳定顺序。映射和 schema 在构造时深拷贝且字段不导出；返回
definitions 或请求时再次复制，调用方不能修改已冻结投影。

Tool Manager 的边界 API 为：

```go
type ProviderToolProjection struct { /* immutable, unexported maps/defs */ }

func (m *Manager) ToToolDefs(
    agentID string,
    history []provider.Message,
) (*ProviderToolProjection, error)

func (p *ProviderToolProjection) ProjectRequest(
    req provider.ChatRequest,
) (provider.ChatRequest, error)

func (p *ProviderToolProjection) ResolveExecutable(
    alias string,
) (canonical string, ok bool)
```

`ToToolDefs` 同时是 alias 碰撞检查点。启动 binding 校验还必须对每个 Agent 的当前
definitions 构造一次投影，在 Runtime Ready 前尽早拒绝碰撞；每 turn 构造仍不可省略，
因为恢复的历史会扩大并集。

## 4. 请求投影

Agent 先用 canonical 数据组装除 `Tools` 之外的 `provider.ChatRequest`，再调用
`ProjectRequest`。输入 `req.Tools` 必须为空；该方法从投影内冻结的 current definitions
填充 wire `Tools`，避免调用方混入未授权 definition。`ProjectRequest` 必须递归深拷贝
所有 slice、map 和 `json.RawMessage`，并只改写：

- 冻结 definitions 的 `Tools[].Function.Name`；
- 历史 assistant message 的 `ToolCalls[].Function.Name`；
- `Role == "tool"` 且非空的 `Message.Name`；
- `ToolChoice.Mode == "specific"` 时的 `ToolChoice.Tool`。

ToolCall ID、Type、Arguments、Content、ReasoningContent、Refusal、schema 和其他请求字段
原样复制。`specific` 的调用方输入必须是当前 executable canonical name；无法在当前
definitions 中解析时在调用 Provider 前失败。其他 ToolChoice mode 不改写。

投影完成的完整请求才传给 `Context.Build`。Context 只复制、摘要或删除消息 unit，
不持有 alias map，也不重新投影名称；其 `EstimateInputTokens` 因而估算与实际 wire
完全相同的 alias、definitions、历史和 `specific` ToolChoice。Context 返回的请求可直接
交给 `Provider.Chat` 或 `Provider.StreamChat`，不得再做第二次名称转换。

## 5. Provider 响应反查

Provider adapter 原样映射厂商返回的 function name，不生成、修复或猜测 alias。
Agent 对 direct 和 stream 最终都调用同一反查步骤：

1. 要求完整 alias 满足 provider-safe regex；
2. 用冻结投影的 executable `alias -> canonical` 做精确、大小写敏感查找；
3. 只替换 `ToolCall.Function.Name`，保留 ID、Type 和 Arguments；
4. 反查全部成功后，才把 canonical calls 交给 `ToolManager.ExecuteBatch`、Session 和
   Remote event。

历史专用 alias、未知 alias、非法 alias 或重复/冲突 call 都返回
`ErrAgentProviderProtocol`。Agent 不执行任何 Tool、不发布 `tool_call`、不提交 partial
assistant/Tool unit；wire alias 也不能作为 `ErrToolNotFound` ToolResult 回传给模型。

### 5.1 Direct

`Provider.Chat` 返回完整 calls。Agent 先校验整个 response，再逐 call 反查；必须采用
all-or-nothing，不能在后续 call 失败前执行前面的 call。

### 5.2 Stream

厂商若用数组 index 而不是稳定 call ID 标识 fragment，adapter 必须在单次响应内维护
`index -> call ID`，并在每个统一 `Delta.ToolCalls` fragment 中补回同一个非空 ID。
首次出现的非空厂商 ID 绑定该 index；厂商协议完全不提供 ID 时，adapter 必须按其
direct/stream 共用的规则从 response ID + index 派生 response-local opaque ID，不能按
fragment 随机生成。index/ID 冲突作为 Provider 协议错误终止 channel。Adapter 不拼接或
反查 name。

Agent 按稳定 ID 分别拼接 `Function.Name` 和 `Arguments`，直到收到合法
`finish_reason=tool_calls`；只有完整 alias 拼接完成后才反查一次。任意 chunk 切分都必须
得到与 direct 相同的 canonical `provider.ToolCall`。

## 6. 执行、持久化与外部边界

- `ToolManager.Execute/ExecuteBatch` 只接受 canonical name；MCP Proxy 使用自己保存的
  `remoteName` 调上游，绝不能收到 Provider alias。
- Session 保存 canonical ToolCalls 和非空 tool message Name；Restore 只验证名称与消息
  序列，不要求历史 Tool 仍已注册、enabled 或 authorized。
- Remote 的 `ToolInfo`、Agent/Skill Tool 列表、Session history、`tool_call` 和
  `tool_result.name` 都是 canonical name；alias 没有 REST/SSE/WS 字段。
- Planner capability 使用 canonical name，不经过 Provider alias 投影。
- 投影只活到 turn 结束。切换 Provider、进程重启或算法实现升级不能污染持久历史。

## 7. 最小验证矩阵

| 场景 | 预期 |
|------|------|
| `shell` | alias 仍为 `shell` |
| `mcp.filesystem.read_file`、Unicode、数字开头、65-byte name | 54-byte hash alias，匹配 provider-safe regex |
| 两个 canonical name 产生同一 alias | `ErrToolAliasCollision`，请求未发送 |
| history-only Tool | 可投影历史；Provider 返回其 alias 时 `ErrAgentProviderProtocol` |
| `specific` ToolChoice | canonical 输入映射到同一 definition alias，estimator 看见 alias |
| direct 与任意 stream fragment 切分 | 反查为完全相同的 canonical calls |
| unknown/非法 alias | 不执行、不发 `tool_call`、不提交 partial |
| Session Restore 后 Tool 已删除 | 恢复成功；历史仍可投影但不可执行 |

---

*最后更新: 2026-07-23*
