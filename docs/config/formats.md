# 配置格式

> 文档路径: `docs/config/formats.md`
> 上级: [README.md](README.md)

---

## 1. 支持范围

| 格式 | 扩展名 | 解析器 |
|------|--------|--------|
| YAML | `.yaml`, `.yml` | `gopkg.in/yaml.v3` |
| JSON | `.json` | 标准库 `encoding/json` |
| TOML | `.toml` | `github.com/BurntSushi/toml` |

主格式是 YAML。路径没有扩展名时按 YAML；存在未知扩展名时返回 `ErrConfigFormatUnsupported`，不做内容嗅探。

```go
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
```

## 2. 统一中间表示

解析器只产生 `map[string]any`，不直接写 `Config`。后续统一执行迁移、环境变量展开、默认值注入、presence-aware 解码和校验。

```go
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
```

JSON number 保持为 `json.Number`，最终由统一解码器按目标字段转换；超出目标整数范围时必须报错。YAML map key 必须是字符串。BurntSushi TOML 会把 array-of-tables 解为 `[]map[string]any`，`normalizeRawValue` 必须在返回前递归统一为 `[]any`，使迁移、环境变量展开和默认值注入只处理一种容器表示。TOML 解到 raw Map 时不能使用 `Undecoded()` 判错，因为 `any` 值会被该库标记为未解码；所有格式的结构体边界未知字段统一由后续 `DecodeInto(... ErrorUnused=true)` 拒绝，开放 `map[string]any` 的内部 key 由对应 Provider/Tool/Skill/Plugin 专用 decoder 校验。

## 3. 跨格式语义

| 值 | YAML | TOML | JSON |
|----|------|------|------|
| duration | `timeout: 30s` | `timeout = "30s"` | `"timeout": "30s"` |
| 环境变量 | `api_key: "${API_KEY}"` | `api_key = "${API_KEY}"` | `"api_key": "${API_KEY}"` |
| 空数组 | `items: []` | `items = []` | `"items": []` |
| null | `null` | 不支持 | `null` |

非零 `duration` 使用 Go duration 字符串，不接受不同格式各自的整数单位；所有 `duration` 字段都允许使用数值 `0` 表示零值，非零数值必须拒绝。TOML 无 null，因此可空字段只能省略；YAML/JSON 的 null 只允许写入 pointer、slice、map 或 interface，写入其他字段时报错。

## 4. 格式转换

`yaa config convert` 转换原始 Map，不调用环境变量展开，也不输出 Effective Config，避免把 Secret 展开后写入磁盘。目标文件使用临时文件 + `fsync` + `Rename` 原子替换，权限固定为 `0600`。

```go
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
```

转换前应以源路径执行一次完整 `config.Load` 做 schema 校验；转换写出的仍是未展开环境变量、未注入默认值的原始配置。TOML 无法表达的 null 会返回明确错误，不静默删除。

```bash
yaa config convert --from ./yaa.yaml --to ./yaa.toml
```

---

*最后更新: 2025-07-17*
