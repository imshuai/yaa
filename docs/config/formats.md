# 多格式支持

> Yaa! Yet Another Agent Runtime
> 依赖: [README.md](README.md) §1.1, [loading.md](loading.md)

---

## 1. 概述

Yaa! 配置系统主推 YAML，同时兼容 TOML 和 JSON。通过统一的解析接口和文件扩展名自动检测，用户可自由选择格式，Runtime 内部统一转换为 Go 结构体。

### 1.1 支持矩阵

| 格式 | 扩展名 | 库 | 场景 |
|------|--------|----|------|
| **YAML** | `.yaml` `.yml` | `gopkg.in/yaml.v3` | 主推格式，人类友好 |
| **TOML** | `.toml` | `github.com/BurntSushi/toml` | 运维场景，语义清晰 |
| **JSON** | `.json` | `encoding/json` (标准库) | 程序生成、API 交互 |

### 1.2 设计原则

- **统一接口**：不同格式解析为同一个 `Config` 结构体
- **自动检测**：根据文件扩展名选择解析器，无需手动指定
- **格式等价**：三种格式表达能力对齐，任意格式可无损转换
- **零 CGO**：所有解析库均为纯 Go 实现

---

## 2. 文件扩展名自动检测

```go
package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Format string

const (
	FormatYAML Format = "yaml"
	FormatTOML Format = "toml"
	FormatJSON Format = "json"
)

// DetectFormat 根据文件扩展名自动检测配置格式
func DetectFormat(path string) (Format, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return FormatYAML, nil
	case ".toml":
		return FormatTOML, nil
	case ".json":
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported config format: %s (supported: .yaml, .yml, .toml, .json)", ext)
	}
}
```

---

## 3. 统一解析接口

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Parser 是所有格式解析器的统一接口
type Parser interface {
	Parse(data []byte, v any) error
	Marshal(v any) ([]byte, error)
}

// --- YAML ---

type YAMLParser struct{}

func (YAMLParser) Parse(data []byte, v any) error  { return yaml.Unmarshal(data, v) }
func (YAMLParser) Marshal(v any) ([]byte, error)  { return yaml.Marshal(v) }

// --- TOML ---

type TOMLParser struct{}

func (TOMLParser) Parse(data []byte, v any) error { return toml.Unmarshal(data, v) }
func (TOMLParser) Marshal(v any) ([]byte, error) { return toml.Marshal(v) }

// --- JSON ---

type JSONParser struct{}

func (JSONParser) Parse(data []byte, v any) error { return json.Unmarshal(data, v) }
func (JSONParser) Marshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// NewParser 根据格式创建对应的解析器
func NewParser(format Format) (Parser, error) {
	switch format {
	case FormatYAML:
		return YAMLParser{}, nil
	case FormatTOML:
		return TOMLParser{}, nil
	case FormatJSON:
		return JSONParser{}, nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// ParseFile 根据文件扩展名自动选择解析器并解析
func ParseFile(path string, v any) error {
	format, err := DetectFormat(path)
	if err != nil {
		return err
	}
	parser, err := NewParser(format)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	if err := parser.Parse(data, v); err != nil {
		return fmt.Errorf("parse %s config: %w", format, err)
	}
	return nil
}
```

---

## 4. 格式转换

### 4.1 转换流程

```text
源文件 ──Parse──▶ Config 结构体 ──Marshal──▶ 目标格式
 (YAML)           (Go struct)               (TOML/JSON)
```

### 4.2 转换实现

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Convert 将配置文件从一种格式转换为另一种格式
func Convert(srcPath, dstPath string) error {
	// 1. 解析源文件
	cfg := &Config{}
	if err := ParseFile(srcPath, cfg); err != nil {
		return fmt.Errorf("parse source: %w", err)
	}

	// 2. 检测目标格式
	dstFormat, err := DetectFormat(dstPath)
	if err != nil {
		return fmt.Errorf("detect dst format: %w", err)
	}

	// 3. 序列化为目标格式
	parser, err := NewParser(dstFormat)
	if err != nil {
		return err
	}
	data, err := parser.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal to %s: %w", dstFormat, err)
	}

	// 4. 写入目标文件
	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		return fmt.Errorf("write dst file: %w", err)
	}
	return nil
}
```

### 4.3 CLI 用法

```bash
# YAML → TOML
yaa config convert --from config.yaml --to config.toml

# YAML → JSON
yaa config convert --from config.yaml --to config.json

# TOML → YAML
yaa config convert --from config.toml --to config.yaml
```

---

## 5. 格式等价性

三种格式对同一配置的表达对照：

| 特性 | YAML | TOML | JSON |
|------|------|------|------|
| 注释 | `# 注释` | `# 注释` | ❌ 不支持 |
| 嵌套对象 | 缩进 | `[section]` | `{}` 嵌套 |
| 数组 | `- item` | `item = [...]` | `[...]` |
| 多行字符串 | `|` 或 `>` | `"""..."""` | 需转义 `\n` |
| 布尔值 | `true`/`false` | `true`/`false` | `true`/`false` |
| 空值 | `null` 或 `~` | 不支持 | `null` |
| 人类可读 | ⭐⭐⭐ | ⭐⭐⭐ | ⭐ |

### 等价配置示例

**YAML (`config.yaml`):**

```yaml
runtime:
  api:
    http:
      addr: ":8080"
      read_timeout: 30s
log:
  level: info
  format: json
providers:
  - name: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
```

**TOML (`config.toml`):**

```toml
[runtime.api.http]
addr = ":8080"
read_timeout = "30s"

[log]
level = "info"
format = "json"

[[providers]]
name = "openai"
model = "gpt-4o"
api_key = "${OPENAI_API_KEY}"
```

**JSON (`config.json`):**

```json
{
  "runtime": {
    "api": {
      "http": {
        "addr": ":8080",
        "read_timeout": "30s"
      }
    }
  },
  "log": {
    "level": "info",
    "format": "json"
  },
  "providers": [
    {
      "name": "openai",
      "model": "gpt-4o",
      "api_key": "${OPENAI_API_KEY}"
    }
  ]
}
```

---

## 6. 注意事项

| 注意点 | 说明 |
|--------|------|
| **TOML 限制** | TOML 不支持 `null` 值，可选字段需使用零值或指针 |
| **JSON 注释** | JSON 标准不支持注释，建议用 YAML/TOML 编写人工维护的配置 |
| **类型保真** | YAML 整数 `1` 和字符串 `"1"` 需注意类型推断，建议显式标注 |
| **环境变量** | 三种格式均支持 `${VAR_NAME}` 引用，在解析后统一展开 |
| **推荐格式** | 新项目推荐 YAML，运维团队偏好 TOML，程序生成用 JSON |

---

*最后更新: 2025-07-16*
