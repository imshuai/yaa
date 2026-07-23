package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeIntoPreservesAbsentFieldsAndReplacesPresentSlices(t *testing.T) {
	cfg := Default()
	raw := map[string]any{
		"runtime": map[string]any{
			"auth": map[string]any{
				"public_paths": []any{"/custom"},
			},
		},
		"plugins": map[string]any{
			"paths": []any{"./custom-plugins"},
		},
	}

	if err := DecodeInto(raw, cfg); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	if !reflect.DeepEqual(cfg.Runtime.Auth.PublicPaths, []string{"/custom"}) {
		t.Fatalf("public_paths = %#v, want only source slice", cfg.Runtime.Auth.PublicPaths)
	}
	if !reflect.DeepEqual(cfg.Plugins.Paths, []string{"./custom-plugins"}) {
		t.Fatalf("plugins.paths = %#v, want only source slice", cfg.Plugins.Paths)
	}
	if cfg.Runtime.API.HTTP.Addr != "127.0.0.1:8080" {
		t.Fatalf("absent HTTP addr = %q, want canonical default", cfg.Runtime.API.HTTP.Addr)
	}
}

func TestDecodeIntoConvertsDurationsAndJSONNumbers(t *testing.T) {
	cfg := Default()
	raw := map[string]any{
		"runtime": map[string]any{
			"api": map[string]any{
				"http": map[string]any{
					"read_timeout":     "45s",
					"max_header_bytes": json.Number("2048"),
				},
			},
		},
		"tools": map[string]any{
			"default_timeout": "90s",
		},
	}

	if err := DecodeInto(raw, cfg); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	if cfg.Runtime.API.HTTP.ReadTimeout != 45*time.Second {
		t.Fatalf("read_timeout = %v, want 45s", cfg.Runtime.API.HTTP.ReadTimeout)
	}
	if cfg.Runtime.API.HTTP.MaxHeaderBytes != 2048 {
		t.Fatalf("max_header_bytes = %d, want 2048", cfg.Runtime.API.HTTP.MaxHeaderBytes)
	}
	if cfg.Tools.DefaultTimeout != 90*time.Second {
		t.Fatalf("default_timeout = %v, want 90s", cfg.Tools.DefaultTimeout)
	}
}

func TestDecodeIntoClearsNullableValues(t *testing.T) {
	cfg := Default()
	if cfg.Planner.Temperature == nil {
		t.Fatal("Default planner temperature is unexpectedly nil")
	}
	raw := map[string]any{
		"planner": map[string]any{
			"temperature": nil,
		},
		"tools": map[string]any{
			"builtin": map[string]any{
				"shell": map[string]any{
					"options": nil,
				},
			},
		},
	}
	if err := ApplyElementDefaults(raw); err != nil {
		t.Fatalf("ApplyElementDefaults returned error: %v", err)
	}
	if err := DecodeInto(raw, cfg); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	if cfg.Planner.Temperature != nil {
		t.Fatalf("planner.temperature = %v, want nil", *cfg.Planner.Temperature)
	}
	if cfg.Tools.Builtin["shell"].Options != nil {
		t.Fatalf("shell.options = %#v, want nil", cfg.Tools.Builtin["shell"].Options)
	}
}

func TestDecodeIntoRejectsUnknownAndNonNullableNullWithFullPaths(t *testing.T) {
	cfg := Default()
	raw := map[string]any{
		"runtime": map[string]any{
			"api": map[string]any{
				"http": map[string]any{
					"addr": nil,
					"typo": true,
				},
			},
		},
	}

	err := DecodeInto(raw, cfg)
	if err == nil {
		t.Fatal("DecodeInto returned nil, want path errors")
	}
	message := err.Error()
	for _, path := range []string{"runtime.api.http.addr", "runtime.api.http.typo"} {
		if !strings.Contains(message, path) {
			t.Errorf("DecodeInto error %q does not contain full path %q", message, path)
		}
	}
}

func TestDecodeIntoLeavesDestinationUntouchedOnPreflightError(t *testing.T) {
	cfg := Default()
	defaultPaths := append([]string(nil), cfg.Runtime.Auth.PublicPaths...)
	defaultAddr := cfg.Runtime.API.HTTP.Addr
	raw := map[string]any{
		"runtime": map[string]any{
			"auth": map[string]any{"public_paths": []any{"/changed"}},
			"api":  map[string]any{"http": map[string]any{"addr": nil, "bogus": true}},
		},
	}
	if err := DecodeInto(raw, cfg); err == nil {
		t.Fatal("DecodeInto returned nil, want preflight error")
	}
	if !reflect.DeepEqual(cfg.Runtime.Auth.PublicPaths, defaultPaths) {
		t.Fatalf("public_paths changed after failed decode: %#v", cfg.Runtime.Auth.PublicPaths)
	}
	if cfg.Runtime.API.HTTP.Addr != defaultAddr {
		t.Fatalf("addr changed after failed decode: %q", cfg.Runtime.API.HTTP.Addr)
	}
}

