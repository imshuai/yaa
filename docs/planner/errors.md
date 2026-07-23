# Planner 错误契约

> 上级: [Planner 系统设计](README.md)

---

## 1. Sentinel 与类型

```go
var (
    ErrPlanGenerate  = errors.New("plan generation failed")
    ErrPlanParse     = errors.New("plan response parse failed")
    ErrPlanInvalid   = errors.New("plan invalid")
    ErrPlanExecution = errors.New("plan execution failed")
)

type ValidationError struct {
    StepID string
    Field  string
    Reason string
}

func (e *ValidationError) Error() string
func (e *ValidationError) Unwrap() error { return ErrPlanInvalid }

type ExecutionError struct {
    PlanID string
    StepID string
    Cause  error
}

func (e *ExecutionError) Error() string
func (e *ExecutionError) Unwrap() error { return errors.Join(ErrPlanExecution, e.Cause) }
```

Go 目标为 1.20，因此多 cause 可使用 `errors.Join`。错误字符串不得包含完整 prompt、Tool 参数、Provider body 或 Step output。

---

## 2. 映射

| 阶段 | 对外错误 | Cause |
|------|----------|-------|
| Provider 调用失败 | `ErrPlanGenerate` | Provider 原始分类错误 |
| 规划 context 超时/取消 | `ErrPlanGenerate` | `context.DeadlineExceeded` / `context.Canceled` |
| JSON 解码失败 | `ErrPlanParse` | `json` 错误 |
| DAG、能力或输入引用非法 | `*ValidationError` | `ErrPlanInvalid` |
| StepRunner 失败 | `*ExecutionError` | `ErrPlanExecution` + 下层错误 |
| turn context 取消 | 原样保留 `context` cause | `context` 错误 |

`PlanResult` 与 error 一并返回，供日志记录已经完成的 Step；它不表示可以恢复。

---

## 3. 重试和降级

- Planner 自己不重试 Provider 请求，避免与 Provider Manager 的有限重试叠加。
- JSON 或 DAG 非法不重新请求模型；本轮直接失败。
- Executor 不重试 Step。
- `planner.type: disabled` 是启动时选择的直接 Agent Loop，不是运行错误后的降级路径。
- Remote 层把这些错误投影为现有 conversation 终态和统一 envelope，不新增 Planner 业务码。
