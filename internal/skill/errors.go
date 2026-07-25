// Package skill 实现启动期加载的领域 Prompt 包管理。
//
// v1 边界（docs/skill/README.md §1）：
//   - 启动期从 skills.dir 加载，成功后 Manager 不可变、并发只读无需运行时锁；
//   - 不实现运行时 install/uninstall/enable/disable/reload/Registry；
//   - 不调用 Provider/Tool/Session，自身不保存对话状态；Agent 把 ResolvedSkill
//     渲染成 protected system message 注入候选 Provider 请求。
package skill

import "errors"

// 稳定 sentinel，调用方用 errors.Is 分类，不解析字符串（docs/skill/errors.md §1）。
var (
	ErrSkillDirectoryUnavailable = errors.New("skill: directory unavailable")
	ErrSkillNotFound             = errors.New("skill: not found")
	ErrSkillInvalid              = errors.New("skill: invalid package")
	ErrSkillDuplicate            = errors.New("skill: duplicate name")
	ErrSkillDependencyMissing    = errors.New("skill: dependency missing")
	ErrSkillDependencyCycle      = errors.New("skill: dependency cycle")
	ErrSkillDisabled             = errors.New("skill: disabled")
	ErrSkillToolUnavailable      = errors.New("skill: tool unavailable")
	ErrSkillPermissionDenied     = errors.New("skill: dependency not allowed")
	ErrSkillAgentNotFound        = errors.New("skill: agent not found")
	ErrSkillOptionsInvalid       = errors.New("skill: invalid options")
)
