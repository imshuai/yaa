// Package plugin Loader: 发现、路径解析和校验.
// docs/plugin/loader.md §2: 路径与发现规则.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/exp/slog"
)

// Loader 发现并校验 Plugin Manifest, 不启动进程.
// docs/plugin/loader.md §1: 职责 = Discover (读 yaml + 校验 + 解析 entry).
type Loader struct {
	configDir       string // 主配置文件目录, 相对路径的基准
	paths           []string
	protocolVersion string // 固定 "1"
	logger          *slog.Logger
}

// NewLoader 校验 configDir 并规范化/去重 search paths. docs/plugin/loader.md §2.
// 空配置目录或无法规范化的搜索路径直接返回错误.
func NewLoader(configDir string, paths []string, logger *slog.Logger) (*Loader, error) {
	configDirAbs, err := filepath.Abs(configDir)
	if err != nil {
		return nil, fmt.Errorf("plugin loader: resolve config dir: %w", err)
	}
	if configDirAbs == "" {
		return nil, fmt.Errorf("plugin loader: empty config dir")
	}
	// 规范化并去重搜索路径
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		var absPath string
		if filepath.IsAbs(p) {
			absPath = filepath.Clean(p)
		} else {
			absPath = filepath.Join(configDirAbs, p)
			absPath = filepath.Clean(absPath)
		}
		if !seen[absPath] {
			seen[absPath] = true
			deduped = append(deduped, absPath)
		}
	}
	if logger == nil {
		return nil, fmt.Errorf("plugin loader: nil logger")
	}
	return &Loader{
		configDir:       configDirAbs,
		paths:           deduped,
		protocolVersion: "1",
		logger:          logger,
	}, nil
}

// Discover 扫描搜索目录的每个直接子目录, 读取并校验 plugin.yaml.
// docs/plugin/loader.md §2:
//   - 只返回完整且 ID 唯一的 Descriptor
//   - 无法解析 ID → 空 PluginID diagnostic
//   - 已解析 ID 后的 entry/版本错误 → diagnostic 携带 ID + 部分 Descriptor
//   - 重复 ID → 全部从成功结果移除, 各自产生同 ID diagnostic
func (l *Loader) Discover() (descriptors []PluginDescriptor, diagnostics []DiscoveryDiagnostic) {
	// 用 manifestID → PluginDescriptor 收集, 然后去重
	byID := make(map[string][]PluginDescriptor)
	byIDDiag := make(map[string][]DiscoveryDiagnostic)

	for _, searchDir := range l.paths {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			// 搜索目录不存在或不可读: 每个目录一条 diagnostic
			diagnostics = append(diagnostics, DiscoveryDiagnostic{
				Err: fmt.Errorf("read plugin path %s: %w", searchDir, err),
			})
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			manifestPath := filepath.Join(searchDir, entry.Name(), "plugin.yaml")
			manifest, err := LoadManifest(manifestPath)
			if err != nil {
				// 无法读取/解析 manifest → 空 PluginID (还未解析出 ID)
				diagnostics = append(diagnostics, DiscoveryDiagnostic{
					Err: err,
				})
				continue
			}
			// 先尝试从 manifest 提取 ID 用于 diagnostic
			pluginID := manifest.ID
			// 校验 manifest
			if vErr := ValidateManifest(manifest); vErr != nil {
				// manifest 校验失败: 如果 ID 已解析则携带, 否则空
				desc := &PluginDescriptor{ManifestPath: manifestPath, Manifest: manifest}
				diag := DiscoveryDiagnostic{
					PluginID:   pluginID,
					Descriptor: desc,
					Err:        vErr,
				}
				if pluginID != "" {
					byIDDiag[pluginID] = append(byIDDiag[pluginID], diag)
				} else {
					diagnostics = append(diagnostics, diag)
				}
				continue
			}
			// 解析 entry 路径
			entryPath, err := ResolveEntry(manifestPath, manifest.Entry)
			if err != nil {
				desc := &PluginDescriptor{ManifestPath: manifestPath, Manifest: manifest}
				diag := DiscoveryDiagnostic{
					PluginID:   pluginID,
					Descriptor: desc,
					Err:        err,
				}
				byIDDiag[pluginID] = append(byIDDiag[pluginID], diag)
				continue
			}
			// entry 可执行权限校验 (Unix)
			if err := validateEntryExecutable(entryPath); err != nil {
				desc := &PluginDescriptor{ManifestPath: manifestPath, Manifest: manifest}
				diag := DiscoveryDiagnostic{
					PluginID:   pluginID,
					Descriptor: desc,
					Err:        err,
				}
				byIDDiag[pluginID] = append(byIDDiag[pluginID], diag)
				continue
			}
			// 校验 dependency version 是有效 SemVer range 格式
			// (requires_runtime 校验在 Start 阶段, 需要外部注入 runtime 版本)
			if rErr := ValidateDependencies(manifest); rErr != nil {
				desc := &PluginDescriptor{ManifestPath: manifestPath, Manifest: manifest, EntryPath: entryPath}
				diag := DiscoveryDiagnostic{
					PluginID:   pluginID,
					Descriptor: desc,
					Err:        rErr,
				}
				byIDDiag[pluginID] = append(byIDDiag[pluginID], diag)
				continue
			}
			// 全部通过
			desc := PluginDescriptor{
				ManifestPath: manifestPath,
				EntryPath:    entryPath,
				Manifest:     manifest,
			}
			byID[manifest.ID] = append(byID[manifest.ID], desc)
			l.logger.Debug("plugin.discovered",
				"plugin_id", manifest.ID,
				"version", manifest.Version,
				"protocol_version", manifest.ProtocolVersion,
			)
		}
	}

	// 处理重复 ID: 出现多次的 ID 全部从结果移除, 各自产生 diagnostic
	var result []PluginDescriptor
	for id, descs := range byID {
		if len(descs) > 1 {
			// 重复 ID 全部拒绝
			for _, d := range descs {
				diag := DiscoveryDiagnostic{
					PluginID:   id,
					Descriptor: &d,
					Err:        fmt.Errorf("%w: %s appears in %d paths", ErrPluginDuplicateID, id, len(descs)),
				}
				byIDDiag[id] = append(byIDDiag[id], diag)
			}
			continue
		}
		result = append(result, descs[0])
	}

	// 合并 cardID diagnostics
	for id, diags := range byIDDiag {
		_ = id
		for _, diag := range diags {
			diagnostics = append(diagnostics, diag)
		}
	}

	return result, diagnostics
}

