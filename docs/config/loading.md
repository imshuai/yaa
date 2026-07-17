# 配置加载流程

> 文档路径: docs/config/loading.md
> 上级: README.md | 依赖: overview.md, architecture.md §3.12

---

## 1. 概述

配置加载是 Runtime 启动的第一步（参见架构 §3.1 初始化顺序）。加载流程负责从多个来源收集配置，按优先级合并，最终生成 Effective Config 供所有子系统使用。

```text
Runtime 启动
    │
    ▼
┌──────────────────────────────────────────────┐
│              配置加载管线                       │
│                                                │
│  1. 确定配置文件路径（查找顺序）                   │
│  2. 解析配置文件（YAML / TOML / JSON）           │
│  3. 展开环境变量引用（${VAR_NAME}）               │
│  4. 注入默认值（缺失字段填充）                    │
│  5. 命令行参数覆盖                               │
│  6. 校验配置                                    │
│                                                │
│  ──────────────────────────────────────→        │
│          Effective Config (*config.Config)      │
└──────────────────────────────────────────────┘
```

---

## 2. 配置文件查找顺序

Yaa! 按以下顺序查找配置文件，**第一个找到的文件即为最终配置文件**（不支持多文件叠加合并）：

| 优先级 | 查找路径 | 说明 |
|--------|----------|------|
| 1 | `--config` 命令行参数指定的路径 | 显式指定，最高优先 |
| 2 | `$YAA_CONFIG_PATH` 环境变量指定的路径 | 环境变量指定 |
| 3 | `./yaa.yaml`（或 `.yml` / `.toml` / `.json`） | 当前工作目录 |
| 4 | `~/.yaa/yaa.yaml` | 用户主目录 |
| 5 | `/etc/yaa/yaa.yaml` | 系统级配置目录（Linux/macOS） |

**格式自动检测：** 根据文件扩展名选择解析器。无扩展名时默认按 YAML 解析。

```text
查找流程:

  --config flag 存在?
    ├─ 是 → 使用该路径（不存在则报错退出）
    └─ 否 → 检查 $YAA_CONFIG_PATH
              ├─ 已设置 → 使用该路径
              └─ 未设置 → 依次探测:
                          ./yaa.{yaml,yml,toml,json}
                          ~/.yaa/yaa.{yaml,yml,toml,json}
                          /etc/yaa/yaa.{yaml,yml,toml,json}
                          ├─ 找到 → 使用第一个匹配项
                          └─ 全部未找到 → 使用内置默认配置（仅日志输出 warning）
```

---

## 3. 多源合并优先级

配置值来自四个层级，优先级从低到高依次覆盖：

```text
┌──────────────────────────────────────────────────────┐
│  优先级递增（后者覆盖前者）                              │
│                                                        │
│  ┌────────────────┐                                   │
│  │ 1. 内置默认值    │  代码中 Default() 定义              │  最低
│  └───────┬────────┘                                   │
│          ▼                                              │
│  ┌────────────────┐                                   │
│  │ 2. 配置文件      │  YAML / TOML / JSON 按字段覆盖      │
│  └───────┬────────┘                                   │
│          ▼                                              │
│  ┌────────────────┐                                   │
│  │ 3. 环境变量引用  │  ${VAR_NAME} 在配置文件值中展开      │
│  └───────┬────────┘                                   │
│          ▼                                              │
│  ┌────────────────┐                                   │
│  │ 4. 命令行参数    │  --flag value 按字段覆盖             │  最高
│  └────────────────┘                                   │
└──────────────────────────────────────────────────────┘
```

### 3.1 合并规则

| 规则 | 说明 |
|------|------|
| **按字段覆盖** | 非整体替换，仅覆盖显式设置了值的字段 |
| **切片覆盖** | 切片类型（如 `agents`、`providers`）整体替换，不逐元素合并 |
| **Map 合并** | `map[string]any` 类型按 key 合并，同 key 覆盖 |
| **零值不覆盖** | 命令行参数未设置时为零值，不覆盖已有配置值 |
| **环境变量展开** | 在配置文件解析后、默认值注入前展开 |

### 3.2 覆盖示例

```yaml
# yaa.yaml（配置文件）
runtime:
  api:
    http:
      addr: ":9090"
    # ws.enabled 未设置 → 使用默认值 true
```

```bash
# 环境变量引用（在配置文件值中展开）
# yaa.yaml 中: api_key: "${OPENAI_API_KEY}"
# 展开后: api_key: "sk-xxxxxxxx"
```

```bash
# 命令行参数覆盖
yaa --runtime.api.http.addr ":7070"
# 最终 addr = ":7070"（覆盖配置文件的 ":9090"）
```

### 3.3 最终值确定示例

| 配置项 | 内置默认值 | 配置文件 | 环境变量引用 | 命令行参数 | Effective |
|--------|-----------|----------|-------------|-----------|-----------|
| `runtime.api.http.addr` | `:8080` | `:9090` | — | `:7070` | `:7070` |
| `runtime.api.ws.enabled` | `true` | — | — | — | `true` |
| `providers[0].api_key` | — | `${OPENAI_API_KEY}` | `sk-xxx` | — | `sk-xxx` |
| `runtime.storage.type` | `sqlite` | — | — | `bbolt` | `bbolt` |

---

## 4. Go 实现代码示例

### 4.1 Loader 核心结构

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Loader 负责从多源加载并合并配置。
type Loader struct {
	configPath string // 显式指定的配置文件路径
	flags      map[string]any // 命令行参数（点分隔路径 → 值）
}

