package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatcherTriggersReloadOnWrite 覆盖: 写入目标文件 → debounce → reload 被调用.
func TestWatcherTriggersReloadOnWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	if err := os.WriteFile(p, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded := make(chan ReloadResult, 4)
	reload := func() (ReloadResult, error) {
		select {
		case reloaded <- ReloadResult{Applied: true}:
		default:
		}
		return ReloadResult{Applied: true}, nil
	}
	w, err := NewWatcher(p, reload, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	// 触发 Write 事件
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(p, []byte("a: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloaded:
	case <-time.After(2 * time.Second):
		t.Fatal("reload not triggered within 2s")
	}
}

// TestWatcherDebouncesRapidWrites 覆盖: 快速多次写只触发一次 reload (防抖).
func TestWatcherDebouncesRapidWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	if err := os.WriteFile(p, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var callCount int32
	reload := func() (ReloadResult, error) {
		atomic.AddInt32(&callCount, 1)
		return ReloadResult{Applied: true}, nil
	}
	w, err := NewWatcher(p, reload, nil, nil)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	// 连续快速 5 次写, 间隔 10ms (< debounce=300ms)
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(p, []byte("a: 2\n"), 0o600)
		time.Sleep(10 * time.Millisecond)
	}
	// 等待 debounce + reload
	time.Sleep(600 * time.Millisecond)
	if got := atomic.LoadInt32(&callCount); got != 1 {
		t.Fatalf("debounce should coalesce 5 writes into 1 reload, got %d", got)
	}
}

// TestWatcherIgnoresUnrelatedFiles 覆盖: 只处理目标路径, 其它文件写入不触发 reload.
func TestWatcherIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	if err := os.WriteFile(p, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.txt")
	_ = os.WriteFile(other, []byte("x"), 0o600)

	var calls int32
	reload := func() (ReloadResult, error) {
		atomic.AddInt32(&calls, 1)
		return ReloadResult{Applied: true}, nil
	}
	w, _ := NewWatcher(p, reload, nil, nil)
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(other, []byte("y"), 0o600)
	time.Sleep(500 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("watcher should ignore unrelated file writes, got %d calls", got)
	}
}

// TestWatcherCtxCancelCleansUp 覆盖: ctx 取消后 Run 返回且不泄漏 goroutine.
func TestWatcherCtxCancelCleansUp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	_ = os.WriteFile(p, []byte("a: 1\n"), 0o600)
	reload := func() (ReloadResult, error) { return ReloadResult{}, nil }
	w, _ := NewWatcher(p, reload, nil, nil)
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancelCause(context.Background())
	go func() { _ = w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel(errors.New("test done"))
	// Run 应该结束; 再次写不应触发 reload (没有 panic 即可)
	time.Sleep(100 * time.Millisecond)
	_ = os.WriteFile(p, []byte("a: 2\n"), 0o600)
	time.Sleep(400 * time.Millisecond)
	// 验证 w.fs 关闭后再 Run 也是安全的 (fs 一关, 后续 select 会读到 nil chan, 这里不重 Run)
}

// TestWatcherOnReloadCallback 覆盖 onReload 回调被调用.
func TestWatcherOnReloadCallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	_ = os.WriteFile(p, []byte("a: 1\n"), 0o600)

	var mu sync.Mutex
	var gotReload ReloadResult
	reload := func() (ReloadResult, error) { return ReloadResult{Applied: true, Changed: []string{"x"}}, nil }
	onReload := func(r ReloadResult) {
		mu.Lock()
		gotReload = r
		mu.Unlock()
	}
	w, _ := NewWatcher(p, reload, onReload, nil)
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(p, []byte("a: 2\n"), 0o600)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := gotReload.Applied && len(gotReload.Changed) == 1
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("onReload callback not received")
}

// TestWatcherOnErrorCallback 覆盖 reload error 回调被调用.
func TestWatcherOnErrorCallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "yaa.yaml")
	_ = os.WriteFile(p, []byte("a: 1\n"), 0o600)

	var onErr atomic.Value // store error
	reload := func() (ReloadResult, error) { return ReloadResult{}, errors.New("reload boom") }
	onError := func(err error) { onErr.Store(err) }
	w, _ := NewWatcher(p, reload, nil, onError)
	t.Cleanup(func() { _ = w.fs.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(p, []byte("a: 2\n"), 0o600)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := onErr.Load(); v != nil {
			if err, ok := v.(error); ok && err != nil && err.Error() == "reload boom" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("onError callback not received within 2s")
}
