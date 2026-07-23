package config

import (
	"reflect"
	"testing"
)

func TestConfigDTOContract(t *testing.T) {
	root := reflect.TypeOf(Config{})
	wantRoot := []string{
		"config_version", "runtime", "agents", "providers", "mcp", "tools",
		"skills", "memory", "session", "context", "planner", "plugins", "log",
	}
	if root.NumField() != len(wantRoot) {
		t.Fatalf("Config has %d fields, want %d", root.NumField(), len(wantRoot))
	}
	for i, want := range wantRoot {
		field := root.Field(i)
		if got := field.Tag.Get("yaml"); got != want {
			t.Errorf("Config.%s yaml tag = %q, want %q", field.Name, got, want)
		}
	}

	agent := reflect.TypeOf(AgentConfig{})
	for fieldName, wantType := range map[string]reflect.Type{
		"Memory":  reflect.TypeOf((*MemoryOverride)(nil)),
		"Session": reflect.TypeOf((*SessionOverride)(nil)),
		"Context": reflect.TypeOf((*ContextOverride)(nil)),
		"Planner": reflect.TypeOf((*PlannerOverride)(nil)),
	} {
		field, ok := agent.FieldByName(fieldName)
		if !ok || field.Type != wantType {
			t.Errorf("AgentConfig.%s type = %v, want %v", fieldName, field.Type, wantType)
		}
	}

	assertMatchingConfigTags(t, root, map[reflect.Type]bool{})
}

func assertMatchingConfigTags(t *testing.T, typ reflect.Type, visited map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ.PkgPath() != reflect.TypeOf(Config{}).PkgPath() || visited[typ] {
		return
	}
	visited[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		yamlTag := field.Tag.Get("yaml")
		jsonTag := field.Tag.Get("json")
		if yamlTag == "" || jsonTag == "" || yamlTag != jsonTag {
			t.Errorf("%s.%s tags yaml=%q json=%q, want matching non-empty tags", typ.Name(), field.Name, yamlTag, jsonTag)
		}
		assertMatchingConfigTags(t, field.Type, visited)
	}
}
