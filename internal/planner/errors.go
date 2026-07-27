package planner

import (
	"errors"
	"fmt"
)

// Sentinel 与 **验证 / 执行** 错误类型 (docs/planner/errors.md §1).
//
// 映射 (docs §2):
//   - Provider 调用失败 / 规划 ctx 超时取消 → ErrPlanGenerate
//   - JSON 解码失败 → ErrPlanParse
//   - DAG / 能力 / 输入引用非法 → *ValidationError (Unwrap → ErrPlanInvalid)
//   - StepRunner 失败 → *ExecutionError (Unwrap → errors.Join(ErrPlanExecution, Cause))
//   - turn ctx 取消 → 原样保留 context.Cause
//
// 错误字符串不得包含完整 prompt / Tool 参数 / Provider body / Step output (docs §1).
var (
	ErrPlanGenerate  = errors.New("plan generation failed")
	ErrPlanParse     = errors.New("plan response parse failed")
	ErrPlanInvalid   = errors.New("plan invalid")
	ErrPlanExecution = errors.New("plan execution failed")
)

// ValidationError 描述 DAG / 能力 / 输入引用的确定性错误.
// Unwrap 返回 ErrPlanInvalid, 供 errors.Is(err, ErrPlanInvalid) 判别.
type ValidationError struct {
	StepID string // 可空: 全局校验错误 (Plan.ID 不一致等) 时为 ""
	Field  string // 校验维度: id / task / steps / action / target / depends / cycle / input / capabilities
	Reason string // 简短描述, 不包含敏感载荷
}

func (e *ValidationError) Error() string {
	if e.StepID == "" {
		return fmt.Sprintf("plan invalid: %s: %s", e.Field, e.Reason)
	}
	return fmt.Sprintf("plan invalid: step %q: %s: %s", e.StepID, e.Field, e.Reason)
}

func (e *ValidationError) Unwrap() error { return ErrPlanInvalid }

// ExecutionError 描述执行期单 Step 失败 (docs/prog/errors.md §1).
type ExecutionError struct {
	PlanID string
	StepID string
	Cause  error
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("plan execution failed: plan=%s step=%s: %v", e.PlanID, e.StepID, e.Cause)
}

func (e *ExecutionError) Unwrap() error {
	// Go 1.20 errors.Join 支持 (docs §1).
	return errors.Join(ErrPlanExecution, e.Cause)
}
