package planner

// ValidatePlan 在启动任何 goroutine 或外部调用前完整校验 Plan, 一次收集或返回首个确定性错误.
// docs/planner/execution.md §1 的 8 条铁律:
//  1. 可信输入 TurnID / AgentID / Task / Model 非空, MaxSteps > 0, Tool capability 名称唯一
//  2. Plan.ID == in.TurnID + ":plan"; Plan.Task == in.Task; 1 <= len(Steps) <= in.MaxSteps
//  3. Step ID 非空且全局唯一
//  4. Action 只能 tool / llm
//  5. tool: Target 非空且属于 in.Capabilities; llm: Target 必空
//  6. Depends 不重复 / 不自依赖 / 不引用不存在 Step (无须要求依赖在数组前方)
//  7. Kahn 拓扑: 整个图无环
//  8. Input 中的 $step 引用只能指向该 Step 的直接依赖
//
// 错误返回 *ValidationError (Unwrap → ErrPlanInvalid). 不收集全部错误, 返回首个确定性错误
// (docs/planner/errors.md 与 execution.md §1 "一次收集或返回首个确定性错误").
func ValidatePlan(plan Plan, in PlanningInput) error {
	// 规则 1: 可信输入字段 + capability 唯一.
	if in.TurnID == "" {
		return &ValidationError{Field: "turn_id", Reason: "empty"}
	}
	if in.AgentID == "" {
		return &ValidationError{Field: "agent_id", Reason: "empty"}
	}
	if in.Task == "" {
		return &ValidationError{Field: "task", Reason: "empty"}
	}
	if in.Model == "" {
		return &ValidationError{Field: "model", Reason: "empty"}
	}
	if in.MaxSteps <= 0 {
		return &ValidationError{Field: "max_steps", Reason: "must be > 0"}
	}
	capNames := make(map[string]struct{}, len(in.Capabilities))
	for i, c := range in.Capabilities {
		if c.Name == "" {
			return &ValidationError{Field: "capabilities", Reason: "empty name at index " + itoa(i)}
		}
		if _, dup := capNames[c.Name]; dup {
			return &ValidationError{Field: "capabilities", Reason: "duplicate name " + c.Name}
		}
		capNames[c.Name] = struct{}{}
	}

	// 规则 2: Plan.ID / Plan.Task / Steps 长度.
	wantID := in.TurnID + ":plan"
	if plan.ID != wantID {
		return &ValidationError{Field: "id", Reason: "must equal turn_id + \":plan\""}
	}
	if plan.Task != in.Task {
		return &ValidationError{Field: "task", Reason: "must equal planning input task"}
	}
	if len(plan.Steps) == 0 {
		return &ValidationError{Field: "steps", Reason: "empty plan"}
	}
	if len(plan.Steps) > in.MaxSteps {
		return &ValidationError{Field: "steps", Reason: "exceeds max_steps"}
	}

	// 规则 3: Step ID 非空 + 全局唯一; 同时收集 idSet 供规则 6/7 引用检查.
	stepIDs := make(map[string]int, len(plan.Steps))
	for i, s := range plan.Steps {
		if s.ID == "" {
			return &ValidationError{Field: "id", Reason: "empty step id at index " + itoa(i)}
		}
		if _, dup := stepIDs[s.ID]; dup {
			return &ValidationError{StepID: s.ID, Field: "id", Reason: "duplicate step id"}
		}
		stepIDs[s.ID] = i
	}

	// 规则 4 / 5: Action 取值 + Target 约束.
	for _, s := range plan.Steps {
		switch s.Action {
		case ActionTool:
			if s.Target == "" {
				return &ValidationError{StepID: s.ID, Field: "target", Reason: "tool step requires target"}
			}
			if _, ok := capNames[s.Target]; !ok {
				return &ValidationError{StepID: s.ID, Field: "target", Reason: "tool " + s.Target + " not in capabilities"}
			}
		case ActionLLM:
			if s.Target != "" {
				return &ValidationError{StepID: s.ID, Field: "target", Reason: "llm step must not set target"}
			}
		default:
			return &ValidationError{StepID: s.ID, Field: "action", Reason: "must be tool or llm"}
		}
	}

	// 规则 6: Depends 不重复 / 不自依赖 / 引用存在. 同时构建入度 + 邻接 (规则 7 Kahn 用).
	adjacency := make(map[string][]string, len(plan.Steps))
	inDeg := make(map[string]int, len(plan.Steps))
	for _, s := range plan.Steps {
		inDeg[s.ID] = 0
	}
	for _, s := range plan.Steps {
		seen := make(map[string]struct{}, len(s.Depends))
		for _, d := range s.Depends {
			if d == s.ID {
				return &ValidationError{StepID: s.ID, Field: "depends", Reason: "self-dependency"}
			}
			if _, dup := seen[d]; dup {
				return &ValidationError{StepID: s.ID, Field: "depends", Reason: "duplicate dependency " + d}
			}
			seen[d] = struct{}{}
			if _, exist := stepIDs[d]; !exist {
				return &ValidationError{StepID: s.ID, Field: "depends", Reason: "unknown dependency " + d}
			}
			adjacency[d] = append(adjacency[d], s.ID)
			inDeg[s.ID]++
		}
	}

	// 规则 7: Kahn 算法检查整图无环 (docs §1 "Kahn 算法拒绝所有环").
	// 用 slice 作为前置节点队列按 Plan.Steps 数组顺序稳定弹出 (确定性的拓扑顺序; 仅用于环检测, 不要求依赖序).
	queue := make([]string, 0, len(plan.Steps))
	for _, s := range plan.Steps {
		if inDeg[s.ID] == 0 {
			queue = append(queue, s.ID)
		}
	}
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, nxt := range adjacency[cur] {
			inDeg[nxt]--
			if inDeg[nxt] == 0 {
				queue = append(queue, nxt)
			}
		}
	}
	if visited != len(plan.Steps) {
		return &ValidationError{Field: "cycle", Reason: "plan has cycle"}
	}

	// 规则 8: Input 中的 $step 引用只能指向该 Step 的直接依赖.
	// 含 $step 的 object 只允许同时含 $step 与可选 key 两字段 (docs/execution.md §2).
	for _, s := range plan.Steps {
		dependsSet := make(map[string]struct{}, len(s.Depends))
		for _, d := range s.Depends {
			dependsSet[d] = struct{}{}
		}
		if err := validateStepInputRefs(s.ID, s.Input, dependsSet); err != nil {
			return err
		}
	}

	return nil
}

