package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/tool"
)

func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "yaa-file-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func fileCfgWithAllowed(allowed, blocked []string) config.ToolConfig {
	opts := map[string]any{}
	if allowed != nil {
		arr := make([]any, 0, len(allowed))
		for _, p := range allowed {
			arr = append(arr, p)
		}
		opts["allowed_paths"] = arr
	}
	if blocked != nil {
		arr := make([]any, 0, len(blocked))
		for _, p := range blocked {
			arr = append(arr, p)
		}
		opts["blocked_paths"] = arr
	}
	return config.ToolConfig{Enabled: true, Options: opts}
}

func TestFileReadWriteDelete(t *testing.T) {
	d := tmpDir(t)
	fpath := filepath.Join(d, "x.txt")
	write, err := NewFileWrite(fileCfgWithAllowed([]string{d}, nil))
	if err != nil {
		t.Fatal(err)
	}
	r, err := write.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"path":    fpath,
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("Execute write: %v", err)
	}
	if r.IsError {
		t.Fatalf("write unexpected IsError: %s", r.Content)
	}

	read, err := NewFileRead(fileCfgWithAllowed([]string{d}, nil))
	if err != nil {
		t.Fatal(err)
	}
	rr, err := read.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"path": fpath,
	})
	if err != nil {
		t.Fatalf("Execute read: %v", err)
	}
	if rr.Content != "hello world" {
		t.Fatalf("content=%q", rr.Content)
	}

	del, err := NewFileDelete(fileCfgWithAllowed([]string{d}, nil))
	if err != nil {
		t.Fatal(err)
	}
	rd, err := del.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"path": fpath,
	})
	if err != nil {
		t.Fatalf("Execute delete: %v", err)
	}
	if !strings.Contains(rd.Content, "removed") {
		t.Fatalf("content=%q", rd.Content)
	}
}

func TestFileReadBlockedPath(t *testing.T) {
	d := tmpDir(t)
	inner := filepath.Join(d, "secret")
	if err := os.WriteFile(inner, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	read, _ := NewFileRead(fileCfgWithAllowed([]string{d}, []string{inner}))
	r, _ := read.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{"path": inner})
	if !r.IsError || r.Content != "path is blocked" {
		t.Fatalf("got=%+v", r)
	}
}

func TestFileReadNotAllowed(t *testing.T) {
	d := tmpDir(t)
	other := tmpDir(t)
	p := filepath.Join(other, "y")
	if err := os.WriteFile(p, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	read, _ := NewFileRead(fileCfgWithAllowed([]string{d}, nil))
	r, _ := read.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{"path": p})
	if !r.IsError || r.Content != "path is not in allowed paths" {
		t.Fatalf("got=%+v", r)
	}
}

func TestFileList(t *testing.T) {
	d := tmpDir(t)
	if err := os.WriteFile(filepath.Join(d, "b.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, _ := NewFileList(fileCfgWithAllowed([]string{d}, nil))
	r, _ := list.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{"path": d})
	var got []string
	if jerr := json.Unmarshal([]byte(r.Content), &got); jerr != nil {
		t.Fatalf("unmarshal: %v (%s)", jerr, r.Content)
	}
	if len(got) != 2 {
		t.Fatalf("names=%v", got)
	}
	if got[0] != "b.txt" || got[1] != "sub" {
		t.Fatalf("order=%v", got)
	}
}

func TestFileWriteCreatesDir(t *testing.T) {
	d := tmpDir(t)
	fpath := filepath.Join(d, "nested", "x.txt")
	write, _ := NewFileWrite(fileCfgWithAllowed([]string{d}, nil))
	r, _ := write.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"path":        fpath,
		"content":     "Z",
		"create_dirs": true,
	})
	if r.IsError {
		t.Fatalf("unexpected IsError: %s", r.Content)
	}
	content, _ := os.ReadFile(fpath)
	if string(content) != "Z" {
		t.Fatalf("file content=%q", content)
	}
}

func TestFileReadBase64(t *testing.T) {
	d := tmpDir(t)
	p := filepath.Join(d, "bin")
	if err := os.WriteFile(p, []byte{0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatal(err)
	}
	read, _ := NewFileRead(fileCfgWithAllowed([]string{d}, nil))
	r, _ := read.Execute(context.Background(), tool.ExecutionScope{AgentID: "a"}, map[string]any{
		"path":     p,
		"encoding": "base64",
	})
	if r.Content != "AQID" {
		t.Fatalf("base64=%q", r.Content)
	}
}