// NewLoader 创建配置加载器。
func NewLoader(configPath string, flags map[string]any) *Loader {
	return &Loader{
		configPath: configPath,
		flags:      flags,
	}
}

// Load 执行完整的配置加载管线。
func (l *Loader) Load() (*Config, error) {
	// Step 1: 确定配置文件路径
	path, err := l.resolveConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	// Step 2: 从内置默认值开始
	cfg := Default()

	// Step 3: 解析配置文件（如果找到）
	if path != "" {
		fileCfg, err := l.parseFile(path)
		if err != nil {
			return nil, fmt.Errorf("parse config file %s: %w", path, err)
		}
		// Step 4: 环境变量展开
		l.expandEnvVars(fileCfg)
		// Step 5: 合并配置文件到默认值之上
		mergeConfig(cfg, fileCfg)
	}

	// Step 6: 命令行参数覆盖
	if err := l.applyFlags(cfg); err != nil {
		return nil, fmt.Errorf("apply flags: %w", err)
	}

	// Step 7: 校验
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}
```

### 4.2 配置文件路径解析

```go
// resolveConfigPath 按优先级顺序确定配置文件路径。
// 返回空字符串表示未找到配置文件（使用纯默认配置）。
func (l *Loader) resolveConfigPath() (string, error) {
	// 优先级 1: --config 命令行参数
	if l.configPath != "" {
		if _, err := os.Stat(l.configPath); err != nil {
			return "", fmt.Errorf("config file not found: %s", l.configPath)
		}
		return l.configPath, nil
	}

	// 优先级 2: 环境变量
	if envPath := os.Getenv("YAA_CONFIG_PATH"); envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", fmt.Errorf("config file not found: %s", envPath)
		}
		return envPath, nil
	}

	// 优先级 3-5: 依次探测默认路径
	supportedExts := []string{".yaml", ".yml", ".toml", ".json"}
	searchDirs := []string{
		".",                // 当前工作目录
		expandHomeDir("~/.yaa"), // 用户主目录
		"/etc/yaa",         // 系统配置目录
	}

	for _, dir := range searchDirs {
		for _, ext := range supportedExts {
			path := filepath.Join(dir, "yaa"+ext)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// 未找到配置文件，使用默认配置
	return "", nil
}
```

### 4.3 环境变量展开

```go
// expandEnvVars 递归展开配置中所有 ${VAR_NAME} 引用。
func (l *Loader) expandEnvVars(cfg *Config) {
	// 遍历 providers 中的 api_key
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey = expandEnv(cfg.Providers[i].APIKey)
		cfg.Providers[i].BaseURL = expandEnv(cfg.Providers[i].BaseURL)
	}
	// 遍历 auth tokens
	for i := range cfg.Runtime.Auth.Tokens {
		cfg.Runtime.Auth.Tokens[i].Token = expandEnv(
			cfg.Runtime.Auth.Tokens[i].Token,
		)
	}
}

// expandEnv 将 ${VAR_NAME} 替换为环境变量值。
// 变量不存在时保留原文本并输出警告。
func expandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	// 简单实现：使用 os.Expand 处理 ${...} 语法
	return os.Expand(s, func(name string) string {
		val := os.Getenv(name)
		if val == "" {
			slog.Warn("environment variable not set",
				"var", name, "using_empty", true)
		}
		return val
	})
}
```

### 4.4 命令行参数覆盖

```go
// applyFlags 将命令行参数按点分隔路径覆盖到配置。
// flags 的 key 格式: "runtime.api.http.addr" → value: ":7070"
func (l *Loader) applyFlags(cfg *Config) error {
	for path, value := range l.flags {
		if err := setByPath(cfg, path, value); err != nil {
			return fmt.Errorf("set flag %s: %w", path, err)
		}
	}
	return nil
}

// setByPath 通过点分隔路径设置配置值（反射实现）。
func setByPath(cfg *Config, path string, value any) error {
	keys := strings.Split(path, ".")
	// 使用反射逐层导航到目标字段并赋值
	// 例如: "runtime.api.http.addr" → cfg.Runtime.API.HTTP.Addr
	return navigateAndSet(cfg, keys, value)
}
```

### 4.5 入口调用

```go
// LoadConfig 是供 Runtime 启动时调用的入口函数。
func LoadConfig(configPath string, flags map[string]any) (*Config, error) {
	loader := NewLoader(configPath, flags)
	return loader.Load()
}
```

```go
// main.go 中的调用示例
func main() {
	configPath := flag.String("config", "", "配置文件路径")
	var flags map[string]any
	// ... 解析其他 flag 到 flags map ...

	cfg, err := config.LoadConfig(*configPath, flags)
	if err != nil {
		log.Fatal(err)
	}

	// 将 cfg 传递给 Runtime
	rt := runtime.New(cfg)
	if err := rt.Start(); err != nil {
		log.Fatal(err)
	}
}
```

---

## 5. 错误处理

| 场景 | 行为 |
|------|------|
| `--config` 指定的文件不存在 | **报错退出**，不静默降级 |
| `$YAA_CONFIG_PATH` 指定的文件不存在 | **报错退出** |
| 默认路径全部未找到配置文件 | **使用内置默认配置**，输出 warning 日志 |
| 配置文件格式错误 | **报错退出**，附带行号与解析错误信息 |
| 环境变量引用的变量不存在 | 保留原文本，输出 warning，不中断加载 |
| 校验失败（必填项缺失等） | **报错退出**，附带校验错误列表 |

---

*最后更新: 2025-07-17*