// validateStepInputRefs 递归复制 Step.Input 后替换 $step 引用.
// 引用 object 必须只含 $step + 可选 key; $step 值必须属于当前 Step 的直接依赖集合.
// docs/planner/execution.md §2.
func validateStepInputRefs(stepID string, v any, depends map[string]struct{}) *ValidationError {
	switch t := v.(type) {
	case map[string]any:
		if stepRef, hasStep := t["$step"].(string); hasStep {
			// 是 $step 引用 object: 必须为 { $step, 可选 key } 两字段.
			allowed := 0
			if _, ok := t["key"]; ok {
				allowed++
			}
			if len(t) != 1+allowed {
				return &ValidationError{StepID: stepID, Field: "input", Reason: "$step reference object may contain only $step and optional key"}
			}
			if _, ok := depends[stepRef]; !ok {
				return &ValidationError{StepID: stepID, Field: "input", Reason: "$step reference " + stepRef + " not in depends"}
			}
		}
		// 递归每个值.
		for _, sub := range t {
			if err := validateStepInputRefs(stepID, sub, depends); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range t {
			if err := validateStepInputRefs(stepID, item, depends); err != nil {
				return err
			}
		}
	}
	return nil
}

// itoa 简单整数转十进制 (避免 strconv 引入; 仅用于错误信息索引).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
