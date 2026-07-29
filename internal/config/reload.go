// reload.go: 热更新 ReloadManager. docs/config/hot-reload.md §3.
package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/exp/slog"
)

// ReloadResult 是 Reload 返回的脱敏结果. docs/config/hot-reload.md §3.
type ReloadResult struct {
	Applied         bool     `json:"applied"`
	Changed         []string `json:"changed"`
	RestartRequired bool     `json:"restart_required"`
	Paths           []string `json:"paths"` // 仅 restart-required 路径
}

// ReloadManager 是热更新唯一发布入口. docs/config/hot-reload.md §3.
type ReloadManager struct {
	path             string
	flags            map[string]any
	validateBindings func(*Config) error
	value            atomic.Value // stores *Config; snapshots are immutable
	reload           sync.Mutex   // 串行化 watcher/tool reload 请求
	active           bool         // 由 reload mutex 保护
	logger           *slog.Logger
}

// NewReloadManager 拒绝 nil, 立即保存已通过基础校验的不可变 initial snapshot.
// flags map 在构造时深拷贝. logger 为 nil 时用 slog.Default().
func NewReloadManager(initial *Config, path string, flags map[string]any, validateBindings func(*Config) error) (*ReloadManager, error) {
	if initial == nil {
		return nil, fmt.Errorf("%w: initial config must not be nil", ErrConfigNotActive)
	}
	flagsCopy := make(map[string]any, len(flags))
	for k, v := range flags {
		flagsCopy[k] = v
	}
	// ponytail: 浅拷贝 flags 已满足不变量 — flags 值是标量(string/int/bool), 无指针嵌套.
	m := &ReloadManager{
		path:             path,
		flags:            flagsCopy,
		validateBindings: validateBindings,
		logger:           slog.Default(),
	}
	m.value.Store(initial)
	return m, nil
}

// SetLogger 注入 logger; 仅在 Activate 之前调用一次.
func (m *ReloadManager) SetLogger(l *slog.Logger) {
	if l != nil {
		m.logger = l
	}
}

// Activate 在 reload mutex 下对当前初始 snapshot 执行 validateBindings,
// 成功后设置 active=true. 失败保持 inactive. 文档 §3.
func (m *ReloadManager) Activate() error {
	m.reload.Lock()
	defer m.reload.Unlock()
	if m.active {
		return nil
	}
	cur := m.value.Load().(*Config)
	if m.validateBindings != nil {
		if err := m.validateBindings(cur); err != nil {
			return fmt.Errorf("activate binding validation: %w", err)
		}
	}
	m.active = true
	return nil
}

// Current 返回当前 immutable snapshot; 调用方不得修改返回的字段/slice/map.
func (m *ReloadManager) Current() *Config {
	return m.value.Load().(*Config)
}

// Reload 是 watcher/Tool 唯一入口. docs/config/hot-reload.md §3.
// 1. 持 reload mutex, 要求 active=true, 读旧 snapshot
// 2. Load(path,flags) 候选 + validateBindings
// 3. diff 旧/新 → Changed (排字典序)
// 4. classify restart-required paths; 非空 → Applied=false,RestartRequired=true,Paths=...,error=nil
// 5. 无 restart path → 原子 Store, 返回 Applied=true
// Load/校验/发布错误返回非 nil error, 旧 snapshot 保持不变.
func (m *ReloadManager) Reload() (ReloadResult, error) {
	m.reload.Lock()
	defer m.reload.Unlock()
	if !m.active {
		return ReloadResult{}, ErrConfigNotActive
	}
	old := m.value.Load().(*Config)
	candidate, err := Load(m.path, m.flags)
	if err != nil {
		// 校验失败 / 解析错误: 保留旧 snapshot, 记录 error 日志. 行56/60.
		m.logger.Error("config reload load failed", err)
		return ReloadResult{}, fmt.Errorf("%w: %v", ErrConfigHotReloadFailed, err)
	}
	if m.validateBindings != nil {
		if err := m.validateBindings(candidate); err != nil {
			m.logger.Error("config reload binding validation failed", err)
			return ReloadResult{}, fmt.Errorf("%w: %v", ErrConfigHotReloadFailed, err)
		}
	}
	// diff
	changed, restartPaths, err := diffAndClassify(old, candidate)
	if err != nil {
		m.logger.Error("config reload diff failed", err)
		return ReloadResult{}, fmt.Errorf("%w: %v", ErrConfigHotReloadFailed, err)
	}
	sort.Strings(changed)
	// 没 restart path → 原子 Store
	if len(restartPaths) == 0 {
		m.value.Store(candidate)
		if len(changed) > 0 {
			m.logger.Info("config reloaded",
				slog.String("changed", strings.Join(changed, ",")))
		}
		return ReloadResult{Applied: true, Changed: changed}, nil
	}
	// restart-required: 不调用 Store, 返回 Applied=false + RestartRequired=true
	m.logger.Info("config reload requires restart",
		slog.String("paths", strings.Join(restartPaths, ",")))
	return ReloadResult{
		Applied:         false,
		Changed:         changed,
		RestartRequired: true,
		Paths:           restartPaths,
	}, nil
}

