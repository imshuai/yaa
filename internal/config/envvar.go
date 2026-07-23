package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
)

// ErrConfigEnvVarMissing identifies a required environment variable that is empty or unset.
var ErrConfigEnvVarMissing = errors.New("envvar: required variable")

var envPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(:-([^}]*))?\}`)

// EnvResolver expands environment references in raw configuration values.
type EnvResolver struct{}

func NewEnvResolver() *EnvResolver {
	return &EnvResolver{}
}

// Resolve expands every environment reference in value once.
func (r *EnvResolver) Resolve(value string) (string, error) {
	var resolveErr error
	result := envPattern.ReplaceAllStringFunc(value, func(match string) string {
		subs := envPattern.FindStringSubmatch(match)
		name := subs[1]
		if envValue, exists := os.LookupEnv(name); exists && envValue != "" {
			return envValue
		}
		if subs[2] != "" {
			return subs[3]
		}
		if resolveErr == nil {
			resolveErr = fmt.Errorf("%w %s is not set", ErrConfigEnvVarMissing, name)
		}
		return match
	})
	return result, resolveErr
}

// ResolveMap recursively expands string values in m in place.
func (r *EnvResolver) ResolveMap(m map[string]any) error {
	_, err := r.resolveValue(m)
	return err
}

func (r *EnvResolver) resolveValue(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return r.Resolve(typed)
	case map[string]any:
		for key, item := range typed {
			resolved, err := r.resolveValue(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			typed[key] = resolved
		}
	case []any:
		for i, item := range typed {
			resolved, err := r.resolveValue(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			typed[i] = resolved
		}
	}
	return value, nil
}
