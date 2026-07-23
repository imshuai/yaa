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
│  3. 按 config_version 迁移原始 Map                │
│  4. 展开环境变量引用（${VAR_NAME}）               │
│  5. 注入默认值并 presence-aware 解码              │
│  6. 命令行参数覆盖                               │
│  7. 校验配置                                    │
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
| **按字段覆盖** | 只覆盖文件中实际出现的字段；显式 `false`、`0` 和空字符串都生效 |
| **切片覆盖** | 切片类型（如 `agents`、`providers`）出现时整体替换；显式 `[]` 会清空默认切片 |
| **Map 合并** | Map 按 key 递归合并，同 key 覆盖；显式 `{}` 不删除默认 key，需要逐 key 配置其关闭值 |
| **Flag presence** | 只应用 `flag.FlagSet.Visit` 确认由用户显式设置的 flag，不能用 Go 零值推断 |
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
| `runtime.api.http.addr` | `127.0.0.1:8080` | `:9090` | — | `:7070` | `:7070` |
| `runtime.api.ws.enabled` | `true` | — | — | — | `true` |
| `providers[0].api_key` | — | `${OPENAI_API_KEY}` | `sk-xxx` | — | `sk-xxx` |
| `runtime.storage.type` | `sqlite` | — | — | `memory` | `memory` |

---

## 4. Go 实现代码示例

### 4.1 Loader 核心结构

```go
package config

import (
    "fmt"

    "golang.org/x/exp/slog"
)

// Loader 负责从多源加载并合并配置。
type Loader struct {
	configPath string         // 显式指定的配置文件路径
	flags      map[string]any // 命令行参数（点分隔路径 → 值）
	logger     *slog.Logger
}

// NewLoader 创建配置加载器。
func NewLoader(configPath string, flags map[string]any) *Loader {
	return &Loader{
		configPath: configPath,
		flags:      flags,
		logger:     slog.Default(),
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

	// Step 3: 将配置文件解析为保留字段存在性的通用 Map
	if path != "" {
		raw, err := l.parseFileToMap(path)
		if err != nil {
			return nil, fmt.Errorf("parse config file %s: %w", path, err)
		}
		// Step 4: 迁移原始 Map；迁移失败不进入解码阶段
		raw, err = migrateRaw(raw, l.logger)
		if err != nil {
			return nil, fmt.Errorf("migrate config: %w", err)
		}
		// Step 5: 环境变量展开
		if err := new(EnvResolver).ResolveMap(raw); err != nil {
			return nil, fmt.Errorf("expand environment: %w", err)
		}
		// Step 6: 为新复合元素补入缺失字段默认值，不覆盖显式零值
		if err := ApplyElementDefaults(raw); err != nil {
			return nil, fmt.Errorf("apply element defaults: %w", err)
		}
		// Step 7: 只覆盖文件中实际出现的字段，显式 false/0 仍然生效
		if err := DecodeInto(raw, cfg); err != nil {
			return nil, fmt.Errorf("decode config: %w", err)
		}
	}

	// Step 8: 命令行参数覆盖
	if err := l.applyFlags(cfg); err != nil {
		return nil, fmt.Errorf("apply flags: %w", err)
	}

	// Step 9: 校验
	if err := new(Validator).Validate(cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}
```

### 4.2 Map 解析与 presence-aware 解码

