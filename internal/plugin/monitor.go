// Package plugin monitor: 进程退出监听 + Health 更新 + restart 退避.
// docs/plugin/manager.md §4.
package plugin

import (
	"context"
	"time"
)

// monitor 是 StartAll 为每个 Ready plugin 启动的 goroutine.
// docs/plugin/manager.md §4: 监听当前 Exited + health ticker + runCtx.Done().
// restart 执行 exec/Dial/Handshake/Init/Ready, 成功后原子替换 handle.
// 请求不自动 replay (in-flight 调用方已经收到 unavailable).
func (m *Manager) monitor(e *Entry) {
	defer m.wg.Done()
	if e == nil || e.Client == nil {
		return
	}
	// 健康检查参数
	healthInterval := nonNegDuration(m.config.HealthInterval, 30*time.Second)
	healthTimeout := nonNegDuration(m.config.HealthTimeout, 5*time.Second)
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()

	for {
		client := m.currentClient(e)
		if client == nil {
			// 当前无 client — 已 Invalidate 或 stopping. 退出.
			return
		}
		select {
		case <-client.Exited:
			// 进程退出:
			// 1. 先做 stop 检查
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
			// 2. 失效 Proxy - in-flight 立即 ErrPluginUnavailable.
			m.mu.Lock()
			if e.Handle != nil {
				e.Handle.Invalidate(client)
			}
			e.Client = nil
			m.mu.Unlock()
			// 3. 清理旧 endpoint + transport (process already exited).
			_ = client.CloseTransport()
			client.CleanupEndpoint()
			// 4. restart 退避策略.
			if !m.config.Restart.Enabled {
				m.mu.Lock()
				e.State = StateError
				e.LastError = ErrPluginUnavailable
				m.mu.Unlock()
				return
			}
			if !m.retryRestart(e, client) {
				m.mu.Lock()
				e.State = StateError
				e.LastError = ErrPluginUnavailable
				m.mu.Unlock()
				return
			}
			// 成功: 切换到新 client 继续 monitor 循环.
			ticker.Reset(healthInterval)
		case <-ticker.C:
			// Health 调用 - 带 timeout. 失败只标 degraded, 不 kill.
			ctx, cancel := context.WithTimeout(m.runCtx, healthTimeout)
			h, err := client.Health(ctx)
			cancel()
			m.mu.Lock()
			if err != nil {
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

// currentClient 返回 e 当前持有的 client 指针 (在 mu.RLock 下读).
func (m *Manager) currentClient(e *Entry) *RPCClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return e.Client
}

// retryRestart 按 restart.config 尝试有限退避重启.
// 成功后将 e.Client 替换为新 Client, 并 handle.Store(newClient), 返回 true.
// 失败 (上限耗尽 or 启动 fail) 返回 false.
// docs/plugin/manager.md §4.
func (m *Manager) retryRestart(e *Entry, oldClient *RPCClient) bool {
	maxAttempts := m.config.Restart.MaxAttempts
	backoff := nonNegDuration(m.config.Restart.Backoff, time.Second)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// 在开始退避、调度 Loader 前、发布前检查 stop gate.
		m.lifecycleMu.Lock()
		stopping := m.stopping.Load()
		m.lifecycleMu.Unlock()
		if stopping {
			return false
		}
		// 退避等待.
		if attempt > 0 {
			// 指数退避: backoff * 2^(attempt-1), 最大 60s.
			d := backoff * time.Duration(1<<(attempt-1))
			if d > 60*time.Second {
				d = 60 * time.Second
			}
			select {
			case <-time.After(d):
			case <-m.runCtx.Done():
				return false
			}
		}
		// 发布前再检查 gate.
		m.lifecycleMu.Lock()
		if m.stopping.Load() {
			m.lifecycleMu.Unlock()
			return false
		}
		m.lifecycleMu.Unlock()
		m.logger.Info("plugin.restarting",
			"plugin_id", e.Descriptor.Manifest.ID,
			"attempt", attempt+1,
		)
		startCtx, cancel := context.WithTimeout(m.runCtx, m.config.StartupTimeout)
		newClient, err := m.loader.Start(startCtx, e.Descriptor, e.Config)
		cancel()
		if err != nil {
			m.logger.Error("plugin.restart_failed",
				err,
				"plugin_id", e.Descriptor.Manifest.ID,
				"attempt", attempt+1,
			)
			continue // 再试
		}
		// 成功: 原子 handle.Store.
		m.lifecycleMu.Lock()
		if m.stopping.Load() {
			m.lifecycleMu.Unlock()
			_ = newClient.Terminate()
			return false
		}
		m.mu.Lock()
		if !e.Handle.Invalidate(oldClient) {
			// handle 已被其他 goroutine 替换 — Term 这个 newClient 防泄漏.
			m.mu.Unlock()
			m.lifecycleMu.Unlock()
			_ = newClient.Terminate()
			return false
		}
		e.Client = newClient
		e.Handle.Store(newClient)
		e.State = StateReady
		e.StartedAt = time.Now()
		e.LastError = nil
		m.mu.Unlock()
		m.lifecycleMu.Unlock()
		m.logger.Info("plugin.ready",
			"plugin_id", e.Descriptor.Manifest.ID,
			"version", e.Descriptor.Manifest.Version,
			"protocol_version", e.Descriptor.Manifest.ProtocolVersion,
			"kind", "restart",
		)
		return true
	}
	return false
}

// nonNegDuration 返回非负 d, 否则返回 fallback.
func nonNegDuration(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}