func TestDecodeIntoRejectsNilDestination(t *testing.T) {
	if err := DecodeInto(map[string]any{}, nil); err == nil {
		t.Fatal("DecodeInto returned nil for nil destination")
	}
}

func TestDecodeIntoAcceptsOnlyZeroNumericDuration(t *testing.T) {
	cfg := Default()
	if err := DecodeInto(map[string]any{
		"mcp": map[string]any{
			"timeout": map[string]any{"tool": json.Number("0")},
		},
	}, cfg); err != nil {
		t.Fatalf("DecodeInto numeric zero duration returned error: %v", err)
	}
	if cfg.MCP.Timeout.Tool != 0 {
		t.Fatalf("mcp.timeout.tool = %v, want 0", cfg.MCP.Timeout.Tool)
	}

	err := DecodeInto(map[string]any{
		"mcp": map[string]any{
			"timeout": map[string]any{"tool": json.Number("1")},
		},
	}, Default())
	if err == nil || !strings.Contains(err.Error(), "mcp.timeout.tool") {
		t.Fatalf("DecodeInto non-zero numeric duration error = %v, want full path", err)
	}
}

func TestDecodeIntoRejectsImplicitPrimitiveConversions(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
		path string
	}{
		{
			name: "json number to string",
			raw:  map[string]any{"config_version": json.Number("1")},
			path: "config_version",
		},
		{
			name: "float to int",
			raw: map[string]any{
				"runtime": map[string]any{
					"api": map[string]any{
						"http": map[string]any{"max_header_bytes": 1.5},
					},
				},
			},
			path: "runtime.api.http.max_header_bytes",
		},
		{
			name: "case mismatch",
			raw:  map[string]any{"Runtime": map[string]any{}},
			path: "Runtime",
		},
		{
			name: "integer overflow",
			raw:  map[string]any{"runtime": map[string]any{"api": map[string]any{"http": map[string]any{"max_header_bytes": json.Number("9223372036854775808")}}}},
			path: "runtime.api.http.max_header_bytes",
		},
		{
			name: "unknown field in typed map value",
			raw: map[string]any{
				"skills": map[string]any{
					"per_skill": map[string]any{
						"review": map[string]any{"bogus": true},
					},
				},
			},
			path: "skills.per_skill.review.bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecodeInto(tt.raw, Default())
			if err == nil || !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("DecodeInto error = %v, want path %q", err, tt.path)
			}
		})
	}
}

func TestDecodeIntoConvertsStringScalars(t *testing.T) {
	cfg := Default()
	raw := map[string]any{
		"runtime": map[string]any{
			"api": map[string]any{
				"http": map[string]any{"max_header_bytes": "2048"},
				"ws":   map[string]any{"enabled": "false"},
			},
		},
		"planner": map[string]any{"temperature": "0.5"},
	}
	if err := DecodeInto(raw, cfg); err != nil {
		t.Fatalf("DecodeInto string scalar conversion returned error: %v", err)
	}
	if cfg.Runtime.API.HTTP.MaxHeaderBytes != 2048 {
		t.Fatalf("max_header_bytes = %d, want 2048", cfg.Runtime.API.HTTP.MaxHeaderBytes)
	}
	if cfg.Runtime.API.WS.Enabled {
		t.Fatal("ws.enabled = true, want false")
	}
	if cfg.Planner.Temperature == nil || *cfg.Planner.Temperature != 0.5 {
		t.Fatalf("planner.temperature = %v, want 0.5", cfg.Planner.Temperature)
	}
}

func TestDecodeIntoPreservesPlannerOverridePresence(t *testing.T) {
	raw := map[string]any{
		"agents": []any{map[string]any{
			"planner": map[string]any{
				"model":       "",
				"temperature": 0,
				"max_tokens":  0,
			},
		}},
	}
	if err := ApplyElementDefaults(raw); err != nil {
		t.Fatalf("ApplyElementDefaults returned error: %v", err)
	}
	cfg := Default()
	if err := DecodeInto(raw, cfg); err != nil {
		t.Fatalf("DecodeInto returned error: %v", err)
	}
	override := cfg.Agents[0].Planner
	if override == nil || override.Model == nil || *override.Model != "" {
		t.Fatalf("planner.model = %#v, want present empty string", override)
	}
	if override.Temperature == nil || *override.Temperature != 0 {
		t.Fatalf("planner.temperature = %#v, want present zero", override)
	}
	if override.MaxTokens == nil || *override.MaxTokens != 0 {
		t.Fatalf("planner.max_tokens = %#v, want present zero", override)
	}
}
