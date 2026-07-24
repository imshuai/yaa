package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/exp/slog"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/provider"
)

// Manager 是 Tool 系统唯一注册、发现、鉴权和执行入口。
// v1 实现：静态配置注入（非 ReloadManager），canonical name 直接作为 Provider alias
// （足够避免 alias 反查在绝大部分 v1 场景的冲突）。
type Manager struct {
	cfg       *config.Config
	providers *provider.Manager
	logger    *slog.Logger

	mu      sync.RWMutex
	tools   map[string]Tool
	configs map[string]config.ToolConfig
	source  map[string]string // canonical -> builtin | plugin | mcp

	// agentBindings 缓存每个 Agent 的 allowlist 与 Tool override。
	agents map[string]agentBinding

	// 并发Gate
	global sema
}

type agentBinding struct {
	AllowAll bool
	Allowed  map[string]struct{}
}

type sema chan struct{}

func newSema(n int) sema {
	if n <= 0 {
		n = 1
	}
	return make(chan struct{}, n)
}

// Dependencies 暴露必要依赖集合。
type Dependencies struct {
	Config    *config.Config
	Providers *provider.Manager
	Logger    *slog.Logger
}

// NewManager 从 config 静态读取 builtin Tool 配置和 Agent binding，构造空 Tool 表；
// builtin Tool 由调用方通过 Register 追加注入。
func NewManager(deps Dependencies) (*Manager, error) {
	if deps.Config == nil {
		return nil, errors.New("tool: nil config")
	}
	if deps.Providers == nil {
		return nil, errors.New("tool: nil provider manager")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	tc := deps.Config.Tools
	m := &Manager{
		cfg:       deps.Config,
		providers: deps.Providers,
		logger:    deps.Logger,
		tools:     map[string]Tool{},
		configs:   map[string]config.ToolConfig{},
		source:    map[string]string{},
		agents:    map[string]agentBinding{},
		global:    newSema(tc.MaxConcurrent),
	}
	// 复制 builtin 配置（深拷贝 options）。
	for name, bc := range tc.Builtin {
		var opts map[string]any
		opts = cloneAnyMap(bc.Options)
		m.configs[name] = config.ToolConfig{Enabled: bc.Enabled, Timeout: bc.Timeout, Options: opts}
		m.source[name] = "builtin"
	}
	// file 容器分裂：docs/config/reference.md §6.3 约定 tools.builtin.file 是
	// file_read/file_write/file_list/file_delete 四个注册 Tool 共享的配置组。
	// 此处把容器配置复制到 4 个 canonical 名，使 Enabled/Timeout/Options 与文档语义一致；
	// 仅当未显式配置 file_read 等键时才复制（显式覆盖优先）。
	if fc, hasFile := tc.Builtin["file"]; hasFile {
		for _, n := range []string{"file_read", "file_write", "file_list", "file_delete"} {
			if _, explicit := m.configs[n]; explicit {
				continue
			}
			m.configs[n] = config.ToolConfig{
				Enabled: fc.Enabled,
				Timeout: fc.Timeout,
				Options: cloneAnyMap(fc.Options),
			}
			m.source[n] = "builtin"
		}
	}
	// 计算每个 Agent 的 allowlist。
	for _, ag := range deps.Config.Agents {
		if len(ag.Tools) == 0 {
			m.agents[ag.ID] = agentBinding{AllowAll: true}
		} else {
			set := make(map[string]struct{}, len(ag.Tools))
			for _, name := range ag.Tools {
				set[name] = struct{}{}
			}
			m.agents[ag.ID] = agentBinding{Allowed: set}
		}
	}
	return m, nil
}

// Register 注册一个 Tool（canonical name 来自 Tool.Name()）。
func (m *Manager) Register(t Tool) error {
	name := t.Name()
	if !isValidToolName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidToolName, name)
	}
	if t.Description() == "" {
		return fmt.Errorf("%w: %q missing description", ErrInvalidToolDef, name)
	}
	params := t.Parameters()
	if !json.Valid(params) {
		return fmt.Errorf("%w: %q invalid parameters schema", ErrInvalidToolDef, name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tools[name]; ok {
		return fmt.Errorf("%w: %q already registered", ErrInvalidToolDef, name)
	}
	// 深拷贝 schema。
	dst := make([]byte, len(params))
	copy(dst, params)
	m.tools[name] = t
	if _, cfgOk := m.configs[name]; !cfgOk {
		// 未被 config 预定义（plugin/mcp 通常如此），按默认启用。
		m.configs[name] = config.ToolConfig{Enabled: true, Options: map[string]any{}}
	}
	if _, sourceOk := m.source[name]; !sourceOk {
		m.source[name] = "builtin"
	}
	return nil
}

func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tools[name]; !ok {
		return fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}
	delete(m.tools, name)
	delete(m.configs, name)
	delete(m.source, name)
	return nil
}

