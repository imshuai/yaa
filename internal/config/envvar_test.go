package config

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestEnvResolverResolve(t *testing.T) {
	resolver := NewEnvResolver()
	t.Setenv("YAA_TEST_VALUE", "configured")
	t.Setenv("YAA_TEST_EMPTY", "")
	t.Setenv("YAA_TEST_RAW", "${YAA_TEST_VALUE}")

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "environment value", value: "before-${YAA_TEST_VALUE}-after", want: "before-configured-after"},
		{name: "empty uses default", value: "${YAA_TEST_EMPTY:-fallback}", want: "fallback"},
		{name: "empty default", value: "${YAA_TEST_EMPTY:-}", want: ""},
		{name: "single expansion", value: "${YAA_TEST_RAW}", want: "${YAA_TEST_VALUE}"},
		{name: "plain value", value: "plain", want: "plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Resolve(tt.value)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestEnvResolverMissingVariable(t *testing.T) {
	const name = "YAA_TEST_REQUIRED_MISSING"
	unsetEnvForTest(t, name)

	got, err := NewEnvResolver().Resolve("${" + name + "}")
	if !errors.Is(err, ErrConfigEnvVarMissing) {
		t.Fatalf("Resolve error = %v, want ErrConfigEnvVarMissing", err)
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("Resolve error = %q, want variable name", err)
	}
	if got != "${"+name+"}" {
		t.Fatalf("Resolve result = %q, want unresolved reference", got)
	}
}

func TestEnvResolverResolveMap(t *testing.T) {
	t.Setenv("YAA_TEST_ADDR", "127.0.0.1:8080")
	t.Setenv("YAA_TEST_TOKEN", "secret")
	raw := map[string]any{
		"runtime": map[string]any{
			"addr":    "${YAA_TEST_ADDR}",
			"enabled": true,
		},
		"tokens": []any{
			map[string]any{"value": "Bearer ${YAA_TEST_TOKEN}"},
			42,
		},
	}
	want := map[string]any{
		"runtime": map[string]any{
			"addr":    "127.0.0.1:8080",
			"enabled": true,
		},
		"tokens": []any{
			map[string]any{"value": "Bearer secret"},
			42,
		},
	}

	if err := NewEnvResolver().ResolveMap(raw); err != nil {
		t.Fatalf("ResolveMap returned error: %v", err)
	}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("ResolveMap result = %#v, want %#v", raw, want)
	}
}

func TestEnvResolverResolveMapReportsPath(t *testing.T) {
	const name = "YAA_TEST_NESTED_MISSING"
	unsetEnvForTest(t, name)
	raw := map[string]any{
		"providers": []any{
			map[string]any{"api_key": "${" + name + "}"},
		},
	}

	err := NewEnvResolver().ResolveMap(raw)
	if !errors.Is(err, ErrConfigEnvVarMissing) {
		t.Fatalf("ResolveMap error = %v, want ErrConfigEnvVarMissing", err)
	}
	for _, part := range []string{"providers", "[0]", "api_key", name} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("ResolveMap error = %q, want path part %q", err, part)
		}
	}
}

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()
	old, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("Unsetenv(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
