# 错误处理

> 文档路径: docs/tool/errors.md
> 上级: README.md 9

---

## 9. 错误处理

### 9.1 错误分类

```go
var (
    ErrToolNotFound       = errors.New("tool not found")
    ErrToolDisabled       = errors.New("tool disabled")
    ErrPermissionDenied   = errors.New("tool permission denied")
    ErrInvalidParams      = errors.New("invalid tool arguments")
    ErrToolTimeout        = errors.New("tool execution timed out")
    ErrToolAliasCollision = errors.New("tool provider alias collision")
)

type ValidationError struct {
    Path    string // JSON Schema 字段路径，不含被拒绝的值
    Keyword string // required/type/enum/range/additionalProperties
}

func (e *ValidationError) Error() string { return "invalid tool arguments" }
func (e *ValidationError) Unwrap() error { return ErrInvalidParams }
```

这些 sentinel 和 `ValidationError` 只在 Tool 包定义一次；实现可包装 cause，但稳定 `Error()` 不包含原始参数或下游正文。

| 错误类型 | 说明 | 处理方式 |
|----------|------|---------|
| `ErrToolNotFound` | Tool 未注册 | Batch 投影稳定分类给 LLM |
| `ErrToolDisabled` | Tool 已禁用 | 同上 |
| `ErrPermissionDenied` | Agent 无权限 | 同上，不暴露 allowlist |
| `ErrInvalidParams` | 参数校验失败 | 只投影字段路径与稳定校验分类 |
| `ErrToolTimeout` | Tool Manager 的 timeout timer 先于 caller 触发 | 投影固定超时信息 |
| `ErrToolAliasCollision` | 两个 canonical name 映射到同一 Provider alias | 启动 binding 或 turn 投影硬失败，不调用 Provider/Tool |
| Tool 返回的 `IsError` | Tool 内部逻辑错误 | 返回给 LLM，LLM 可调整策略 |
| Tool 返回的 `error` | 执行异常 | 按重试策略处理；Batch 最终投影稳定分类 |

### 9.2 错误传递给 LLM

`ExecuteBatch` 对单项失败生成 `ToolResult{IsError:true}`，Agent 再把结果放入 `role="tool"` 消息。原始 error、路径、URL、命令输出、凭据和下游响应正文只进入受控脱敏日志，不能进入 Session、Provider Prompt 或 Remote frame：

```go
func ErrorResult(err error) ToolResult {
    message := "tool execution failed"
    switch {
    case errors.Is(err, ErrToolNotFound):
        message = "tool not found"
    case errors.Is(err, ErrToolDisabled):
        message = "tool disabled"
    case errors.Is(err, ErrPermissionDenied):
        message = "tool permission denied"
    case errors.Is(err, ErrInvalidParams):
        message = "invalid tool arguments"
    case errors.Is(err, ErrToolTimeout):
        message = "tool execution timed out"
    }
    return ToolResult{Content: message, IsError: true}
}
```

`ErrorResult` 是 Manager Batch 和 MCP Server adapter 共用的唯一安全投影。稳定 message 使用固定枚举且不拼接 `err.Error()`；字段级校验详情也必须来自 JSON Schema validator 的脱敏结构并受 `tools.max_result_tokens` 限制。只有 caller context 取消或 deadline 到期使整个 Batch 返回非 nil error，且该失败不生成 Session Tool unit。

Tool Manager 在 Go 1.20 使用 `context.WithCancelCause(ctx)`，并由 `time.AfterFunc(effective.Timeout, func() { cancel(ErrToolTimeout) })` 设置 Manager timeout cause。Tool 返回后先检查 caller：`ctx.Err()!=nil` 时原样返回 `context.Cause(ctx)`，使 request deadline、显式 cancel、Agent Stop 和 Runtime shutdown 保留各自 cause；caller 仍有效但 `callCtx.Err()!=nil` 时才返回 `context.Cause(callCtx)`，此时 Manager timer 稳定为 `ErrToolTimeout` 并可由 `ErrorResult` 转成单项结果。不得用 `ctx.Err()` 丢失 cause，也不得把 caller 失败投影成 LLM 可见 timeout result。cleanup 在读取 cause 后停止 timer 并 `cancel(nil)`；不能引用 Go 1.21 才新增的 `context.WithTimeoutCause`。

`ErrToolAliasCollision` 不属于执行期单项失败，不能加入 `ErrorResult` switch 或伪造成 LLM 可见 Tool result。它的唯一行为由 [Provider-safe Tool alias 契约](provider.md#2-确定性-alias-算法) 定义。Provider 返回 unknown、非法或 history-only alias 则由 Agent 返回 `ErrAgentProviderProtocol`，同样不得进入 `ExecuteBatch`。

### 9.3 重试策略

重试次数只由 `tools.default_max_retry` 控制。Manager 使用 `var retryable RetryableError` 和 `errors.As(err, &retryable)` 解包，并且只在 `retryable.Retryable()==true`、caller/callCtx 都仍有效时重试明确标记为暂时性的 transport 错误；权限、查找、参数、取消、timeout 以及已经返回 `ToolResult{IsError:true}` 的业务失败不重试。v1 不提供基于错误字符串的 `retryable_errors`/`non_retryable_errors` 配置。

---
