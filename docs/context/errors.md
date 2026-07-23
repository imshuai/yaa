# Context 错误处理

> 文档路径: `docs/context/errors.md`
> 上级: [README.md](README.md)

---

## 1. 错误类型

错误 sentinel 位于 Context 包；配置校验错误仍使用 `config.ValidationError`。所有错误都应使用 `%w` 包装原因，供 Agent、Remote API 和日志通过 `errors.Is`/`errors.As` 识别。

```go
var (
    ErrContextBuildFailed       = errors.New("context build failed")
    ErrContextConfigInvalid     = errors.New("context config invalid")
    ErrProviderWindowUnknown    = errors.New("provider model window unknown")
    ErrTokenEstimationFailed    = errors.New("input token estimation failed")
    ErrInvalidMessageSequence   = errors.New("invalid message sequence")
    ErrContextOverflow          = errors.New("context input exceeds budget")
    ErrCompressionFailed        = errors.New("context compression failed")
    ErrCompressionTimeout       = errors.New("context compression timed out")
)
```

| 错误 | 触发条件 | 是否可降级 |
|------|----------|------------|
| `ErrContextConfigInvalid` | 策略、范围或 reserve/output/window 关系非法 | 否，拒绝启动或拒绝该 Agent |
| `ErrProviderWindowUnknown` | 目标 Model 没有正的 `ContextWindow` 或 `MaxOutput` | 否，不能假设无限窗口 |
| `ErrTokenEstimationFailed` | Provider 无法估算完整 `ChatRequest` | 否，不能用字符数近似保证上限 |
| `ErrInvalidMessageSequence` | Tool chain、role 或当前 turn 标记非法 | 否，先修复输入 |
| `ErrContextOverflow` | 受保护输入超限，或没有可删除 unit | 否，返回调用方 |
| `ErrCompressionFailed` | 摘要调用返回空、格式非法或网络错误 | 是，若截断能满足预算 |
| `ErrCompressionTimeout` | 摘要超过 `compression.timeout` | 是，若截断能满足预算 |
| `ErrContextBuildFailed` | 对外包装无法归类的构建错误 | 按 wrapped cause 判断 |

## 2. 降级边界

```text
hybrid + under budget + compression failure -> 原请求成功返回，记录 metadata
hybrid + over budget + compression failure  -> truncate
truncate 无可删除 unit                     -> ErrContextOverflow
reject 超限                                -> ErrContextOverflow
任何 token estimation failure              -> ErrTokenEstimationFailed
```

压缩失败不触发第二个公开构建入口；所有分支都在同一次 `Build` 内完成，且每次变换后重新估算。

## 3. Agent 处理示例

```go
out, err := a.contextManager.Build(ctx, input)
if err != nil {
    switch {
    case errors.Is(err, stdcontext.Canceled), errors.Is(err, stdcontext.DeadlineExceeded):
        return err
    case errors.Is(err, ErrContextOverflow),
        errors.Is(err, ErrContextConfigInvalid),
        errors.Is(err, ErrProviderWindowUnknown),
        errors.Is(err, ErrTokenEstimationFailed),
        errors.Is(err, ErrInvalidMessageSequence):
        return fmt.Errorf("cannot send model request: %w", err)
    default:
        return fmt.Errorf("context build: %w", err)
    }
}
return a.provider.Chat(ctx, &out.Request)
```

`ErrCompressionFailed`/`ErrCompressionTimeout` 在截断成功时只写入 `BuildMetadata` 和指标，不作为成功 Build 的返回错误；截断失败时作为 `ErrContextOverflow` 的 wrapped cause 保留。

## 4. API 映射

Remote API 不把内部错误文本直接暴露给客户端：

| 内部错误 | HTTP 语义 | 建议业务码 |
|----------|-----------|------------|
| 配置/模型窗口未知 | `409` | `40901` |
| 消息序列非法或 Context 超限 | `422` | `42201` |
| Provider Token 估算失败 | `500` | `50001` |
| 调用方取消 | `499` 或连接关闭 | 不生成业务重试 |

具体 envelope 遵循 [Remote API 错误码](../remote-api/INDEX.md#7-错误码)；SSE/WS 使用各自 frame，不套 REST JSON envelope。

---

*最后更新: 2026-07-22*
