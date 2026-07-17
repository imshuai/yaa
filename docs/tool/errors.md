# 错误处理

> 文档路径: docs/tool/errors.md
> 上级: README.md 9

---

## 9. 错误处理

### 9.1 错误分类

| 错误类型 | 说明 | 处理方式 |
|----------|------|---------|
| `ErrToolNotFound` | Tool 未注册 | 返回给 LLM，LLM 可能换一种方式 |
| `ErrToolDisabled` | Tool 已禁用 | 同上 |
| `ErrPermissionDenied` | Agent 无权限 | 同上 |
| `ErrInvalidParams` | 参数校验失败 | 返回校验详情给 LLM |
| `ErrToolTimeout` | 执行超时 | 返回超时信息给 LLM |
| `ErrToolCanceled` | 执行被取消 | 不加入 Context，终止 Tool Loop |
| Tool 返回的 `IsError` | Tool 内部逻辑错误 | 返回给 LLM，LLM 可调整策略 |
| Tool 返回的 `error` | 执行异常 | 按重试策略处理，重试失败后返回给 LLM |

### 9.2 错误传递给 LLM

Tool 执行失败时，错误信息以 `role="tool"` 消息的形式返回给 LLM：

```go
func errorToToolMessage(callID string, err error) provider.Message {
    return provider.Message{
        Role:       "tool",
        Content:    fmt.Sprintf("Error: %v", err),
        ToolCallID: callID,
    }
}
```

**设计意图：** 让 LLM 知道 Tool 执行失败了，使其有机会自行调整策略（如换参数重试、换一个 Tool、或告知用户无法完成）。

### 9.3 重试策略

```yaml
tools:
  default_max_retry: 1         # 默认重试次数
  retryable_errors:
    - "timeout"
    - "connection refused"
    - "502"
    - "503"
  non_retryable_errors:
    - "permission denied"
    - "not found"
    - "invalid params"
```

重试由 Tool Manager 层面处理，Tool 实现本身不需要处理重试逻辑。

---

