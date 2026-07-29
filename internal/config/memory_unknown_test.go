package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMemoryUnknownFieldRejectedStrict 覆盖 docs/memory/config-ref.md §180:
// "未知字段在 strict decoder 阶段拒绝" — 检查 memory 段下的未知字段在 Load 时被拒.
func TestMemoryUnknownFieldRejectedStrict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	// memory 含未知字段 "nonsense_field"
	content := `config_version: "1.0"
runtime:
  storage: {}
  api: {http: {addr: "127.0.0.1:8080"}, ws: {}, sse: {}}
  auth: {enabled: false}
memory:
  enabled: true
  max_items: 500
  nonsense_field: 123
`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p, nil)
	if err == nil {
		t.Fatal("expected Load to reject memory.nonsense_field (strict decoder)")
	}
	if !strings.Contains(err.Error(), "nonsense_field") {
		t.Fatalf("error should mention unknown field, got: %v", err)
	}
}

// TestMemoryOverrideUnknownFieldRejectedStrict 覆盖 agents[].memory 未知字段也被拒.
func TestMemoryOverrideUnknownFieldRejectedStrict(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	content := `config_version: "1.0"
runtime:
  storage: {}
  api: {http: {addr: "127.0.0.1:8080"}, ws: {}, sse: {}}
  auth: {enabled: false}
providers:
  - id: p1
    type: openai
    api_key: ${FAKE_API_KEY}
agents:
  - id: a1
    name: Agent1
    provider: p1
    model: m1
    memory:
      max_items: 100
      made_up: true
`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_API_KEY", "k")
	_, err := Load(p, nil)
	if err == nil {
		t.Fatal("expected Load to reject agents[].memory.made_up (strict decoder)")
	}
	// deleteExpired 错误信息可能 attnagent, 但 "made_up" 字应该出现在 errors join
	if !strings.Contains(err.Error(), "made_up") {
		t.Fatalf("error should mention unknown memory field, got: %v", err)
	}
	// ensure it's actually the strict decoder error, not just any error
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error class should be unknown field, got: %v", err)
	}
}
