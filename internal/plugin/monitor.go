// Package plugin monitor: 进程退出监听 + Health 更新.
// docs/plugin/manager.md §4: Run-time monitoring staff in manager.md; Restart 在 §4.
package plugin

import (
	"context"
	"time"

)

// monitor 是 StartAll 为每个 Ready plugin 启动的 goroutine.
// docs/plugin/manager.md §4: 监听 Exited + health ticker + runCtx.Done().
// ponytail: MVP monitor 先只关注 Exited + 简单 Health; restart 留到 Phase 4.4进 一步实 - InStances的 详细 Phase 5.
func (m *Manager) monitor(e *Entry) {
	defer m.wg.Done()
	if e == nil || e.Client == nil {
		return
	}
	client := e.Client
	// 健康检查 ticker
	healthInterval := m.config.HealthInterval
	if healthInterval <= 0 {
		healthInterval = 30 * time.Second
	}
	healthTimeout := m.config.HealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = 5 * time.Second
	}
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-client.Exited:
			// 进程退出 → 测试 stopping?
			m.lifecycleMu.Lock()
			stopping := m.stopping.Load()
			m.lifecycleMu.Unlock()
			if stopping {
				m.logger.Info("plugin.process_exit",
					"plugin_id", e.Descriptor.Manifest.ID,
					"version", e.Descriptor.Manifest.Version,
					"protocol_version", e.Descriptor.Manifest.ProtocolVersion,
					"kind", "stopping",
				)
				return
			}
			exitErr := client.WaitErr()
			m.logger.Error("plugin.process_exit",
				exitErr,
				"plugin_id", e.Descriptor.Manifest.ID,
				"version", e.Descriptor.Manifest.Version,
				"protocol_version", e.Descriptor.Manifest.ProtocolVersion,
				"kind", "unexpected",
			)
			// 失效 Proxy handle, 让 in-flight 直接 unavailable.
			m.mu.Lock()
			if e.Handle != nil {
				e.Handle.Invalidate(client)
			}
			e.Client = nil
			e.State = StateError
			e.LastError = ErrPluginUnavailable
			m.mu.Unlock()
			// 关闭旧 RPC transport + cleanup endpoint (KillAndWait is 已 done, 但 cleanup endpoint)
			_ = client.CloseTransport()
			client.CleanupEndpoint()
			// TODO: restart 但 - Phase 4.5; 当前 MVP 进入 error 状态等待 Runtime 重启.
			return
		case <-ticker.C:
			// Health 调用 - 带 timeout.
			ctx, cancel := context.WithTimeout(m.runCtx, healthTimeout)
			h, err := client.Health(ctx)
			cancel()
			m.mu.Lock()
			if err != nil {
				// 失败只标 degraded, 不 kill.
				if e.Health.Level != HealthLevelUnhealthy {
					e.Health = HealthStatus{
						Level:     HealthLevelDegraded,
						Message:   err.Error(),
						Timestamp: time.Now(),
					}
				}
			} else {
				e.Health = HealthStatus{
					Level:     h.Level,
					Message:   h.Message,
					Timestamp: h.Timestamp,
				}
			}
			m.mu.Unlock()
		case <-m.runCtx.Done():
			return
		}
	}
}
