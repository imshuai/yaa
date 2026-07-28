// Package plugin 实现进程外 Plugin 的发现、校验和生命周期管理.
// docs/plugin/README.md §1: Plugin 是独立可执行进程, 通过版本化 RPC 与 Runtime 通信.
package plugin

import "errors"

// 稳定错误, docs/plugin/errors.md §1.
var (
	ErrPluginNotFound             = errors.New("plugin not found")
	ErrPluginManifestInvalid      = errors.New("invalid plugin manifest")
	ErrPluginConfigInvalid        = errors.New("invalid plugin config")
	ErrPluginDependencyMissing    = errors.New("plugin dependency missing")
	ErrPluginCircularDependency   = errors.New("plugin circular dependency")
	ErrPluginRuntimeIncompatible  = errors.New("runtime version incompatible")
	ErrPluginProcessStart         = errors.New("plugin process start failed")
	ErrPluginConnectionTimeout    = errors.New("plugin connection timeout")
	ErrPluginProtocolIncompatible = errors.New("plugin protocol incompatible")
	ErrPluginInitFailed           = errors.New("plugin init failed")
	ErrPluginCapabilityConflict   = errors.New("plugin capability conflict")
	ErrPluginCallFailed           = errors.New("plugin call failed")
	ErrPluginCallTimeout          = errors.New("plugin call timeout")
	ErrPluginUnavailable          = errors.New("plugin unavailable")
	ErrPluginPermissionDenied     = errors.New("plugin permission denied")
)

// 旧名兼容别名 — Manifest 校验阶段使用的内部细分 error, 不出现在 errors.md 稳定表中,
// 但 manifest.go 逻辑需要区分具体原因. 用 fmt.Errorf("%w: ...") 包装上面的稳定 error.
var (
	ErrPluginManifestNotFound  = errors.New("plugin: manifest not found")
	ErrPluginEntryNotFound     = errors.New("plugin: entry executable not found")
	ErrPluginEntryEscape       = errors.New("plugin: entry path escapes manifest directory")
	ErrPluginDuplicateID       = errors.New("plugin: duplicate plugin id")
	ErrPluginUnknownCapability = errors.New("plugin: unknown capability type")
	ErrPluginDependencyVersion = errors.New("plugin: dependency version mismatch")
)