所有格式先解析为 `map[string]any`。迁移和环境变量展开后，先按 [默认值契约](validation.md#数组与动态-map-元素) 调用 `ApplyElementDefaults(raw)`，再用 `mapstructure` 写入已经由 `Default()` 初始化的目标。`ZeroFields=false` 只保留 source 未触及的根字段和 Map key；同一 Map key 的复合 value 仍须由 `ApplyElementDefaults` 补齐，不能替代新切片/动态 Map 元素的基底注入；`ErrorUnused=true` 拒绝结构体边界的未知键，duration hook 统一解析 `30s` 等字符串。显式 `false`、`0`、空字符串和空切片会覆盖默认值；空 Map 按 §3.1 的 key merge 语义处理，不表示清空继承的默认 key。显式 `null` 不被默认阶段覆盖，非 nullable 字段由 `DecodeInto` 以带路径类型错误拒绝。`map[string]any` 内部键由对应 Provider/Tool/Skill/Plugin 的第二阶段严格 decoder 校验。

```go
func DecodeInto(raw map[string]any, dst *Config) error {
    // 在 mapstructure 前执行 presence 预处理：未知键报完整路径；
    // 已出现的 slice 先清零以实现整体替换；nullable 的 null 清零，
    // 非 nullable 的 null 返回类型错误。Map 非 null 仍按 key 合并。
    if err := prepareDecodeTarget(raw, dst); err != nil {
        return err
    }
    dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
        Result:           dst,
        TagName:          "yaml",
        ZeroFields:       false,
        ErrorUnused:      true,
        WeaklyTypedInput: false,
        MatchName: func(mapKey, fieldName string) bool {
            return mapKey == fieldName
        },
        DecodeHook: strictDurationDecodeHook(),
    })
    if err != nil {
        return err
    }
    return dec.Decode(raw)
}
```

`prepareDecodeTarget` 是 `DecodeInto` 的必需前置步骤，不能省略。`mapstructure` 在 `ZeroFields=false` 时会保留目标 slice 的尾部元素，也会静默忽略 `nil`；预处理必须只遍历 raw 中实际存在的字段，按 canonical YAML tag 生成完整路径（数组使用 `[n]`），并执行以下规则：出现的 slice 先设为零值；`null` 只允许写入 pointer、slice、map 或 interface 并设为零值；写入 string、number、bool、struct 等非 nullable 字段时报错；非 null map 保留目标 map 并递归合并。预处理失败时不得调用 decoder。字段名按 YAML tag 大小写精确匹配，不能使用 `mapstructure` 默认的大小写不敏感匹配。

`strictDurationDecodeHook` 只把 Go duration 字符串转换为 `time.Duration`；数值只接受 `0`，用于表示文档中的禁用/继承语义，任何非零数值都返回带完整字段路径的类型错误。该 hook 必须显式区分 `json.Number` 与 Go `string`，不能直接使用 `mapstructure.StringToTimeDurationHookFunc()`：`json.Number` 的底层 kind 也是 string，后者会对 JSON 数值执行错误的类型断言并 panic。普通 string/int/bool 字段也必须在预处理阶段检查原始类型和目标范围，禁止 `json.Number` 到 string、float 到 int 等隐式转换。

`parseFileToMap` 按扩展名选择解析器：YAML 使用 `yaml.v3` 解到 `map[string]any`；JSON 使用 `json.Decoder` + `UseNumber` 并拒绝第二个顶层值；TOML 使用 `BurntSushi/toml` 解到 raw Map，并在返回前把 array-of-tables 的 `[]map[string]any` 递归规范化为 `[]any`。TOML 不在此阶段调用 `Undecoded()`（`any` 值会被该库标记为未解码）。所有格式的未知字段统一由后续 `DecodeInto(... ErrorUnused=true)` 检查。无扩展名按 YAML。解析器不得直接解到 `Config`，否则会绕过迁移、环境变量和统一 unknown-field 检查。完整格式约定见 [`formats.md`](formats.md)。

### 4.3 配置文件路径解析

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

### 4.4 环境变量展开

环境变量必须在通用 Map 上递归展开，不能只遍历 Provider 和 Auth 字段。具体语法、缺失变量行为和 `EnvResolver.ResolveMap` 实现见 [envvar.md](envvar.md)。

### 4.5 命令行参数覆盖

CLI flag 只允许修改预先注册的固定结构体标量叶字段（例如 `runtime.api.http.addr`）；不允许通过数组下标、动态 Map key 或复合值创建 `agents[]`、`providers[]`、`mcp.servers[]` 等新元素。`applyFlags`/`setByPath` 对未注册、数组/动态 Map 或非标量路径返回错误，因此所有元素默认值都已在 typed decode 前完成。

```go
// applyFlags 将命令行参数按点分隔路径覆盖到配置。
// flags 只包含已注册的固定标量路径，如 "runtime.api.http.addr" → ":7070"
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

### 4.6 入口调用

```go
// Load 是供 Runtime 启动和热更新共同调用的唯一入口函数。
func Load(configPath string, flags map[string]any) (*Config, error) {
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

	cfg, err := config.Load(*configPath, flags)
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
| 环境变量引用的变量不存在且没有默认值 | **报错退出**；可选值必须写 `${VAR:-default}` |
| 校验失败（必填项缺失等） | **报错退出**，附带校验错误列表 |

---

*最后更新: 2025-07-17*
