# 配置迁移与兼容

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/config/migration.md`
> 依赖: `docs/architecture.md` §3.12, §7, §8

---

## 1. 概述

Yaa! 遵循 **向后兼容优先** 原则。配置文件在版本升级时应尽可能无需手动修改即可继续使用。当不可避免的破坏性变更发生时，通过版本化机制和迁移工具平滑过渡。

### 1.1 核心原则

| 原则 | 说明 |
|------|------|
| **向后兼容** | 新版本能直接加载旧版本配置，新增字段使用默认值 |
| **版本化** | 配置文件通过 `version` 字段标识 schema 版本 |
| **渐进式废弃** | 废弃字段先标记 `deprecated`，至少保留一个 LTS 周期后移除 |
| **自动迁移** | 提供内置迁移工具，自动升级配置格式 |
| **透明告警** | 废弃字段使用时输出 Warning 日志，不中断运行 |

---

## 2. 配置版本化

### 2.1 版本字段

每个配置文件应在根级别声明 `version` 字段：

```yaml
# yaa.yaml
version: "1.2"          # schema 版本，格式: major.minor
runtime:
  api:
    http:
      addr: ":8080"
```

**版本规则：**

| 变更类型 | 版本变化 | 兼容性 | 示例 |
|----------|----------|--------|------|
| 新增字段 | minor +1 | 完全兼容 | `1.0` → `1.1` |
| 字段重命名 | major +1 | 需迁移 | `1.2` → `2.0` |
| 字段移除 | major +1 | 需迁移 | `1.5` → `2.0` |
| 默认值调整 | minor +1 | 兼容 | `1.1` → `1.2` |
| 结构重组 | major +1 | 需迁移 | `1.8` → `2.0` |

### 2.2 版本解析

```go
package config

import "fmt"

// ConfigSchema 版本信息
type ConfigSchema struct {
    Major int `yaml:"major"`
    Minor int `yaml:"minor"`
}

// ParseVersion 从 "1.2" 解析为 ConfigSchema
func ParseVersion(v string) (ConfigSchema, error) {
    var s ConfigSchema
    if _, err := fmt.Sscanf(v, "%d.%d", &s.Major, &s.Minor); err != nil {
        return s, fmt.Errorf("invalid version format: %s", v)
    }
    return s, nil
}

// IsCompatible 判断目标版本是否与当前版本兼容（同 major）
func (s ConfigSchema) IsCompatible(target ConfigSchema) bool {
    return s.Major == target.Major
}
```

---

## 3. 迁移工具

### 3.1 迁移命令

Yaa! CLI 内置配置迁移命令：

```bash
# 检查配置是否需要迁移（dry-run）
yaa config migrate --dry-run

# 执行迁移，备份原文件
yaa config migrate --backup

# 迁移到指定版本
yaa config migrate --target 2.0

# 批量迁移目录下所有配置
yaa config migrate --dir /etc/yaa/ --recursive
```

### 3.2 迁移注册表

```go
package config

import "fmt"

// MigrationFunc 单个版本的迁移函数
type MigrationFunc func(map[string]any) (map[string]any, error)

// migrationRegistry 注册所有迁移路径
var migrationRegistry = map[string]MigrationFunc{
    "1.0->1.1": migrateV10ToV11,
    "1.1->1.2": migrateV11ToV12,
    "1.2->2.0": migrateV12ToV20,
}

// Migrate 逐步迁移配置到目标版本
func Migrate(raw map[string]any, from, to ConfigSchema) (map[string]any, error) {
    result := raw
    current := from

    for current.LessThan(to) {
        key := fmt.Sprintf("%d.%d->%d.%d",
            current.Major, current.Minor,
            nextVersion(current).Major, nextVersion(current).Minor)

        fn, ok := migrationRegistry[key]
        if !ok {
            return nil, fmt.Errorf("no migration path from %s", key)
        }

        migrated, err := fn(result)
        if err != nil {
            return nil, fmt.Errorf("migration %s failed: %w", key, err)
        }
        result = migrated
        current = nextVersion(current)
    }

    result["version"] = fmt.Sprintf("%d.%d", to.Major, to.Minor)
    return result, nil
}
```

### 3.3 迁移函数示例

```go
// migrateV12ToV20: 1.2 → 2.0
// 变更: runtime.http.addr → runtime.api.http.addr
func migrateV12ToV20(raw map[string]any) (map[string]any, error) {
    runtime, ok := raw["runtime"].(map[string]any)
    if !ok {
        return raw, nil // 无需迁移
    }

    // 旧字段 http.addr 迁移到新结构 api.http.addr
    if http, ok := runtime["http"].(map[string]any); ok {
        api, _ := runtime["api"].(map[string]any)
        if api == nil {
            api = make(map[string]any)
        }
        if apiHTTP, _ := api["http"].(map[string]any); apiHTTP == nil {
            api["http"] = http
        }
        api["http"] = http
        runtime["api"] = api
        delete(runtime, "http")
        raw["runtime"] = runtime
    }

    return raw, nil
}
```

---

## 4. 废弃字段策略

### 4.1 废弃生命周期

```text
┌─────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────┐
│  Active  │ →  │  Deprecated  │ →  │  Deprecated+  │ →  │ Removed  │
│          │    │  (Warning)   │    │  Error        │    │          │
└─────────┘    └──────────────┘    └──────────────┘    └──────────┘
   当前版本       下一 minor          下一 major          下下 major