func (m *Manager) Get(name string) (Tool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}
	return t, nil
}

func (m *Manager) List() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listLocked()
}

func (m *Manager) listLocked() []ToolInfo {
	names := make([]string, 0, len(m.tools))
	for n := range m.tools {
		names = append(names, n)
	}
	sortStrings(names)
	out := make([]ToolInfo, 0, len(names))
	for _, n := range names {
		t := m.tools[n]
		cfg := m.configs[n]
		out = append(out, ToolInfo{
			Name:        n,
			Description: t.Description(),
			Parameters:  cloneBytes(t.Parameters()),
			Enabled:     cfg.Enabled,
			Source:      m.source[n],
		})
	}
	return out
}

// ListForAgent 返回 enabled 且授权的 Tool。
func (m *Manager) ListForAgent(agentID string) []ToolInfo {
	all := m.List()
	b, ok := m.agentBinding(agentID)
	if !ok {
		return nil
	}
	out := all[:0]
	for _, ti := range all {
		if !ti.Enabled {
			continue
		}
		if !b.AllowAll {
			if _, allowed := b.Allowed[ti.Name]; !allowed {
				continue
			}
		}
		out = append(out, ti)
	}
	return out
}

func (m *Manager) agentBinding(agentID string) (agentBinding, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.agents[agentID]
	return b, ok
}

func (m *Manager) CheckPermission(agentID, toolName string) bool {
	b, ok := m.agentBinding(agentID)
	if !ok {
		return false
	}
	if b.AllowAll {
		return true
	}
	_, allowed := b.Allowed[toolName]
	return allowed
}

// Execute 单次执行 Tool，按 Manager §6 流程。
func (m *Manager) Execute(ctx context.Context, scope ExecutionScope, toolName string, params map[string]any) (ToolResult, error) {
	if scope.AgentID == "" {
		return ToolResult{}, fmt.Errorf("%w: empty agent id", ErrPermissionDenied)
	}
	m.mu.RLock()
	t, ok := m.tools[toolName]
	cfg := m.configs[toolName]
	m.mu.RUnlock()
	if !ok {
		return ToolResult{}, fmt.Errorf("%w: %q", ErrToolNotFound, toolName)
	}
	if !cfg.Enabled {
		return ToolResult{}, fmt.Errorf("%w: %q", ErrToolDisabled, toolName)
	}
	if !m.CheckPermission(scope.AgentID, toolName) {
		return ToolResult{}, fmt.Errorf("%w: %q", ErrPermissionDenied, toolName)
	}
	// JSON Schema 校验。Ponytail v1 跳过 JSON Schema validator 等重型手段；
	// 校验 params 必须为 json.RawMessage decode 后回声：保留原始 error。
	if err := validateParams(t.Parameters(), params); err != nil {
		return ToolResult{}, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}

	// 计算 effective timeout：Tool 上层 inherited default。
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = m.cfg.Tools.DefaultTimeout
	}
	if timeout > m.cfg.Tools.MaxTimeout {
		timeout = m.cfg.Tools.MaxTimeout
	}

	// 获取 global gate。
	select {
	case m.global <- struct{}{}:
		defer func() { <-m.global }()
	case <-ctx.Done():
		return ToolResult{}, context.Cause(ctx)
	}

	// callCtx with timeout。
	callCtx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(timeout, func() { cancel(ErrToolTimeout) })
	defer func() {
		timer.Stop()
		cancel(nil)
	}()

	result, err := t.Execute(callCtx, scope, params)
	if ctx.Err() != nil {
		return ToolResult{}, context.Cause(ctx)
	}
	if callCtx.Err() != nil {
		return ToolResult{}, context.Cause(callCtx)
	}
	return result, err
}

