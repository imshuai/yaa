# 环境变量引用机制

> Yaa! Yet Another Agent Runtime
> 依赖: `docs/config/README.md` §1.3 配置层级与优先级

---

## 1. 概述

环境变量引用机制允许在配置文件中使用 `${VAR_NAME}` 语法引用环境变量，在配置加载阶段将其展开为实际值。这是 Yaa! 实现「敏感信息不入配置文件」安全策略的核心手段。

### 1.1 设计目标

| 目标 | 说明 |
|------|------|
| **安全性** | API Key、Token 等敏感信息通过环境变量注入，不硬编码到配置文件 |
| **灵活性** | 支持默认值、嵌套引用，适应不同部署环境 |
| **透明性** | 展开过程可追溯，支持展开日志与调试 |
| **零侵入** | 对配置文件格式无特殊要求，`${...}` 语法在所有格式中通用 |

---

## 2. 语法规范

### 2.1 基本语法

| 语法 | 含义 | 示例 |
|------|------|------|
| `${VAR_NAME}` | 引用环境变量，未设置时展开为空字符串 | `${YAA_HTTP_ADDR}` |
| `${VAR_NAME:-default}` | 引用环境变量，未设置或为空时使用默认值 | `${YAA_DB_PATH:-/data/yaa.db}` |
| `${VAR_NAME-default}` | 引用环境变量，未设置时使用默认值（允许空值） | `${YAA_PORT-8080}` |
| `$$` | 转义，展开为字面量 `$` | `$$5.00` → `$5.00` |

### 2.2 展开规则

```text
展开顺序: 递归展开 → 循环检测 → 类型转换

1. 扫描配置值中的 ${...} 模式
2. 提取变量名和默认值
3. 查找环境变量:
   - 找到 → 使用环境变量值
   - 未找到且有默认值 → 使用默认值
   - 未找到且无默认值 → 展开为空字符串
4. 对展开后的值递归检查是否仍包含 ${...}（嵌套引用）
5. 循环检测: 记录已展开的变量名，发现循环则报错
```

### 2.3 嵌套引用

环境变量的值本身也可以包含 `${...}` 引用，展开时会递归处理：

```yaml
# 配置文件
database:
  url: "${YAA_DB_URL:-postgres://${YAA_DB_USER:-yaa}:${YAA_DB_PASS:-yaa}@localhost:5432/yaa}"
```

```bash
# 环境变量
export YAA_DB_USER=admin
# YAA_DB_PASS 未设置
# YAA_DB_URL 未设置

# 展开结果:
# postgres://admin:yaa@localhost:5432/yaa
```

---

## 3. 使用示例

### 3.1 常见场景

```yaml
# config.yaml — 典型配置示例
runtime:
  api:
    http:
      addr: "${YAA_HTTP_ADDR:-:8080}"
      # 未设置环境变量时使用 :8080

providers:
  openai:
    api_key: "${OPENAI_API_KEY}"
    # 敏感信息必须通过环境变量注入，无默认值
    base_url: "${OPENAI_BASE_URL:-https://api.openai.com/v1}"
    # 可选覆盖，默认指向官方 API

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    base_url: "${ANTHROPIC_BASE_URL:-https://api.anthropic.com}"

agent:
  default_provider: "${YAA_DEFAULT_PROVIDER:-openai}"
  max_turns: "${YAA_MAX_TURNS:-50}"
  # 数值类型: 展开后由配置解析器做类型转换
```

### 3.2 默认值中的特殊字符

```yaml
# 默认值包含冒号时需注意
logging:
  level: "${YAA_LOG_LEVEL:-info}"
  # 默认值 "info" 不含特殊字符，正常

# 默认值包含 } 时无法转义，请避免使用
# 错误示例: ${VAR:-value}with}brace}
```

---

## 4. Go 实现

### 4.1 环境变量展开器

