// Package plugin Manager: 依赖图、启用决策、生命周期和关闭.
// docs/plugin/manager.md: 全流程.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/exp/slog"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/tool"
)

// Manager 管理 Plugin 发现、依赖图、启用决策和生命周期.
// docs/plugin/manager.md §1.
// ponytail: Client/Handle/ProxyNames/monitor/teardown 等 RPC 相关字段后续补,
// 当前只实现发现结果合并 + 配置 entries 合并 + 依赖图 + enabled 决策.
type Manager struct {
	loader               *Loader
	tools                *tool.Manager
	entries              map[string]*Entry
	discoveryDiagnostics []error
	config               config.PluginsConfig
	runtimeVersion       string
	runCtx               context.Context
	runCancel            context.CancelFunc
	stopping             atomic.Bool
	lifecycleMu          sync.Mutex // 关闭启动 gate, 禁止 wg.Add 与 wg.Wait 竞态
	mu                   sync.RWMutex
	logger               *slog.Logger
	wg                   sync.WaitGroup
	stopOnce             sync.Once
	stopDone             chan struct{}
	stopErr              error
}

// NewManager 发现 Plugin 并合并配置 entries. docs/plugin/manager.md §1.
// ctx 应是 Runtime 级别 context, 用于后续 Health/monitor goroutine.
// tools 当前可为 nil (MVP 阶段不需要 Proxy 注册); 后续 RPC 启动阶段需要非 nil.
func NewManager(
	ctx context.Context,
	cfg config.PluginsConfig,
	loader *Loader,
	tools *tool.Manager,
	logger *slog.Logger,
) (*Manager, error) {
	if ctx == nil || loader == nil || logger == nil {
		return nil, errors.New("plugin manager: nil dependency")
	}
	runCtx, cancel := context.WithCancel(ctx)
	m := &Manager{
		loader:    loader,
		tools:     tools,
		entries:   make(map[string]*Entry),
		config:    cfg,
		runCtx:    runCtx,
		runCancel: cancel,
		logger:    logger,
		stopDone:  make(chan struct{}),
	}

	descriptors, diagnostics := loader.Discover()
	for _, d := range descriptors {
		m.entries[d.Manifest.ID] = &Entry{
			Descriptor: d,
			State:      StateDiscovered,
			Health:     HealthStatus{Level: HealthLevelUnknown},
			Config:     map[string]any{},
		}
	}
	for _, diag := range diagnostics {
		m.discoveryDiagnostics = append(m.discoveryDiagnostics, diag)
		if diag.PluginID == "" {
			continue
		}
		e := m.entries[diag.PluginID]
		if e == nil {
			e = &Entry{
				State:  StateError,
				Health: HealthStatus{Level: HealthLevelUnknown},
				Config: map[string]any{},
			}
			if diag.Descriptor != nil {
				e.Descriptor = *diag.Descriptor
			}
			m.entries[diag.PluginID] = e
		}
		e.State = StateError
		e.LastError = errors.Join(e.LastError, diag)
	}
	for _, configured := range cfg.Entries {
		e := m.entries[configured.ID]
		if e == nil {
			err := fmt.Errorf("%w: %s", ErrPluginNotFound, configured.ID)
			m.discoveryDiagnostics = append(m.discoveryDiagnostics, err)
			e = &Entry{
				State:     StateError,
				LastError: err,
				Health:    HealthStatus{Level: HealthLevelUnknown},
				Config:    map[string]any{},
			}
			m.entries[configured.ID] = e
		}
		e.Enabled = configured.Enabled
		e.Config = clonePluginConfig(configured.Config)
	}
	return m, nil
}

// clonePluginConfig 递归复制 map[string]any, 避免 caller 修改嵌套值.
// docs/plugin/manager.md §1.
func clonePluginConfig(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = clonePluginConfigValue(value)
	}
	return dst
}

func clonePluginConfigValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return clonePluginConfig(v)
	case []any:
		cloned := make([]any, len(v))
		for i, item := range v {
			cloned[i] = clonePluginConfigValue(item)
		}
		return cloned
	default:
		return value
	}
}

// EntrySnapshot 是对外暴露的只读 Entry 总结.
type EntrySnapshot struct {
	ID       string
	State    PluginState
	Health   HealthStatus
	Enabled  *bool
	HasError bool
	Manifest Manifest
}

// Entries 返回当前所有 Plugin Entry 的 snapshot (只读).
// docs/plugin/manager.md §6: 对外汇总在 mu.RLock 下复制.
func (m *Manager) Entries() map[string]EntrySnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]EntrySnapshot, len(m.entries))
	for id, e := range m.entries {
		result[id] = EntrySnapshot{
			ID:       id,
			State:    e.State,
			Health:   e.Health,
			Enabled:  e.Enabled,
			HasError: e.LastError != nil,
			Manifest: e.Descriptor.Manifest,
		}
	}
	return result
}

// DiscoveryDiagnostics 返回发现阶段的诊断错误列表.
func (m *Manager) DiscoveryDiagnostics() []error {
	return m.discoveryDiagnostics
}