```

| 阶段 | 行为 | 日志级别 | 运行影响 |
|------|------|----------|----------|
| Active | 正常使用 | 无 | 无 |
| Deprecated | 可用，建议迁移 | `WARN` | 无 |
| Deprecated+Error | 可用但报错 | `ERROR` | 无（仍继续运行） |
| Removed | 解析失败 | — | 启动失败 |

### 4.2 废弃声明

```go
package config

// DeprecatedField 记录废弃字段信息
type DeprecatedField struct {
    OldPath     string // 旧字段路径，如 "runtime.http.addr"
    NewPath     string // 新字段路径，如 "runtime.api.http.addr"
    Since       string // 废弃起始版本
    RemoveIn    string // 计划移除版本
    Reason      string // 废弃原因
}

var deprecatedFields = []DeprecatedField{
    {
        OldPath:  "runtime.http.addr",
        NewPath:  "runtime.api.http.addr",
        Since:    "1.2",
        RemoveIn: "2.0",
        Reason:   "HTTP 配置移入 api 子结构，统一管理",
    },
}

// CheckDeprecated 扫描配置中的废弃字段并输出告警
func CheckDeprecated(raw map[string]any) []string {
    var warnings []string
    for _, d := range deprecatedFields {
        if hasPath(raw, d.OldPath) {
            warnings = append(warnings, fmt.Sprintf(
                "[DEPRECATED] %s is deprecated since v%s, "+
                    "use %s instead. Will be removed in v%s. Reason: %s",
                d.OldPath, d.Since, d.NewPath, d.RemoveIn, d.Reason,
            ))
        }
    }
    return warnings
}
```

---

## 5. 向后兼容规则

### 5.1 兼容性矩阵

| 变更场景 | 兼容策略 | 用户操作 |
|----------|----------|----------|
| 新增可选字段 | 自动使用默认值 | 无需修改 |
| 新增必填字段 | 必须有合理默认值 | 无需修改 |
| 字段重命名 | 旧名保留为别名，旧名优先 | 建议迁移 |
| 字段移除 | 经过完整废弃周期后移除 | 迁移后删除旧字段 |
| 结构嵌套调整 | 迁移工具自动重组 | 运行 `migrate` |
| 枚举值新增 | 旧值保持有效 | 无需修改 |
| 枚举值移除 | 旧值映射到最接近的新值 | 检查日志告警 |

### 5.2 加载时兼容处理

```go
// LoadConfig 加载配置时自动处理兼容性
func LoadConfig(path string) (*Config, error) {
    raw, err := loadRawConfig(path)
    if err != nil {
        return nil, err
    }

    // 1. 解析版本
    versionStr, _ := raw["version"].(string)
    if versionStr == "" {
        versionStr = "1.0" // 无版本号视为 1.0
    }
    schema, err := ParseVersion(versionStr)
    if err != nil {
        return nil, err
    }

    // 2. 检查废弃字段（输出 Warning）
    for _, w := range CheckDeprecated(raw) {
        log.Warn(w)
    }

    // 3. 自动应用别名兼容
    applyAliases(raw)

    // 4. 版本不匹配时提示迁移
    if !schema.IsCompatible(CurrentSchemaVersion) {
        log.Warnf("config version %s is outdated, "+
            "run 'yaa config migrate' to upgrade", versionStr)
    }

    // 5. 反序列化为最终配置结构
    var cfg Config
    if err := decode(raw, &cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

### 5.3 别名兼容

```go
// aliasMap 旧字段名 → 新字段名的映射
var aliasMap = map[string]string{
    "runtime.http.addr":  "runtime.api.http.addr",
    "runtime.http.port":  "runtime.api.http.port",
    "log.level":         "runtime.log.level",
}

// applyAliases 将旧字段值复制到新字段路径（仅当新路径不存在时）
func applyAliases(raw map[string]any) {
    for oldPath, newPath := range aliasMap {
        if val, ok := getPath(raw, oldPath); ok {
            if !hasPath(raw, newPath) {
                setPath(raw, newPath, val)
            }
        }
    }
}
```

---

## 6. 最佳实践

1. **永远不要静默删除用户配置** — 废弃字段至少保留一个 major 周期
2. **迁移工具先 dry-run** — 让用户预览变更再确认
3. **备份优先** — 迁移前自动备份原文件到 `.bak`
4. **日志驱动** — 废弃告警写入结构化日志，方便 grep 和监控
5. **版本号写在配置里** — 不依赖外部推断，配置自描述
6. **迁移路径线性** — 不跳跃版本，逐步迁移每一步有注册函数

---

*最后更新: 2025-07-17*
