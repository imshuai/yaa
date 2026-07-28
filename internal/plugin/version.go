// Package plugin 版本校验: requires_runtime 和 dependency version range.
// docs/plugin/config-ref.md §2 + checklist.md 行17: SemVer parser.
package plugin

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// versionConstraint 是单个比较项, 如 ">=" + "1.0.0".
type versionConstraint struct {
	op  string // ">=", "<=", ">", "<", "=", "=="
	ver string // canonical semver
}

// parseVersionRange 解析空格分隔的比较列表, 如 ">=0.1.0 <1.0.0".
// 空 range 表示无约束 (返回 nil constraint = 始终满足).
func parseVersionRange(rangeStr string) ([]versionConstraint, error) {
	rangeStr = strings.TrimSpace(rangeStr)
	if rangeStr == "" {
		return nil, nil
	}
	parts := strings.Fields(rangeStr)
	var constraints []versionConstraint
	for _, part := range parts {
		c, err := parseSingleConstraint(part)
		if err != nil {
			return nil, err
		}
		constraints = append(constraints, c)
	}
	return constraints, nil
}

// parseSingleConstraint 解析单个比较项.
func parseSingleConstraint(s string) (versionConstraint, error) {
	// 按前缀匹配最长操作符
	for _, op := range []string{">=", "<=", "==", ">", "<", "="} {
		if strings.HasPrefix(s, op) {
			ver := strings.TrimSpace(s[len(op):])
			if !semver.IsValid(canonicalSemVer(ver)) {
				return versionConstraint{}, fmt.Errorf("invalid version %q in constraint %q", ver, s)
			}
			return versionConstraint{op: op, ver: canonicalSemVer(ver)}, nil
		}
	}
	// 无操作符前缀 → 精确等
	if !semver.IsValid(canonicalSemVer(s)) {
		return versionConstraint{}, fmt.Errorf("invalid version %q", s)
	}
	return versionConstraint{op: "=", ver: canonicalSemVer(s)}, nil
}

// canonicalSemVer 确保版本号有 v 前缀 (semver 包要求).
func canonicalSemVer(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// versionInRange 校验给定版本是否满足 range.
// 空 constraints (nil) 表示无约束, 始终返回 true.
func versionInRange(version string, constraints []versionConstraint) bool {
	if len(constraints) == 0 {
		return true
	}
	ver := canonicalSemVer(version)
	if !semver.IsValid(ver) {
		return false
	}
	for _, c := range constraints {
		if !satisfiesConstraint(ver, c) {
			return false
		}
	}
	return true
}

// satisfiesConstraint 校验单个比较项.
func satisfiesConstraint(ver string, c versionConstraint) bool {
	cmp := semver.Compare(ver, c.ver)
	switch c.op {
	case ">=":
		return cmp >= 0
	case "<=":
		return cmp <= 0
	case ">", "<":
		if c.op == ">" {
			return cmp > 0
		}
		return cmp < 0
	case "=", "==":
		return cmp == 0
	}
	return false
}

// ValidateRuntimeVersion 校验 requires_runtime SemVer range 对当前 Runtime 兼容.
// docs/plugin/config-ref.md §2: requires_runtime 如 ">=0.1.0 <1.0.0".
func ValidateRuntimeVersion(m Manifest, runtimeVersion string) error {
	if m.RequiresRuntime == "" {
		return nil // 无约束
	}
	constraints, err := parseVersionRange(m.RequiresRuntime)
	if err != nil {
		return fmt.Errorf("%w: %s requires_runtime %q: %v",
			ErrPluginRuntimeIncompatible, m.ID, m.RequiresRuntime, err)
	}
	if !versionInRange(runtimeVersion, constraints) {
		return fmt.Errorf("%w: %s requires_runtime %q but runtime is %s",
			ErrPluginRuntimeIncompatible, m.ID, m.RequiresRuntime, runtimeVersion)
	}
	return nil
}

// ValidateDependencies 校验 dependencies 的 version 是有效 SemVer range 格式.
// docs/plugin/config-ref.md §2: dependency version 如 ">=1.0.0 <2.0.0".
// 注意: 依赖图解析和缺失/循环校验在 Manager 阶段执行 (需要全部 descriptor).
func ValidateDependencies(m Manifest) error {
	for i, dep := range m.Dependencies {
		if _, err := parseVersionRange(dep.Version); err != nil {
			return fmt.Errorf("%w: %s dependency[%d] %s version %q: %v",
				ErrPluginManifestInvalid, m.ID, i, dep.ID, dep.Version, err)
		}
	}
	return nil
}
