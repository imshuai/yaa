// Package plugin 类型定义. docs/plugin/config-ref.md §2 + loader.md §2 + interface.md §5.
package plugin

import (
	"time"
)

// Manifest 是 plugin.yaml 的 Go 表示. docs/plugin/config-ref.md §2.
type Manifest struct {
	ID              string                 `yaml:"id" json:"id"`
	DisplayName     string                 `yaml:"display_name" json:"display_name"`
	Description     string                 `yaml:"description" json:"description"`
	Version         string                 `yaml:"version" json:"version"`
	ProtocolVersion string                 `yaml:"protocol_version" json:"protocol_version"`
	RequiresRuntime string                 `yaml:"requires_runtime" json:"requires_runtime"`
	Entry           string                 `yaml:"entry" json:"entry"`
	DefaultEnabled  bool                   `yaml:"default_enabled" json:"default_enabled"`
	Dependencies    []Dependency           `yaml:"dependencies" json:"dependencies"`
	Provides        []CapabilityDescriptor `yaml:"provides" json:"provides"`
	ConfigSchema    map[string]any         `yaml:"config_schema" json:"config_schema"`
}

// Dependency 是 Manifest 的依赖声明.
type Dependency struct {
	ID       string `yaml:"id" json:"id"`
	Version  string `yaml:"version" json:"version"`
	Optional bool   `yaml:"optional" json:"optional"`
}

// CapabilityDescriptor 描述 Plugin 提供的能力.
// docs/plugin/interface.md §2: v1 只有 type=tool.
type CapabilityDescriptor struct {
	Type        string         `yaml:"type" json:"type"`
	Name        string         `yaml:"name" json:"name"`
	Description string         `yaml:"description" json:"description"`
	Schema      map[string]any `yaml:"schema" json:"schema"`
}

// PluginDescriptor 是 Loader 发现一个 Plugin 后的冻结描述符. docs/plugin/loader.md §2.
type PluginDescriptor struct {
	ManifestPath string   // plugin.yaml 的绝对路径
	EntryPath    string   // entry 可执行文件的绝对路径
	Manifest     Manifest // 解析后的 Manifest
}

// DiscoveryDiagnostic 记录发现阶段的错误.
// docs/plugin/loader.md §2.
type DiscoveryDiagnostic struct {
	PluginID   string            // 无法恢复 ID 时为空
	Descriptor *PluginDescriptor // 已解析出 ID 时保留部分 Descriptor
	Err        error             // 始终非 nil
}

func (d DiscoveryDiagnostic) Error() string { return d.Err.Error() }
func (d DiscoveryDiagnostic) Unwrap() error { return d.Err }

// PluginState 是 Plugin 的运行时状态. docs/plugin/manager.md §1.
type PluginState string

const (
	StateDiscovered PluginState = "discovered"
	StateStarting   PluginState = "starting"
	StateReady      PluginState = "ready"
	StateError      PluginState = "error"
	StateStopped    PluginState = "stopped"
)

// HealthStatus 是 Plugin Health RPC 的响应映射.
// docs/plugin/interface.md §5: healthy/degraded/unhealthy/unknown.
// docs/plugin/observability.md §2: manager.md Entry.Health 用 struct.
type HealthStatus struct {
	Level     string    `json:"level"` // healthy | degraded | unhealthy | unknown
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// HealthResponse 是 Health RPC 的响应.
type HealthResponse struct {
	Level     string    // healthy | degraded | unhealthy
	Message   string
	Timestamp time.Time
}

// 健康级别常量, docs/plugin/interface.md §5.
const (
	HealthLevelHealthy   = "healthy"
	HealthLevelDegraded  = "degraded"
	HealthLevelUnhealthy = "unhealthy"
	HealthLevelUnknown   = "unknown"
)

// ReadyResponse 是 Ready RPC 的响应, 包含能力列表.
type ReadyResponse struct {
	Capabilities []CapabilityDescriptor
}

// HandshakeResponse 是 Handshake RPC 的响应.
type HandshakeResponse struct {
	ProtocolVersion string
	PluginID        string
	PluginVersion   string
	StartupNonce    string
}

// ToolRequest 是 InvokeTool RPC 的请求.
type ToolRequest struct {
	Name string
	Args map[string]any
}

// ToolResponse 是 InvokeTool RPC 的响应.
type ToolResponse struct {
	Result map[string]any
}

// Entry 是 Manager 侧的单个 Plugin 条目. docs/plugin/manager.md §1.
type Entry struct {
	Descriptor PluginDescriptor
	State      PluginState
	Health     HealthStatus
	Config     map[string]any
	Enabled    *bool
	StartedAt  time.Time
	LastError  error
}
