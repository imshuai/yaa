# Plugin Loader 详解

## 概述

Plugin Loader 是 Yaa! 插件系统的核心组件，负责在运行时动态加载、验证和管理外部插件。它基于 Go 的 `plugin` 包实现原生插件加载，同时提供签名验证、版本兼容检查和加载失败处理等安全机制。

## 目录结构

```
plugin/
├── loader.go          # 插件加载器
├── manager.go         # 插件管理器
├── signature.go       # 签名验证
├── version.go         # 版本兼容检查
└── types.go           # 插件接口定义
```

## Go plugin 加载机制

Go 标准库 `plugin` 包提供了动态加载 `.so`（Linux/macOS）和 `.dll`（Windows，需第三方库）共享库的能力。

### 编译插件

插件需要编译为共享库格式，使用 `-buildmode=plugin` 标志：

```bash
# Linux / macOS
go build -buildmode=plugin -o myplugin.so ./myplugin

# Windows（需借助第三方方案，如 golang.org/x/sys/windows）
# Yaa! 推荐使用 Go 1.23+ 的 plugin Windows 支持
go build -buildmode=plugin -o myplugin.dll ./myplugin
```

### 插件入口约定

每个插件必须导出一个符合 `PluginFactory` 签名的函数：

```go
// 插件代码（编译为 .so/.dll 的模块）
package main

import "github.com/imshuai/yaa/plugin"

// Plugin 必须导出的工厂函数
var Plugin = func() plugin.Plugin {
    return &MyPlugin{}
}

type MyPlugin struct{}

func (p *MyPlugin) Init(ctx plugin.Context) error {
    return nil
}

func (p *MyPlugin) Name() string {
    return "my-plugin"
}

func (p *MyPlugin) Version() string {
    return "1.0.0"
}

func (p *MyPlugin) Shutdown() error {
    return nil
}
```

## Loader 核心实现

```go
package plugin

import (
    "fmt"
    "os"
    "plugin"
    "runtime"
)

// PluginFactory 插件工厂函数签名
type PluginFactory func() Plugin

// Loader 插件加载器
type Loader struct {
    signatureVerifier *SignatureVerifier
    versionChecker   *VersionChecker
}

// NewLoader 创建插件加载器
func NewLoader(sv *SignatureVerifier, vc *VersionChecker) *Loader {
    return &Loader{
        signatureVerifier: sv,
        versionChecker:    vc,
    }
}

// Load 从指定路径加载插件
func (l *Loader) Load(path string) (Plugin, error) {
    // 1. 检查文件是否存在
    if _, err := os.Stat(path); err != nil {
        return nil, fmt.Errorf("plugin file not found: %s, %w", path, err)
    }

    // 2. 签名验证
    if l.signatureVerifier != nil {
        if err := l.signatureVerifier.Verify(path); err != nil {
            return nil, fmt.Errorf("signature verification failed: %w", err)
        }
    }

    // 3. 打开共享库
    so, err := plugin.Open(path)
    if err != nil {
        return nil, fmt.Errorf("failed to open plugin: %w", err)
    }

    // 4. 查找 Plugin 工厂符号
    sym, err := so.Lookup("Plugin")
    if err != nil {
        return nil, fmt.Errorf("Plugin symbol not found: %w", err)
    }

    factory, ok := sym.(PluginFactory)
    if !ok {
        return nil, fmt.Errorf("Plugin symbol is not a valid PluginFactory")
    }

    // 5. 实例化插件
    p := factory()

    // 6. 版本兼容检查
    if l.versionChecker != nil {
        if err := l.versionChecker.Check(p.Version()); err != nil {
            return nil, fmt.Errorf("version incompatible: %w", err)
        }
    }

    return p, nil
}
```

## 签名验证

为防止恶意插件加载，Yaa! 支持对插件文件进行数字签名验证。

```go
package plugin

import (
    "crypto/ed25519"
    "crypto/sha256"
    "io"
    "os"
)

// SignatureVerifier 签名验证器
type SignatureVerifier struct {
    publicKey ed25519.PublicKey
    enabled   bool
}

// Verify 验证插件文件签名
func (sv *SignatureVerifier) Verify(path string) error {
    if !sv.enabled {
        return nil // 签名验证未启用，跳过
    }

    // 读取插件文件
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()

    // 计算哈希
    hasher := sha256.New()
    if _, err := io.Copy(hasher, file); err != nil {
        return err
    }
    hash := hasher.Sum(nil)

    // 读取签名文件（.sig）
    sigPath := path + ".sig"
    sig, err := os.ReadFile(sigPath)
    if err != nil {
        return fmt.Errorf("signature file not found: %w", err)
    }

    // 验证签名
    if !ed25519.Verify(sv.publicKey, hash, sig) {
        return fmt.Errorf("invalid plugin signature")
    }

    return nil
}
```

