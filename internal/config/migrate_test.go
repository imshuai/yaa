package config

import (
	"errors"
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