// ExecuteBatch 顺序保持的并发 batch。每个 call 的 name 必须是 canonical（不反查 alias）。
func (m *Manager) ExecuteBatch(ctx context.Context, scope ExecutionScope, calls []provider.ToolCall) ([]ToolResult, error) {
	results := make([]ToolResult, len(calls))
	if len(calls) == 0 {
		return results, nil
	}
	var wg sync.WaitGroup
	idx := make([]int, len(calls))
	for i := range idx {
		idx[i] = i
	}
	var idxMu sync.Mutex
	workers := m.cfg.Tools.MaxConcurrent
	if workers < 1 {
		workers = 1
	}
	if workers > len(calls) {
		workers = len(calls)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idxMu.Lock()
				if len(idx) == 0 {
					idxMu.Unlock()
					return
				}
				i := idx[0]
				idx = idx[1:]
				idxMu.Unlock()
				call := calls[i]
				params, perr := decodeArgs(call.Function.Arguments)
				if perr != nil {
					results[i] = ErrorResult(fmt.Errorf("%w: %v", ErrInvalidParams, perr))
					continue
				}
				result, err := m.Execute(ctx, scope, call.Function.Name, params)
				if ctx.Err() != nil {
					// Batch 会在全部完成后返回 caller cause；此处不做 short-circuit。
					result = ErrorResult(err)
				} else if err != nil {
					result = ErrorResult(err)
				}
				results[i] = result
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return results, context.Cause(ctx)
	}
	return results, nil
}

// ErrorResult 把 Manager 硬错误转成脱敏 ToolResult IsError，供 Agent Tool unit 使用。
func ErrorResult(err error) ToolResult {
	message := "tool execution failed"
	switch {
	case errors.Is(err, ErrToolNotFound):
		message = "tool not found"
	case errors.Is(err, ErrToolDisabled):
		message = "tool disabled"
	case errors.Is(err, ErrPermissionDenied):
		message = "tool permission denied"
	case errors.Is(err, ErrInvalidParams):
		message = "invalid tool arguments"
	case errors.Is(err, ErrToolTimeout):
		message = "tool execution timed out"
	}
	return ToolResult{Content: message, IsError: true}
}

// validateParams 在 v1 不接 JSON Schema validator，而是验证 params 是可序列化 map（保留原 nil 兼容）。
// Ponytail：若 Parameters 为空 schema 必须接受空对象；后续接入完整 validator。
func validateParams(schema json.RawMessage, params map[string]any) error {
	_ = schema
	if params == nil {
		return errors.New("params must be object")
	}
	// 抽样编码确保 map 可序列化（避免 channel/func）。
	if _, err := json.Marshal(params); err != nil {
		return fmt.Errorf("params not serializable: %w", err)
	}
	return nil
}

func decodeArgs(args string) (map[string]any, error) {
	if args == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(args), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	d := make([]byte, len(b))
	copy(d, b)
	return d
}

func sortStrings(a []string) {
	// stdlib sort import not used; 用插入排序简化观察者？不：直接调用 pkg 内摘录泡排序。
	// ponytail：21 个 builtin 不需要快排，插入排序足够，避免引入 sort 包 dependency。
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func isValidToolName(name string) bool {
	if len(name) < 1 || len(name) > 256 {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
