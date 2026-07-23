package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		path string
		want Format
	}{
		{path: "yaa.yaml", want: FormatYAML},
		{path: "yaa.YML", want: FormatYAML},
		{path: "yaa.json", want: FormatJSON},
		{path: "yaa.TOML", want: FormatTOML},
		{path: "yaa", want: FormatYAML},
	}
	for _, tt := range tests {
		got, err := DetectFormat(tt.path)
		if err != nil {
			t.Errorf("DetectFormat(%q) returned error: %v", tt.path, err)
		}
		if got != tt.want {
			t.Errorf("DetectFormat(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}

	if _, err := DetectFormat("yaa.ini"); !errors.Is(err, ErrConfigFormatUnsupported) {
		t.Fatalf("DetectFormat unknown extension error = %v, want ErrConfigFormatUnsupported", err)
	}
}

func TestParseToMapAllFormats(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		data   string
		want   map[string]any
	}{
		{
			name:   "yaml",
			format: FormatYAML,
			data:   "runtime:\n  addr: 127.0.0.1:8080\nitems:\n  - one\n",
			want: map[string]any{
				"runtime": map[string]any{"addr": "127.0.0.1:8080"},
				"items":   []any{"one"},
			},
		},
		{
			name:   "json",
			format: FormatJSON,
			data:   `{"count":42,"enabled":true}`,
			want: map[string]any{
				"count":   json.Number("42"),
				"enabled": true,
			},
		},
		{
			name:   "toml",
			format: FormatTOML,
			data:   "[runtime]\naddr = \"127.0.0.1:8080\"\n",
			want: map[string]any{
				"runtime": map[string]any{"addr": "127.0.0.1:8080"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseToMap([]byte(tt.data), tt.format)
			if err != nil {
				t.Fatalf("ParseToMap returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseToMap = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseToMapRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		data   string
	}{
		{name: "yaml", format: FormatYAML, data: "- not a map"},
		{name: "json", format: FormatJSON, data: "{"},
		{name: "json multiple", format: FormatJSON, data: "{} {}"},
		{name: "toml", format: FormatTOML, data: "bad = = value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseToMap([]byte(tt.data), tt.format)
			if !errors.Is(err, ErrConfigParseFailed) {
				t.Fatalf("ParseToMap error = %v, want ErrConfigParseFailed", err)
			}
		})
	}

	if _, err := ParseToMap(nil, Format("ini")); !errors.Is(err, ErrConfigFormatUnsupported) {
		t.Fatalf("ParseToMap unsupported format error = %v, want ErrConfigFormatUnsupported", err)
	}
}

func TestParseToMapNullRoot(t *testing.T) {
	got, err := ParseToMap([]byte("null"), FormatJSON)
	if err != nil {
		t.Fatalf("ParseToMap(null) returned error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("ParseToMap(null) = %#v, want empty non-nil map", got)
	}
}

func TestParseFileToMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yaa.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := ParseFileToMap(path)
	if err != nil {
		t.Fatalf("ParseFileToMap returned error: %v", err)
	}
	if got["ok"] != true {
		t.Fatalf("ParseFileToMap = %#v, want ok=true", got)
	}

	if _, err := ParseFileToMap(filepath.Join(dir, "missing.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ParseFileToMap missing file error = %v, want os.ErrNotExist", err)
	}
	if _, err := ParseFileToMap(filepath.Join(dir, "yaa.ini")); !errors.Is(err, ErrConfigFormatUnsupported) {
		t.Fatalf("ParseFileToMap unknown format error = %v, want ErrConfigFormatUnsupported", err)
	}
}