## 版本兼容检查

Yaa! 使用语义化版本（Semantic Versioning）进行兼容性检查。

```go
package plugin

import (
    "fmt"
    "strings"
)

// VersionChecker 版本兼容检查器
type VersionChecker struct {
    minVersion string // 最低支持版本
    maxVersion string // 最高支持版本（可选）
}

// Check 检查插件版本是否兼容
func (vc *VersionChecker) Check(pluginVersion string) error {
    cmp := compareVersions(pluginVersion, vc.minVersion)
    if cmp < 0 {
        return fmt.Errorf("plugin version %s is lower than required %s",
            pluginVersion, vc.minVersion)
    }

    if vc.maxVersion != "" {
        cmp = compareVersions(pluginVersion, vc.maxVersion)
        if cmp > 0 {
            return fmt.Errorf("plugin version %s is higher than maximum %s",
                pluginVersion, vc.maxVersion)
        }
    }

    return nil
}

// compareVersions 比较两个语义化版本号
func compareVersions(a, b string) int {
    pa := strings.Split(a, ".")
    pb := strings.Split(b, ".")
    for i := 0; i < 3; i++ {
        va := atoiSafe(pa[i])
        vb := atoiSafe(pb[i])
        if va < vb {
            return -1
        } else if va > vb {
            return 1
        }
    }
    return 0
}
```

## 加载失败处理

| 错误类型 | 错误原因 | 处理策略 |
|---------|---------|---------|
| `FileNotFound` | 插件文件路径不存在 | 返回错误，记录日志，跳过加载 |
| `SignatureInvalid` | 签名验证失败 | 拒绝加载，记录安全告警 |
| `OpenFailed` | `plugin.Open` 失败 | 检查 Go 版本兼容性，提示重新编译 |
| `SymbolNotFound` | 未找到 `Plugin` 导出符号 | 提示插件代码不符合规范 |
| `TypeMismatch` | 工厂函数类型不匹配 | 提示插件接口版本不一致 |
| `VersionIncompatible` | 插件版本不在兼容范围 | 提示升级或降级插件 |
| `InitFailed` | 插件 `Init()` 返回错误 | 记录错误，标记为加载失败 |

### 错误处理示例

```go
func (l *Loader) LoadWithRetry(path string, maxRetries int) (Plugin, error) {
    var lastErr error
    for i := 0; i < maxRetries; i++ {
        p, err := l.Load(path)
        if err == nil {
            if initErr := p.Init(ctx); initErr != nil {
                lastErr = fmt.Errorf("plugin init failed: %w", initErr)
                continue
            }
            return p, nil
        }
        lastErr = err

        // 不可恢复的错误，直接返回
        if isFatalError(err) {
            return nil, err
        }
        time.Sleep(time.Duration(i+1) * time.Second)
    }
    return nil, fmt.Errorf("load failed after %d retries: %w", maxRetries, lastErr)
}

func isFatalError(err error) bool {
    switch {
    case strings.Contains(err.Error(), "signature"):
        return true
    case strings.Contains(err.Error(), "version"):
        return true
    default:
        return false
    }
}
```

## 平台兼容性说明

| 平台 | 共享库格式 | 支持状态 | 备注 |
|------|-----------|---------|------|
| Linux | `.so` | ✅ 原生支持 | Go `plugin` 标准包 |
| macOS | `.so` | ✅ 原生支持 | Go `plugin` 标准包 |
| Windows | `.dll` | ⚠️ 实验性 | 需 Go 1.23+ 或第三方方案 |
| FreeBSD | `.so` | ✅ 原生支持 | Go `plugin` 标准包 |

> **注意**：Go plugin 要求宿主程序与插件使用相同的 Go 版本和依赖版本编译，否则会触发 `plugin was built with a different version of package` 错误。Yaa! 建议在 CI/CD 流程中统一编译插件。
