package config

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/exp/slog"
)

// ErrConfigMigrationFailed 是 Migrate 失败 (迁移函数返回错误/返回 nil 输入) 的 sentinel.
// 配合 docs/config/checklist.md 错误处理章节, 启动加载迁移失败通过 %w 包此 sentinel.
var ErrConfigMigrationFailed = errors.New("config: migration failed")

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
			return nil, fmt.Errorf("%w: migration %s->%s failed: %w", ErrConfigMigrationFailed, step.From, step.To, err)
		}
		if result == nil {
			return nil, fmt.Errorf("%w: migration %s->%s returned a nil config", ErrConfigMigrationFailed, step.From, step.To)
		}
		current = step.To
	}

	if result == nil {
		return nil, fmt.Errorf("%w: config migration input is nil", ErrConfigMigrationFailed)
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

// MigrateFile 解析给定配置文件, 执行迁移, 可选备份/写回/dry-run.
// docs/config/migration.md §4: --backup 写回, --dry-run 只输出摘要.
// 返回迁移结果 raw map 和写回状态.
func MigrateFile(path string, backup bool, dryRun bool) (map[string]any, error) {
	raw, err := ParseFileToMap(path)
	if err != nil {
		return nil, err
	}
	versionText, _ := raw["config_version"].(string)
	if versionText == "" {
		versionText = CurrentSchemaVersion.String()
	}
	from, err := ParseVersion(versionText)
	if err != nil {
		return nil, fmt.Errorf("parse config_version %q: %w", versionText, err)
	}
	if from.Compare(CurrentSchemaVersion) > 0 {
		return nil, fmt.Errorf("config_version %s is newer than Runtime %s", from, CurrentSchemaVersion)
	}
	if from.Compare(CurrentSchemaVersion) == 0 {
		// 无需迁移
		return raw, nil
	}
	migrated, err := Migrate(raw, from, CurrentSchemaVersion)
	if err != nil {
		return nil, err
	}
	if dryRun {
		// 只返回结果, 不写盘
		return migrated, nil
	}
	if !backup {
		// 不写回
		return migrated, nil
	}
	// 备份原文件
	bakPath := path + ".bak"
	origData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read original for backup: %w", err)
	}
	if err := atomicWriteFile(bakPath, origData, 0o600); err != nil {
		return nil, fmt.Errorf("write backup %s: %w", bakPath, err)
	}
	// 写回迁移后的配置 (保持原格式)
	format, err := DetectFormat(path)
	if err != nil {
		return nil, err
	}
	data, err := MarshalMap(migrated, format)
	if err != nil {
		return nil, err
	}
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write migrated config: %w", err)
	}
	return migrated, nil
}