// validateEntryExecutable 校验 entry 是普通文件且有可执行权限 (Unix).
func validateEntryExecutable(entryPath string) error {
	info, err := os.Lstat(entryPath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPluginEntryNotFound, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file", ErrPluginEntryNotFound)
	}
	// 可执行位校验 (至少 user 或 group 或 other 有 x bit)
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: no execute permission", ErrPluginEntryNotFound)
	}
	return nil
}

// matchCapabilities 校验 Manifest provides 和 Ready capabilities 集合精确相等.
// docs/plugin/interface.md §2: type/name/description/schema 不得漂移.
func matchCapabilities(manifestCaps []CapabilityDescriptor, readyCaps []CapabilityDescriptor) error {
	if len(manifestCaps) != len(readyCaps) {
		return fmt.Errorf("%w: capability count mismatch (manifest %d vs ready %d)",
			ErrPluginCapabilityConflict, len(manifestCaps), len(readyCaps))
	}
	// 以 manifest 顺序找 ready 中匹配项
	readyMap := make(map[string]CapabilityDescriptor, len(readyCaps))
	for _, rc := range readyCaps {
		readyMap[rc.Type+":"+rc.Name] = rc
	}
	for _, mc := range manifestCaps {
		rc, ok := readyMap[mc.Type+":"+mc.Name]
		if !ok {
			return fmt.Errorf("%w: ready missing %s/%s", ErrPluginCapabilityConflict, mc.Type, mc.Name)
		}
		if rc.Description != mc.Description {
			return fmt.Errorf("%w: description mismatch for %s/%s", ErrPluginCapabilityConflict, mc.Type, mc.Name)
		}
		if !mapsEqual(rc.Schema, mc.Schema) {
			return fmt.Errorf("%w: schema mismatch for %s/%s", ErrPluginCapabilityConflict, mc.Type, mc.Name)
		}
	}
	return nil
}

// mapsEqual 递归比较两个 map[string]any.
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		if !valueEqual(av, bv) {
			return false
		}
	}
	return true
}

func valueEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return false
		}
		return mapsEqual(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valueEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
