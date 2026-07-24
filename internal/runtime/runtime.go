package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/imshuai/yaa/internal/api"
	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/storage"
	"golang.org/x/exp/slog"
)

// Runtime 是系统根容器，负责启动/停止子系统与提供健康检查。
// Phase 1 阶段已接 Config + Storage + API；后续阶段逐步接入 Provider/Tool 等组件。
type Runtime struct {
	cfg       *config.Config
	store     storage.Storage
	providers *provider.Manager
	sessions  *session.Manager
	api       *api.Server
	logger    *slog.Logger

	ready      atomic.Bool
	startedAt  time.Time
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
// Phase 1：Storage（sqlite|memory）→ API Server。
func (rt *Runtime) Start(ctx context.Context) error {
	rt.startedAt = time.Now()

	// Storage：未知类型/路径/migration 失败阻止 Ready。
	store, derr := storage.New(rt.cfg.Runtime.Storage)
	if derr != nil {
		return derr
	}
	rt.store = store
	if rt.cfg.Runtime.Storage.Type == "memory" {
		rt.components["storage"] = "degraded"
		rt.logger.Warn("root storage using memory backend", "durable", false)
	} else {
		rt.components["storage"] = "ready"
	}

	// Provider：未知类型/重复 ID/构造失败阻止 Ready。
	pm, perr := provider.NewManager(rt.cfg.Providers)
	if perr != nil {
		rt.rollback()
		return perr
	}
	rt.providers = pm
	rt.components["provider"] = "ready"

	// Session：Restore 失败阻止 Ready（文档：Remote API 不得在 Restore 完成前进入 Ready）。
	sm := session.NewManager(rt.cfg.Session, rt.store, rt.logger, session.ManagerOptions{
		AgentExists:   rt.agentExists,
		AgentOverride: rt.agentSessionOverride,
	})
	if rerr := sm.Restore(ctx, time.Now().UTC()); rerr != nil {
		rt.rollback()
		return rerr
	}
	if serr := sm.Start(ctx); serr != nil {
		rt.rollback()
		return serr
	}
	rt.sessions = sm
	rt.components["session_restore"] = "ready"

	rt.api = api.NewServer(rt.cfg.Runtime.API.HTTP.Addr, rt, rt.logger)
	rt.api.SetSessionProvider(sm, rt.agentAPIShim())
	if err := rt.api.Start(ctx); err != nil {
		rt.rollback()
		return err
	}
	rt.components["api"] = "ready"

	rt.ready.Store(true)
	rt.logger.Info("runtime ready", "addr", rt.cfg.Runtime.API.HTTP.Addr, "storage", rt.cfg.Runtime.Storage.Type)
	return nil
}

// Ready 返回当前是否已就绪。
func (rt *Runtime) Ready() bool { return rt.ready.Load() }

// Health 实现 api.HealthProvider，返回当前运行态快照。
func (rt *Runtime) Health() api.HealthData {
	status := "healthy"
	if !rt.ready.Load() {
		status = "not_ready"
	} else if rt.components["storage"] == "degraded" {
		// 关键组件 ready 但 storage 降级（memory 后端）→ degraded、ready=true。
		status = "degraded"
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

// Shutdown 按依赖逆序关闭子系统：先 Not Ready，再 API，最后 Storage。
func (rt *Runtime) Shutdown(ctx context.Context) error {
	rt.ready.Store(false) // 先原子标记 Not Ready
	var errs []error
	if rt.api != nil {
		if err := rt.api.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if rt.sessions != nil {
		if err := rt.sessions.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if rt.providers != nil {
		if err := rt.providers.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if rt.store != nil {
		if err := rt.store.Close(); err != nil {
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
	if rt.sessions != nil {
		_ = rt.sessions.Shutdown(context.Background())
	}
	if rt.providers != nil {
		_ = rt.providers.Close()
	}
	if rt.store != nil {
		_ = rt.store.Close()
	}
	rt.api = nil
	rt.sessions = nil
	rt.providers = nil
	rt.store = nil
	rt.components = map[string]string{}
}


// agentExists 判断某 Agent ID 是否在配置中注册。
func (rt *Runtime) agentExists(agentID string) bool {
	for _, a := range rt.cfg.Agents {
		if a.ID == agentID {
			return true
		}
	}
	return false
}

// agentSessionOverride 返回某 Agent 的 Session override 配置。
func (rt *Runtime) agentSessionOverride(agentID string) *config.SessionOverride {
	for _, a := range rt.cfg.Agents {
		if a.ID == agentID {
			return a.Session
		}
	}
	return nil
}

// agentAPIShim 实现 api.AgentExistsProvider，供 Session REST 端点校验 Agent。
type agentAPIProvider struct {
	exists   func(string) bool
	override func(string) *config.SessionOverride
}

func (a *agentAPIProvider) AgentExists(agentID string) bool                  { return a.exists(agentID) }
func (a *agentAPIProvider) AgentSessionOverride(agentID string) *config.SessionOverride {
	return a.override(agentID)
}

func (rt *Runtime) agentAPIShim() *agentAPIProvider {
	return &agentAPIProvider{
		exists:   rt.agentExists,
		override: rt.agentSessionOverride,
	}
}

// APIAddr 返回 API Server 的实际监听地址。
func (rt *Runtime) APIAddr() string {
	if rt.api != nil {
		return rt.api.Addr()
	}
	return rt.cfg.Runtime.API.HTTP.Addr
}

func cloneComponents(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
