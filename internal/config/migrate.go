package config

import (
	"fmt"

	"golang.org/x/exp/slog"
)

// MigrationFunc upgrades a presence-aware raw configuration map.
type MigrationFunc func(map[string]any) (map[string]any, error)

// Migration is one explicit edge in the configuration migration graph.
type Migration struct {
	From ConfigSchema
	To   ConfigSchema
	Run  MigrationFunc
}

var migrations = []Migration{}

// Migrate applies registered edges from from to to in order.
func Migrate(raw map[string]any, from, to ConfigSchema) (map[string]any, error) {
	if from.Compare(to) > 0 {
		return nil, fmt.Errorf("config downgrade is not supported: %s -> %s", from, to)
	}

	result := raw
	current := from
	for current.Compare(to) < 0 {
		var step *Migration
		for i := range migrations {
			candidate := &migrations[i]
			if candidate.From.Compare(current) != 0 {
				continue
			}
			if step != nil {
				return nil, fmt.Errorf("multiple migrations start at %s", current)
			}
			step = candidate
		}

		if step == nil || step.Run == nil || step.To.Compare(current) <= 0 || step.To.Compare(to) > 0 {
			return nil, fmt.Errorf("no migration path from %s to %s", current, to)
		}

		var err error
		result, err = step.Run(result)
		if err != nil {
			return nil, fmt.Errorf("migration %s->%s failed: %w", step.From, step.To, err)
		}
		if result == nil {
			return nil, fmt.Errorf("migration %s->%s returned a nil config", step.From, step.To)
		}
		current = step.To
	}

	if result == nil {
		return nil, fmt.Errorf("config migration input is nil")
	}
	result["config_version"] = to.String()
	return result, nil
}

// migrateRaw 解析 config_version 并按显式迁移边将 raw Map 升级到 CurrentSchemaVersion。
// 缺失 config_version 时按当前版本处理（不迁移）。downgrade 或无法识别的版本直接报错。
func migrateRaw(raw map[string]any, logger *slog.Logger) (map[string]any, error) {
	versionText, ok := raw["config_version"].(string)
	if !ok || versionText == "" {
		versionText = CurrentSchemaVersion.String()
		raw["config_version"] = versionText
	}
	from, err := ParseVersion(versionText)
	if err != nil {
		return nil, fmt.Errorf("parse config_version %q: %w", versionText, err)
	}
	cmp := from.Compare(CurrentSchemaVersion)
	if cmp > 0 {
		return nil, fmt.Errorf("config_version %s is newer than Runtime %s", from, CurrentSchemaVersion)
	}
	if cmp == 0 {
		return raw, nil
	}

	migrated, err := Migrate(raw, from, CurrentSchemaVersion)
	if err != nil {
		return nil, err
	}
	logger.Info("config migrated", "from", from.String(), "to", CurrentSchemaVersion.String())
	return migrated, nil
}
