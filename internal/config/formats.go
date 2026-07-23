package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Format identifies a supported configuration encoding.
type Format string

const (
	FormatYAML Format = "yaml"
	FormatJSON Format = "json"
	FormatTOML Format = "toml"
)

var (
	ErrConfigFormatUnsupported = errors.New("config: unsupported format")
	ErrConfigParseFailed       = errors.New("config: parse failed")
)

// DetectFormat selects a parser from path's extension. Unknown extensions are rejected.
func DetectFormat(path string) (Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case "", ".yaml", ".yml":
		return FormatYAML, nil
	case ".json":
		return FormatJSON, nil
	case ".toml":
		return FormatTOML, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrConfigFormatUnsupported, path)
	}
}

// ParseToMap parses data into the raw map used by migration and defaulting.
func ParseToMap(data []byte, format Format) (map[string]any, error) {
	out := map[string]any{}
	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("%w: yaml: %v", ErrConfigParseFailed, err)
		}
	case FormatJSON:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			return nil, fmt.Errorf("%w: json: %v", ErrConfigParseFailed, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%w: json: multiple top-level values", ErrConfigParseFailed)
		}
	case FormatTOML:
		if _, err := toml.Decode(string(data), &out); err != nil {
			return nil, fmt.Errorf("%w: toml: %v", ErrConfigParseFailed, err)
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrConfigFormatUnsupported, format)
	}
	if out == nil {
		out = map[string]any{}
	}
	normalizeRawValue(out)
	return out, nil
}

func normalizeRawValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeRawValue(item)
		}
	case []any:
		for i, item := range typed {
			typed[i] = normalizeRawValue(item)
		}
	case []map[string]any:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = normalizeRawValue(item)
		}
		return items
	}
	return value
}

// ParseFileToMap reads and parses one configuration file.
func ParseFileToMap(path string) (map[string]any, error) {
	format, err := DetectFormat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return ParseToMap(data, format)
}