// hotReloadAllowlist 是可热更新路径前缀集合. docs/config/hot-reload.md §4 表格.
// 路径形式为点分隔的 leaf 字段路径 (数组下标已去除).
var hotReloadAllowlist = []string{
	"log.level",
	"agents.model",
	"agents.system_prompt",
	"agents.max_tokens",
	"agents.temperature",
	"tools.default_timeout",
	"tools.max_timeout",
	"tools.default_max_retry",
	"tools.max_result_tokens",
	"tools.builtin.timeout",
	"tools.builtin.options",
	"session.max_messages",
	"session.max_message_bytes",
	"session.ttl",
	"session.max_lifetime",
	"session.persist",
	"session.max_sessions_per_agent",
	"session.cleanup_interval",
	"agents.session", // 覆盖 agents[N].session.* 子字段
	"context",        // 覆盖 context.* 及 agents[N].context.* (后者路径去 [N] 后为 agents.context)
	"agents.context",
	"memory.max_items",
	"memory.default_ttl",
	"memory.eviction_policy",
	"memory.expire_interval",
	"memory.expire_batch_size",
	"agents.memory.max_items",
	"agents.memory.default_ttl",
	"agents.memory.eviction_policy",
}

// pathIsHotReloadable 判断路径是否在 allowlist (前缀匹配).
func pathIsHotReloadable(path string) bool {
	for _, p := range hotReloadAllowlist {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// diffAndClassify 比较两个 Config, 返回所有 changed leaf 路径
// 与其中需要 restart 的子集.
func diffAndClassify(old, cur *Config) (changed, restartPaths []string, err error) {
	oldMap, err := ConfigToMap(old)
	if err != nil {
		return nil, nil, fmt.Errorf("old to map: %w", err)
	}
	curMap, err := ConfigToMap(cur)
	if err != nil {
		return nil, nil, fmt.Errorf("cur to map: %w", err)
	}
	allChanged := diffMaps("", oldMap, curMap)
	// 去重 + 排序, 便于稳定输出和 allowlist 判定
	seen := make(map[string]struct{}, len(allChanged))
	for _, p := range allChanged {
		// 规范化路径: 把数组下标 [N] 移除, 转为点分隔
		norm := normalizeArrayIndexPath(p)
		seen[norm] = struct{}{}
	}
	for path := range seen {
		changed = append(changed, path)
		if !pathIsHotReloadable(path) {
			restartPaths = append(restartPaths, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(restartPaths)
	return changed, restartPaths, nil
}

// normalizeArrayIndexPath 把 "agents[0].model" 转成 "agents.model".
// 对数组层级变化 (长度不同) 会产生 "agents[N]" 形式, 去掉 [N] 得 "agents".
func normalizeArrayIndexPath(p string) string {
	// 反复替换 "[数字]."
	out := p
	for strings.Contains(out, "[") {
		i := strings.IndexByte(out, '[')
		j := strings.IndexByte(out, ']')
		if i < 0 || j < 0 || j < i {
			break
		}
		// 去掉 [..] 段, 后面若有 "." 则保留点
		var b strings.Builder
		b.WriteString(out[:i])
		// 去掉紧接其后的 "."
		rest := out[j+1:]
		if strings.HasPrefix(rest, ".") {
			rest = rest[1:]
		}
		b.WriteString(".")
		b.WriteString(rest)
		out = b.String()
	}
	return out
}

// diffMaps 递归比较两个 map[string]any, 收集所有叶子 (标量/null) 的变化路径.
// 中间结构相同但子叶子不同, 也展开为子路径. 数组按 index 对齐比较.
// ponytail: 数组长度不同 → 在路径末尾产生数组层级路径; 元素按 index 递归比较.
func diffMaps(prefix string, old, cur map[string]any) []string {
	var paths []string
	keys := make(map[string]struct{})
	for k := range old {
		keys[k] = struct{}{}
	}
	for k := range cur {
		keys[k] = struct{}{}
	}
	for k := range keys {
		var key string
		if prefix == "" {
			key = k
		} else {
			key = prefix + "." + k
		}
		ov, ohas := old[k]
		cv, chas := cur[k]
		if !ohas {
			// cur 新增字段
			paths = append(paths, leafPaths(key, cv)...)
			continue
		}
		if !chas {
			// cur 删除字段
			paths = append(paths, key)
			continue
		}
		paths = append(paths, diffVal(key, ov, cv)...)
	}
	return paths
}

// diffVal 比较两个任意值, 返回变化叶子路径列表.
func diffVal(key string, ov, cv any) []string {
	// 两边都是 map → 递归
	om, oisMap := ov.(map[string]any)
	cm, cisMap := cv.(map[string]any)
	if oisMap && cisMap {
		return diffMaps(key, om, cm)
	}
	// 两边都是 array → 按 index 对齐
	oa, oisArr := ov.([]any)
	ca, cisArr := cv.([]any)
	if oisArr && cisArr {
		var paths []string
		maxLen := len(oa)
		if len(ca) > maxLen {
			maxLen = len(ca)
		}
		if len(oa) != len(ca) {
			// 数组长度变化: 整个数组层级视为 changed (非 allowlist → restart)
			paths = append(paths, key)
		}
		for i := 0; i < maxLen; i++ {
			ik := fmt.Sprintf("%s[%d]", key, i)
			if i >= len(oa) {
				// 新增元素
				paths = append(paths, leafPaths(ik, ca[i])...)
				continue
			}
			if i >= len(ca) {
				// 删除元素
				paths = append(paths, ik)
				continue
			}
			paths = append(paths, diffVal(ik, oa[i], ca[i])...)
		}
		return paths
	}
	// 类型变化 (如 nil ↔ map): 都视为 leaf 变化
	if !valEqual(ov, cv) {
		return []string{key}
	}
	return nil
}

// valEqual 标量/nil 比较: yaml unmarshal 出来的标量类型有限 (string/bool/int64/float64/nil).
func valEqual(a, b any) bool {
	// 时间类型 (time.Time / time.Duration) 经 yaml marshal 会变 string, 已 round trip 一致.
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// leafPaths 返回 v 展开到叶子后的所有路径 (用于新增字段).
func leafPaths(key string, v any) []string {
	switch t := v.(type) {
	case map[string]any:
		var ps []string
		for k := range t {
			var sub string
			if key == "" {
				sub = k
			} else {
				sub = key + "." + k
			}
			ps = append(ps, leafPaths(sub, t[k])...)
		}
		return ps
	case []any:
		var ps []string
		for i, e := range t {
			ps = append(ps, leafPaths(fmt.Sprintf("%s[%d]", key, i), e)...)
		}
		return ps
	default:
		return []string{key}
	}
}