```go
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// EnvResolver 环境变量引用解析器
type EnvResolver struct {
	// maxDepth 递归展开最大深度，防止无限循环
	maxDepth int
	// debug 是否记录展开日志
	debug bool
}

// NewEnvResolver 创建环境变量解析器
func NewEnvResolver() *EnvResolver {
	return &EnvResolver{maxDepth: 10}
}

// envPattern 匹配 ${VAR}、${VAR:-default}、${VAR-default}
var envPattern = regexp.MustCompile(
	`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(:?[-?])?([^}]*)\}`,
)

// Resolve 展开字符串中的所有环境变量引用
func (r *EnvResolver) Resolve(value string) (string, error) {
	return r.resolveDepth(value, 0, map[string]bool{})
}

func (r *EnvResolver) resolveDepth(value string, depth int, seen map[string]bool) (string, error) {
	if depth > r.maxDepth {
		return "", fmt.Errorf("envvar: exceeded max depth %d, possible circular reference", r.maxDepth)
	}

	result := envPattern.ReplaceAllStringFunc(value, func(match string) string {
		// 转义 $$ 不在此处理，ReplaceAllStringFunc 已逐个匹配

		subs := envPattern.FindStringSubmatch(match)
		// subs: [0]全匹配 [1]变量名 [2]操作符 [3]默认值
		varName := subs[1]
		operator := subs[2]
		defaultVal := subs[3]

		// 循环检测
		if seen[varName] {
			return match // 保留原文，上层报错
		}

		envVal, exists := os.LookupEnv(varName)

		switch {
		case exists && envVal != "":
			return envVal
		case exists && operator == ":-":
			// 变量存在但为空，且有 :- 操作符 → 使用默认值
			return defaultVal
		case exists:
			// 变量存在但为空，无 :- → 空字符串
			return ""
		case operator == ":-" || operator == "-":
			// 变量不存在，有默认值
			return defaultVal
		default:
			// 变量不存在，无默认值 → 空字符串
			return ""
		}
	})

	// 递归检查: 展开后的值可能仍含 ${...}
	if envPattern.MatchString(result) && result != value {
		return r.resolveDepth(result, depth+1, seen)
	}

	return result, nil
}

// ResolveMap 递归展开 map 中所有字符串值
func (r *EnvResolver) ResolveMap(m map[string]interface{}) error {
	return r.resolveMap(m, 0)
}

func (r *EnvResolver) resolveMap(m map[string]interface{}, depth int) error {
	for key, val := range m {
		switch v := val.(type) {
		case string:
			resolved, err := r.Resolve(v)
			if err != nil {
				return fmt.Errorf("envvar: resolve %q: %w", key, err)
			}
			m[key] = resolved
		case map[string]interface{}:
			if err := r.resolveMap(v, depth+1); err != nil {
				return fmt.Errorf("envvar: resolve nested %q: %w", key, err)
			}
		case []interface{}:
			for i, item := range v {
				if s, ok := item.(string); ok {
					resolved, err := r.Resolve(s)
					if err != nil {
						return fmt.Errorf("envvar: resolve %q[%d]: %w", key, i, err)
					}
					v[i] = resolved
				}
			}
		}
	}
	return nil
}
```

### 4.2 在加载管线中集成

```go
// LoadPipeline 配置加载管线
func LoadPipeline(path string) (*Config, error) {
	// 1. 读取并解析配置文件 (YAML/TOML/JSON)
	raw, err := loadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load file: %w", err)
	}

	// 2. 环境变量展开 (在校验前)
	resolver := NewEnvResolver()
	if err := resolver.ResolveMap(raw); err != nil {
		return nil, fmt.Errorf("envvar resolve: %w", err)
	}

	// 3. 类型转换与结构映射
	cfg, err := mapToConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("map to config: %w", err)
	}

	// 4. 默认值注入 + 校验
	cfg = ApplyDefaults(cfg)
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	return cfg, nil
}
```

---

## 5. 调试与排查

### 5.1 展开日志

当 `debug` 为 true 时，解析器会记录每次展开的变量名和结果：

```text
[envvar] YAA_HTTP_ADDR: env not set, default=":8080"
[envvar] OPENAI_API_KEY: resolved from env (length=51)
[envvar] YAA_DB_URL: nested resolve depth=2
```

### 5.2 常见问题

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 值为空字符串 | 环境变量未设置且无默认值 | 添加 `${VAR:-default}` |
| `exceeded max depth` | 变量引用形成循环 | 检查环境变量值是否互相引用 |
| 类型转换失败 | 数值字段展开为非数字 | 确保环境变量或默认值为合法数字 |
| 默认值含 `}` 被截断 | 正则匹配 `[^}]*` 不支持嵌套 `}` | 避免在默认值中使用 `}` 字符 |

---

## 6. 安全注意事项

| 事项 | 说明 |
|------|------|
| **不记录敏感值** | 展开日志只记录变量名和长度，不记录实际值 |
| **配置文件不入库** | 配置文件不应包含 `${OPENAI_API_KEY}` 以外的密钥明文 |
| **环境变量管理** | 生产环境通过 `.env` 文件或密钥管理服务注入，不直接 export |
| **展开时机** | 在配置校验前展开，校验器看到的是最终值 |

---

*最后更新: 2025-07-17*
