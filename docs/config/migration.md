# 配置迁移与兼容

> 文档路径: `docs/config/migration.md`
> 上级: [README.md](README.md)

---

## 1. 版本规则

配置根字段 `config_version` 使用 `major.minor` 字符串。当前版本是 `1.0`，由 `CurrentSchemaVersion` 唯一声明。

| 变更 | 版本 | 处理 |
|------|------|------|
| 新增可选字段或调整默认值 | minor +1 | 可自动迁移 |
| 重命名、删除或改变字段语义 | major +1 | 必须有显式迁移函数 |
| 文件版本高于 Runtime | 无 | 拒绝加载，不降级解析 |
| 文件版本低于当前版本但无迁移路径 | 无 | 拒绝加载并指出缺失边 |

未写 `config_version` 的旧文件按 `1.0` 解释。该规则只适用于当前仍兼容 `1.0` 的文件；一旦未来引入无法推断的旧格式，必须新增明确的 legacy 版本。

```go
import (
    "fmt"
    "strconv"
    "strings"
)

type ConfigSchema struct {
    Major int
    Minor int
}

var CurrentSchemaVersion = ConfigSchema{Major: 1, Minor: 0}

func ParseVersion(raw string) (ConfigSchema, error) {
    parts := strings.Split(raw, ".")
    if len(parts) != 2 {
        return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
    }
    major, ok := parseVersionPart(parts[0])
    if !ok {
        return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
    }
    minor, ok := parseVersionPart(parts[1])
    if !ok {
        return ConfigSchema{}, fmt.Errorf("invalid config_version %q", raw)
    }
    return ConfigSchema{Major: major, Minor: minor}, nil
}

func parseVersionPart(raw string) (int, bool) {
    if raw == "" {
        return 0, false
    }
    for i := 0; i < len(raw); i++ {
        if raw[i] < '0' || raw[i] > '9' {
            return 0, false
        }
    }
    value, err := strconv.Atoi(raw)
    return value, err == nil
}

func (v ConfigSchema) Compare(other ConfigSchema) int {
    if v.Major != other.Major {
        if v.Major < other.Major {
            return -1
        }
        return 1
    }
    if v.Minor < other.Minor {
        return -1
    }
    if v.Minor > other.Minor {
        return 1
    }
    return 0
}

func (v ConfigSchema) String() string {
    return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}
```

## 2. 迁移注册表

迁移函数接收 presence-aware 的 `map[string]any`，返回迁移后的 Map；它不得直接操作已经解码的 `Config`，否则无法区分显式零值。

```go
type MigrationFunc func(map[string]any) (map[string]any, error)

type Migration struct {
    From ConfigSchema
    To   ConfigSchema
    Run  MigrationFunc
}

var migrations = []Migration{
    // 当前版本为 1.0；未来新增边时在这里追加，不自动推测路径。
}

func Migrate(raw map[string]any, from, to ConfigSchema) (map[string]any, error) {
    if from.Compare(to) > 0 {
        return nil, fmt.Errorf("config downgrade is not supported: %s -> %s", from, to)
    }
    result := raw
    current := from
    for current.Compare(to) < 0 {
        var step *Migration
        for i := range migrations {
            candidate := &migrations[i]
            if candidate.From.Compare(current) == 0 {
                if step != nil {
                    return nil, fmt.Errorf("multiple migrations start at %s", current)
                }
                step = candidate
            }
        }
        if step == nil || step.Run == nil || step.To.Compare(current) <= 0 || step.To.Compare(to) > 0 {
            return nil, fmt.Errorf("no migration path from %s to %s", current, to)
        }
        var err error
        result, err = step.Run(result)
        if err != nil {
            return nil, fmt.Errorf("migration %s->%s failed: %w", step.From, step.To, err)
        }
        if result == nil {
            return nil, fmt.Errorf("migration %s->%s returned a nil config", step.From, step.To)
        }
        current = step.To
    }
    if result == nil {
        return nil, fmt.Errorf("config migration input is nil")
    }
    result["config_version"] = to.String()
    return result, nil
}
```

### 2.1 示例迁移

```go
// migrateV10ToV11 将旧 runtime.http.addr 移到 runtime.api.http.addr。
func migrateV10ToV11(raw map[string]any) (map[string]any, error) {
    runtime, ok := raw["runtime"].(map[string]any)
    if !ok {
        return raw, nil
    }
    oldHTTP, ok := runtime["http"].(map[string]any)
    if !ok {
        return raw, nil
    }
    api, _ := runtime["api"].(map[string]any)
    if api == nil {
        api = map[string]any{}
    }
    if _, exists := api["http"]; !exists {
        api["http"] = oldHTTP
    }
    runtime["api"] = api
    delete(runtime, "http")
    raw["runtime"] = runtime
    return raw, nil
}
```

迁移函数只在目标路径不存在时写入，避免覆盖用户已经同时填写的新字段；迁移成功后旧路径必须删除，未知字段检查才能发现拼写错误。

## 3. 加载管线集成

配置包只有一个公开加载入口：`(*Loader).Load()`（详见 [loading.md](loading.md)）。迁移是该入口中的一个步骤，不再定义第二个 `LoadConfig` 或让迁移模块自行反序列化。

```go
func migrateRaw(raw map[string]any, logger *slog.Logger) (map[string]any, error) {
    versionText, ok := raw["config_version"].(string)
    if !ok || versionText == "" {
        versionText = CurrentSchemaVersion.String()
        raw["config_version"] = versionText
    }
    from, err := ParseVersion(versionText)
    if err != nil {
        return nil, err
    }
    cmp := from.Compare(CurrentSchemaVersion)
    if cmp > 0 {
        return nil, fmt.Errorf("config_version %s is newer than Runtime %s", from, CurrentSchemaVersion)
    }
    if cmp == 0 {
        return raw, nil
    }

    migrated, err := Migrate(raw, from, CurrentSchemaVersion)
    if err != nil {
        return nil, err
    }
    logger.Info("config migrated", "from", from.String(), "to", CurrentSchemaVersion.String())
    return migrated, nil
}
```

完整顺序固定为：

```text
resolve path
  → cfg := Default()
  → parse to map
  → migrate raw map
  → expand ${VAR} / ${VAR:-default}
  → ApplyElementDefaults(raw)
  → DecodeInto(presence-aware, reject unknown)
  → apply CLI flags
  → Validate
```

迁移失败不写回磁盘，也不启动 Runtime。

## 4. 备份与 CLI

自动启动迁移只改变内存中的 raw Map。`yaa config migrate --backup` 才写回文件：先把原文件复制为 `<path>.bak`，使用临时文件写新内容、`fsync` 后 `Rename` 替换；配置文件权限保持 `0600`，备份也保持 `0600`。`--dry-run` 只输出变更摘要，不写任何文件。

```bash
yaa config migrate --dry-run --config ./yaa.yaml
yaa config migrate --backup --config ./yaa.yaml
```

## 5. 别名与废弃字段

别名是迁移函数的一部分，不维护一个会绕过版本图的全局 alias Map。每条迁移必须声明旧路径、新路径、来源版本和目标版本，并在日志中记录。当前唯一规划中的别名是 `runtime.http.addr -> runtime.api.http.addr`；`runtime.http.port`、`runtime.log.*` 等未在 schema 中定义的路径不得自动猜测。

旧字段在迁移前可输出一次 `WARN`，迁移后若仍残留则按未知字段错误处理。新字段优先级高于旧字段，冲突时拒绝迁移而不是静默覆盖。

---

*最后更新: 2025-07-17*
