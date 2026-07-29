package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateAppliesExplicitChain(t *testing.T) {
	setMigrationsForTest(t, []Migration{
		{
			From: ConfigSchema{Major: 1, Minor: 0},
			To:   ConfigSchema{Major: 1, Minor: 1},
			Run: func(raw map[string]any) (map[string]any, error) {
				raw["first"] = true
				return raw, nil
			},
		},
		{
			From: ConfigSchema{Major: 1, Minor: 1},
			To:   ConfigSchema{Major: 2, Minor: 0},
			Run: func(raw map[string]any) (map[string]any, error) {
				if raw["first"] != true {
					t.Fatal("second migration ran before the first")
				}
				raw["second"] = true
				return raw, nil
			},
		},
	})

	got, err := Migrate(
		map[string]any{"config_version": "1.0"},
		ConfigSchema{Major: 1, Minor: 0},
		ConfigSchema{Major: 2, Minor: 0},
	)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if got["first"] != true || got["second"] != true {
		t.Fatalf("Migrate did not apply every step: %#v", got)
	}
	if got["config_version"] != "2.0" {
		t.Fatalf("config_version = %#v, want %q", got["config_version"], "2.0")
	}
}

func TestMigrateRejectsInvalidGraph(t *testing.T) {
	v10 := ConfigSchema{Major: 1, Minor: 0}
	v11 := ConfigSchema{Major: 1, Minor: 1}

	t.Run("downgrade", func(t *testing.T) {
		setMigrationsForTest(t, nil)
		_, err := Migrate(map[string]any{}, v11, v10)
		assertErrorContains(t, err, "downgrade is not supported")
	})

	t.Run("missing edge", func(t *testing.T) {
		setMigrationsForTest(t, nil)
		_, err := Migrate(map[string]any{}, v10, v11)
		assertErrorContains(t, err, "no migration path")
	})

	t.Run("duplicate source", func(t *testing.T) {
		identity := func(raw map[string]any) (map[string]any, error) { return raw, nil }
		setMigrationsForTest(t, []Migration{
			{From: v10, To: v11, Run: identity},
			{From: v10, To: ConfigSchema{Major: 1, Minor: 2}, Run: identity},
		})
		_, err := Migrate(map[string]any{}, v10, ConfigSchema{Major: 1, Minor: 2})
		assertErrorContains(t, err, "multiple migrations start")
	})

	t.Run("nil runner", func(t *testing.T) {
		setMigrationsForTest(t, []Migration{{From: v10, To: v11}})
		_, err := Migrate(map[string]any{}, v10, v11)
		assertErrorContains(t, err, "no migration path")
	})

	t.Run("nil result", func(t *testing.T) {
		setMigrationsForTest(t, []Migration{{
			From: v10,
			To:   v11,
			Run: func(map[string]any) (map[string]any, error) {
				return nil, nil
			},
		}})
		_, err := Migrate(map[string]any{}, v10, v11)
		assertErrorContains(t, err, "returned a nil config")
	})
}

func TestMigrateWrapsStepFailure(t *testing.T) {
	v10 := ConfigSchema{Major: 1, Minor: 0}
	v11 := ConfigSchema{Major: 1, Minor: 1}
	want := errors.New("broken migration")
	setMigrationsForTest(t, []Migration{
		{From: v10, To: v11, Run: func(map[string]any) (map[string]any, error) {
			return nil, want
		}},
	})

	_, err := Migrate(map[string]any{}, v10, v11)
	if !errors.Is(err, want) {
		t.Fatalf("Migrate error = %v, want wrapped %v", err, want)
	}
}

func TestMigrateCurrentVersion(t *testing.T) {
	setMigrationsForTest(t, nil)
	raw := map[string]any{}
	got, err := Migrate(raw, CurrentSchemaVersion, CurrentSchemaVersion)
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if got["config_version"] != CurrentSchemaVersion.String() {
		t.Fatalf("config_version = %#v, want %q", got["config_version"], CurrentSchemaVersion)
	}
}

func TestMigrateRejectsNilInput(t *testing.T) {
	setMigrationsForTest(t, nil)
	_, err := Migrate(nil, CurrentSchemaVersion, CurrentSchemaVersion)
	assertErrorContains(t, err, "migration input is nil")
}

func setMigrationsForTest(t *testing.T, value []Migration) {
	t.Helper()
	original := migrations
	migrations = value
	t.Cleanup(func() { migrations = original })
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want text %q", err, want)
	}
}

func TestMigrateFailedErrorsIsSentinel(t *testing.T) {
	// Migrate 失败 (迁移函数返错 或 nil 输入) 必须 errors.Is(ErrConfigMigrationFailed).
	// 文本断言只覆盖已存在用例, 此测试显式锁定 sentinel 包装.
	t.Run("nil_input wraps sentinel", func(t *testing.T) {
		setMigrationsForTest(t, nil)
		_, err := Migrate(nil, CurrentSchemaVersion, CurrentSchemaVersion)
		if !errors.Is(err, ErrConfigMigrationFailed) {
			t.Fatalf("errors.Is(ErrConfigMigrationFailed) = false; err = %v", err)
		}
	})
	t.Run("nil_result wraps sentinel", func(t *testing.T) {
		setMigrationsForTest(t, []Migration{
			{From: ConfigSchema{1, 0}, To: ConfigSchema{1, 1}, Run: func(map[string]any) (map[string]any, error) {
				return nil, nil
			}},
		})
		_, err := Migrate(map[string]any{"config_version": "1.0"}, ConfigSchema{1, 0}, ConfigSchema{1, 1})
		if !errors.Is(err, ErrConfigMigrationFailed) {
			t.Fatalf("errors.Is(ErrConfigMigrationFailed) = false; err = %v", err)
		}
	})
}

// TestMigrateFileNoMigrationNeeded 验证 from==to 时 MigrateFile 返回原 raw 不写盘.
func TestMigrateFileNoMigrationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yaa.yaml")
	if err := os.WriteFile(path, []byte("config_version: \"1.0\"\nname: yaa\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := MigrateFile(path, true, false)
	if err != nil {
		t.Fatalf("MigrateFile: %v", err)
	}
	if result["name"] != "yaa" {
		t.Fatalf("expected name=yaa, got %v", result["name"])
	}
	// 备份文件不应存在 (无迁移)
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup should not exist, got err=%v", err)
	}
}

// TestMigrateFileNonExistentSource 验证源文件不存在时返回 error.
func TestMigrateFileNonExistentSource(t *testing.T) {
	dir := t.TempDir()
	_, err := MigrateFile(filepath.Join(dir, "nonexistent.yaml"), false, false)
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

// TestMigrateFileVersionTooNew 验证 config_version 比 CurrentSchemaVersion 高时返回 error.
func TestMigrateFileVersionTooNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yaa.yaml")
	// 假设当前版本是 1.0, 这里用 2.0 触发 "newer than Runtime" 错误
	if err := os.WriteFile(path, []byte("config_version: \"2.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MigrateFile(path, false, false)
	if err == nil {
		t.Fatal("expected error for version too new")
	}
}

// TestMigrateFileDryRunNoWrite 验证 dry-run=true 时不写盘, 不创建备份.
func TestMigrateFileDryRunNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yaa.yaml")
	origData := []byte("config_version: \"1.0\"\nname: yaa\n")
	if err := os.WriteFile(path, origData, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MigrateFile(path, true, true)
	if err != nil {
		t.Fatalf("dry-run MigrateFile: %v", err)
	}
	// 原文件不变
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(origData) {
		t.Fatalf("dry-run modified the file: got %q", data)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("backup should not exist in dry-run")
	}
}
