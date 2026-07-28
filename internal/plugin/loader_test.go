package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/exp/slog"
)

// testLogger 返回一个 text handler 的 logger, 输出到 stderr.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr))
}

// writeValidPluginDir 在 dir 下创建一个完整可执行的 plugin 子目录.
func writeValidPluginDir(t *testing.T, searchDir, pluginID, entryName string) string {
	t.Helper()
	pluginDir := filepath.Join(searchDir, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `id: ` + pluginID + `
version: 0.1.0
protocol_version: "1"
entry: ` + entryName + `
provides:
  - type: tool
    name: ` + pluginID + `
    description: test tool
    schema:
      type: object
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// 创建可执行的 entry 文件
	entryPath := filepath.Join(pluginDir, entryName)
	if err := os.WriteFile(entryPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return pluginDir
}

func TestNewLoaderDedupPaths(t *testing.T) {
	dir := t.TempDir()
	// 同一路径传两次, 应去重为 1
	l, err := NewLoader(dir, []string{"./plugins", "./plugins", "./other"}, testLogger())
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if len(l.paths) != 2 {
		t.Fatalf("expected 2 deduped paths, got %d: %v", len(l.paths), l.paths)
	}
}

func TestNewLoaderNilLogger(t *testing.T) {
	_, err := NewLoader(t.TempDir(), []string{"./plugins"}, nil)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestNewLoaderRelativePaths(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLoader(dir, []string{"./plugins"}, testLogger())
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if len(l.paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(l.paths))
	}
	// 应该是绝对路径, 以 configDir 为基准
	if !filepath.IsAbs(l.paths[0]) {
		t.Fatalf("expected absolute path, got %s", l.paths[0])
	}
}

func TestNewLoaderAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "plugins")
	l, err := NewLoader(dir, []string{absPath}, testLogger())
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	if l.paths[0] != absPath {
		t.Fatalf("expected %s, got %s", absPath, l.paths[0])
	}
}

func TestDiscoverValidPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeValidPluginDir(t, pluginsDir, "weather", "yaa-plugin-weather")

	l, err := NewLoader(dir, []string{"./plugins"}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	descs, diags := l.Discover()
	if len(diags) > 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 descriptor, got %d: %v", len(descs), descs)
	}
	if descs[0].Manifest.ID != "weather" {
		t.Fatalf("expected id weather, got %s", descs[0].Manifest.ID)
	}
	if descs[0].EntryPath == "" {
		t.Fatal("empty entry path")
	}
}

func TestDiscoverMissingManifest(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "plugins", "mysub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := NewLoader(dir, []string{"./plugins"}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	_, diags := l.Discover()
	if len(diags) == 0 {
		t.Fatal("expected diagnostic for missing manifest")
	}
	// 无法读取 manifest → 空 PluginID
	if diags[0].PluginID != "" {
		t.Fatalf("expected empty PluginID, got %s", diags[0].PluginID)
	}
}

func TestDiscoverDuplicateID(t *testing.T) {
	dir := t.TempDir()
	pluginsDir1 := filepath.Join(dir, "plugins1")
	pluginsDir2 := filepath.Join(dir, "plugins2")
	if err := os.MkdirAll(pluginsDir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginsDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeValidPluginDir(t, pluginsDir1, "dup", "a")
	writeValidPluginDir(t, pluginsDir2, "dup", "b")

	l, err := NewLoader(dir, []string{"./plugins1", "./plugins2"}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	descs, diags := l.Discover()
	// 重复 ID → 0 descriptors
	if len(descs) != 0 {
		t.Fatalf("expected 0 descriptors for duplicate ID, got %d", len(descs))
	}
	// 应产生 diagnostic
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for duplicate ID")
	}
	// 所有 diagnostic 的 PluginID 应为 "dup"
	for _, d := range diags {
		if d.PluginID != "dup" {
			t.Fatalf("expected pluginID dup, got %s", d.PluginID)
		}
	}
}

func TestDiscoverEntryEscape(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	pluginDir := filepath.Join(pluginsDir, "bad")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// entry 指向逃逸路径
	manifest := `id: bad
version: 0.1.0
protocol_version: "1"
entry: ../malicious
provides:
  - type: tool
    name: bad
    description: bad
    schema:
      type: object
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := NewLoader(dir, []string{"./plugins"}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	descs, diags := l.Discover()
	if len(descs) != 0 {
		t.Fatalf("expected 0 descriptors for escape, got %d", len(descs))
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostic for escape")
	}
	if diags[0].PluginID != "bad" {
		t.Fatalf("expected pluginID bad, got %s", diags[0].PluginID)
	}
}

func TestDiscoverEntryNotExecutable(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	pluginDir := filepath.Join(pluginsDir, "noexec")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `id: noexec
version: 0.1.0
protocol_version: "1"
entry: noexec-bin
provides:
  - type: tool
    name: noexec
    description: x
    schema:
      type: object
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// 创建不可执行文件
	if err := os.WriteFile(filepath.Join(pluginDir, "noexec-bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := NewLoader(dir, []string{"./plugins"}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	descs, diags := l.Discover()
	if len(descs) != 0 {
		t.Fatalf("expected 0 descriptors, got %d", len(descs))
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostic for non-executable entry")
	}
}
