package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/imshuai/yaa/internal/api"
	"github.com/imshuai/yaa/internal/config"
	"golang.org/x/exp/slog"
)

// Runtime 是系统根容器，负责启动/停止子系统与提供健康检查。
// Phase 1 阶段只接 Config + API；后续阶段逐步接入 Storage/Provider/Tool 等组件。
type Runtime struct {
	cfg    *config.Config
	api    *api.Server
	logger *slog.Logger

	ready     atomic.Bool
	startedAt time.Time
	// Components 记录已启动子系统的状态名（后续按依赖逆序填充）。
	components map[string]string
}

// New 构造 Runtime，但不启动任何组件。
func New(cfg *config.Config, logger *slog.Logger) (*Runtime, error) {
	if cfg == nil {
		return nil, errors.New("runtime: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runtime{
		cfg:        cfg,
		logger:     logger,
		components: map[string]string{},
	}, nil
}

// Start 按初始化顺序启动子系统并标记 Ready。
// Phase 1：仅启动 API Server。
func (rt *Runtime) Start(ctx context.Context) error {
	rt.startedAt = time.Now()

	rt.api = api.NewServer(rt.cfg.Runtime.API.HTTP.Addr, rt, rt.logger)
	if err := rt.api.Start(ctx); err != nil {
		rt.rollback()
		return err
	}
	rt.components["api"] = "ready"
	rt.ready.Store(true)
	rt.logger.Info("runtime ready", "addr", rt.cfg.Runtime.API.HTTP.Addr)
	return nil
}

// Ready 返回当前是否已就绪。
func (rt *Runtime) Ready() bool { return rt.ready.Load() }

// Health 实现 api.HealthProvider，返回当前运行态快照。
func (rt *Runtime) Health() api.HealthData {
	status := "healthy"
	if !rt.ready.Load() {
		status = "not_ready"
	}
	return api.HealthData{
		Status:     status,
		Ready:      rt.ready.Load(),
		Agents:     api.AgentCounts{},
		Components: cloneComponents(rt.components),
	}
}

// UptimeSeconds 返回启动以来的秒数。
func (rt *Runtime) UptimeSeconds() int64 {
	if rt.startedAt.IsZero() {
		return 0
	}
	return int64(time.Since(rt.startedAt).Seconds())
}

// Shutdown 按依赖逆序关闭子系统。
func (rt *Runtime) Shutdown(ctx context.Context) error {
	rt.ready.Store(false) // 先原子标记 Not Ready
	var errs []error
	if rt.api != nil {
		if err := rt.api.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	rt.components = map[string]string{}
	return errors.Join(errs...)
}

// rollback 在启动失败时按已成功启动组件的逆序回滚。
func (rt *Runtime) rollback() {
	rt.ready.Store(false)
	if rt.api != nil {
		_ = rt.api.Shutdown(context.Background())
	}
	rt.api = nil
	rt.components = map[string]string{}
}

func cloneComponents(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// APIAddr 返回 API Server 的实际监听地址；未启动时返回配置 addr。
func (rt *Runtime) APIAddr() string {
	if rt.api != nil {
		return rt.api.Addr()
	}
	return rt.cfg.Runtime.API.HTTP.Addr
}
