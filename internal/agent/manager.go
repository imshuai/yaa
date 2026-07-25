// Package agent 是 Provider、Session、Context、Memory、Tool、Skill 和 Planner 的唯一编排 owner。
// Phase 2 最小实现：Status 生命周期 + direct turn（无 Tool/Memory/Skill）。
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/imshuai/yaa/internal/config"
	ctxwindow "github.com/imshuai/yaa/internal/context"
	"github.com/imshuai/yaa/internal/provider"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/skill"
	"github.com/imshuai/yaa/internal/tool"
	"golang.org/x/exp/slog"
)

// Dependencies 是 Runtime 持有并借给 Agent Manager 的对象。
// ponytail: v1 不含 Memory；Tool/Skill 已注入；使用 nil 兼容。
type Dependencies struct {
	Config    *config.Config
	Sessions  *session.Manager
	Context   *ctxwindow.Manager
	Providers *provider.Manager
	Tools     *tool.Manager
	Skills    *skill.Manager
	Logger    *slog.Logger
}

// agentBinding 是冻结的 Agent 特有配置。
type agentBinding struct {
	id        string
	name      string
	provider  string
	model     string
	sysPrompt string
	maxTokens int
	status    Status
}

// Manager 是 Agent Manager。
type Manager struct {
	deps   Dependencies
	mu     sync.Mutex
	agents map[string]*agentBinding
	closed bool
}

// NewManager 构造 Agent Manager。
// deps 中 Config/Sessions/Context/Providers/Logger 不可为 nil。
// 按 Agent ID 排序构造绑定。ponytail: v1 不含 Agent 匹配 关 键 字.
func NewManager(deps Dependencies) (*Manager, error) {
	if deps.Config == nil {
		return nil, errors.New("agent: config is nil")
	}
	// Sessions 可延迟注入（Runtime 先构造 Agent，再创建和 Restore Session，
	// 最后通过 SetSessions 补全指针），构造时允许 nil。
	if deps.Context == nil {
		return nil, errors.New("agent: context is nil")
	}
	if deps.Providers == nil {
		return nil, errors.New("agent: providers is nil")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	m := &Manager{
		deps:   deps,
		agents: map[string]*agentBinding{},
	}
	for _, a := range deps.Config.Agents {
		if a.ID == "" {
			return nil, fmt.Errorf("agent: agent with empty id")
		}
		if _, ok := m.agents[a.ID]; ok {
			return nil, fmt.Errorf("agent: duplicate agent id %q", a.ID)
		}
		// 验证 Provider/Model 存在
		p, perr := deps.Providers.Get(a.Provider)
		if perr != nil {
			return nil, fmt.Errorf("agent %q: provider %q not found: %w", a.ID, a.Provider, perr)
		}
		// 验证 Model 存在
		found := false
		for _, mi := range p.Models() {
			if mi.ID == a.Model {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("agent %q: model %q not found in provider %q", a.ID, a.Model, a.Provider)
		}
		maxTokens := a.MaxTokens
		if maxTokens <= 0 {
			return nil, fmt.Errorf("agent %q: max_tokens must be > 0", a.ID)
		}
		m.agents[a.ID] = &agentBinding{
			id:        a.ID,
			name:      a.Name,
			provider:  a.Provider,
			model:     a.Model,
			sysPrompt: a.SystemPrompt,
			maxTokens: maxTokens,
			status:    StatusRunning,
		}
	}
	return m, nil
}

// Get 返回 Agent Info 副本。
func (m *Manager) Get(id string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return Info{}, fmt.Errorf("%w: %s", ErrAgentNotFound, id)
	}
	return Info{
		ID: a.id, Name: a.name, Provider: a.provider, Model: a.model, Status: a.status,
	}, nil
}

// Inspect 返回 Agent Detail 副本。
func (m *Manager) Inspect(id string) (Detail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return Detail{}, fmt.Errorf("%w: %s", ErrAgentNotFound, id)
	}
	// ponytail: v1 无 Tools/Skills/Memory/Planner 冻结
	return Detail{
		Info: Info{
			ID: a.id, Name: a.name, Provider: a.provider, Model: a.model, Status: a.status,
		},
		Tools:          []string{},
		Skills:         []string{},
		MemoryEnabled:  false,
		PlannerEnabled: false,
	}, nil
}

// List 返回所有 Agent Info 副本，按 ID 升序。
func (m *Manager) List(status *Status) []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Info
	for _, a := range m.agents {
		if status != nil && a.status != *status {
			continue
		}
		out = append(out, Info{
			ID: a.id, Name: a.name, Provider: a.provider, Model: a.model, Status: a.status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Start 将 stopped|paused -> running；已 running 幂等。
func (m *Manager) Start(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, id)
	}
	if a.status == StatusRunning {
		return nil
	}
	if a.status == StatusStopped || a.status == StatusPaused {
		a.status = StatusRunning
		return nil
	}
	return fmt.Errorf("%w: %s in state %s", ErrAgentInvalidState, id, a.status)
}

// Pause 将 running -> paused；已 paused 幂等；stopped 返回 ErrAgentInvalidState。
func (m *Manager) Pause(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, id)
	}
	if a.status == StatusPaused {
		return nil
	}
	if a.status == StatusStopped {
		return fmt.Errorf("%w: cannot pause stopped agent %s", ErrAgentInvalidState, id)
	}
	a.status = StatusPaused
	return nil
}

// Stop 将 paused|running -> stopped。
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, id)
	}
	a.status = StatusStopped
	return nil
}

// Quiesce 关闭 admission（不再接受新 turn），不取消已登记 turn。
// DBus lv1: 用于 Runtime Shutdown 前。
func (m *Manager) Quiesce() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
}

// Shutdown 关闭 Agent Manager。幂等。
// v1 无独立 goroutine，仅标记 closed。
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

// CancelTurn 取消某个在途 turn。
// ponytail: v1 直接委托给 Session Manager 的 CancelTurn。
func (m *Manager) CancelTurn(ctx context.Context, agentID, sessionID, turnID string) error {
	// 验证 agent 存在
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrAgentManagerClosed
	}
	_, ok := m.agents[agentID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrAgentNotFound, agentID)
	}
	return m.deps.Sessions.CancelTurn(sessionID, turnID, ErrAgentStopped)
}

// SetSessions 注入 Session Manager（在 Runtime 启动 Session 完成后调用）。
// 用于解决 Agent 依赖 Session 但 Session 也依赖 Agent 信息的循环。
func (m *Manager) SetSessions(sm *session.Manager) {
	m.mu.Lock()
	m.deps.Sessions = sm
	m.mu.Unlock()
}

// SetTools 延迟注入 Tool Manager（Runtime 先构造 Agent，再创建 Tool Manager 并注册 builtin）。
func (m *Manager) SetTools(tm *tool.Manager) {
	m.mu.Lock()
	m.deps.Tools = tm
	m.mu.Unlock()
}

// SetSkills 延迟注入 Skill Manager（Runtime 在 Tool Manager 之后才创建 Skill Manager 并完成 Agent binding）。
func (m *Manager) SetSkills(sm *skill.Manager) {
	m.mu.Lock()
	m.deps.Skills = sm
	m.mu.Unlock()
}
