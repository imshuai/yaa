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

// MarshalMap 将 raw map 序列化为指定格式. docs/config/formats.md §4.
// TOML 无法表达 null — 遇到会返回明确错误, 不静默删除.
func MarshalMap(raw map[string]any, format Format) ([]byte, error) {
	switch format {
	case FormatYAML:
		data, err := yaml.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("marshal yaml: %w", err)
		}
		return data, nil
	case FormatJSON:
		// JSON 保留 key 顺序使用 json.MarshalIndent
		data, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		return append(data, '\n'), nil
	case FormatTOML:
		buf := &bytes.Buffer{}
		if err := toml.NewEncoder(buf).Encode(raw); err != nil {
			return nil, fmt.Errorf("marshal toml: %w", err)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrConfigFormatUnsupported, format)
	}
}

// ErrConfigMarshalFailed 表示配置序列化失败.
var ErrConfigMarshalFailed = errors.New("config: marshal failed")

// atomicWriteFile 原子写入文件: 写临时文件 → fsync → Rename 替换.
// docs/config/formats.md §4: 目标文件权限固定为 perm.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".yaa-convert-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// 失败路径: 清理临时文件
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// Convert 转换配置文件格式. docs/config/formats.md §4.
// 不做环境变量展开, 不输出 Effective Config, 避免 Secret 展开后写入磁盘.
// 转换前可选 schema 校验 (由 caller 决定, Convert 本身只做格式转换).
func Convert(srcPath, dstPath string) error {
	raw, err := ParseFileToMap(srcPath)
	if err != nil {
		return err
	}
	dstFormat, err := DetectFormat(dstPath)
	if err != nil {
		return err
	}
	data, err := MarshalMap(raw, dstFormat)
	if err != nil {
		return err
	}
	return atomicWriteFile(dstPath, data, 0o600)
}
