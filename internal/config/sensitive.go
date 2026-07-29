// sensitive.go: 敏感字段强制环境变量来源校验. docs/config/envvar.md §5.
package config

import (
	"errors"
	"fmt"
	"strings"
)

// ErrConfigSensitivePlain 验证敏感字段未使用 ${VAR} 注入.
var ErrConfigSensitivePlain = errors.New("config: sensitive field must use ${VAR} environment reference")

// sensitiveFieldPaths 是必须通过环境变量注入的字段路径列表.
// ponytail: 用一组最小路径, 不引入动态 schema 注册.
var sensitiveFieldPaths = []string{
	"providers.api_key", // providers[].api_key — [] 是数组下标通配
	"auth.jwt.secret",
}

// validateSensitiveSources 在 envvar 展开前校验 raw map: 敏感字段值必须为空或 ${...} 引用.
// docs config checklist 行16: 敏感字段不在配置文件中明文存储.
func validateSensitiveSources(raw map[string]any) error {
	var errs []error
	// providers[].api_key
	if providers, ok := raw["providers"].([]any); ok {
		for i, p := range providers {
			if pm, ok := p.(map[string]any); ok {
				if v, ok := pm["api_key"].(string); ok && v != "" && !isEnvRef(v) {
					errs = append(errs, fmt.Errorf("%w: providers[%d].api_key", ErrConfigSensitivePlain, i))
				}
			}
		}
	}
	// auth.jwt.secret
	if auth, ok := raw["auth"].(map[string]any); ok {
		if jwt, ok := auth["jwt"].(map[string]any); ok {
			if v, ok := jwt["secret"].(string); ok && v != "" && !isEnvRef(v) {
				errs = append(errs, fmt.Errorf("%w: auth.jwt.secret", ErrConfigSensitivePlain))
			}
		}
	}
	return errors.Join(errs...)
}

// isEnvRef 判断 s 是否完全匹配 ${VAR_NAME} 或 ${VAR_NAME:-default}.
func isEnvRef(s string) bool {
	if !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return false
	}
	inner := s[2 : len(s)-1]
	if inner == "" {
		return false
	}
	// VAR_NAME 或 VAR_NAME:-default
	if idx := strings.Index(inner, ":-"); idx >= 0 {
		inner = inner[:idx]
	}
	// 校验变量名: 字母数字下划线, 首字母不能数字
	if inner == "" {
		return false
	}
	for i, r := range inner {
		if r == '_' {
			continue
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// ErrConfigHotReloadFailed 是热更新流程失败的 sentinel. docs/config/checklist.md 行114.
// Phase 5 ReloadManager 引用.
var ErrConfigHotReloadFailed = errors.New("config: hot reload failed")

// ErrConfigNotActive 是 ReloadManager 未 Activate 时被访问的 sentinel. docs/config/checklist.md 行115.
// Phase 5 ReloadManager 引用.
var ErrConfigNotActive = errors.New("config: not active")
