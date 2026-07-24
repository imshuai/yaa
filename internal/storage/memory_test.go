package storage

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func newFakeClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

func TestMemorySetGet(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Set("k", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "v" {
		t.Fatalf("got %q want v", got)
	}
}

func TestMemorySetCopiesBytes(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	src := []byte("hello")
	_ = s.Set("k", src)
	src[0] = 'X'
	got, _ := s.Get("k")
	if string(got) != "hello" {
		t.Fatalf("input not copied: %q", got)
	}
	// 写入后修改返回 slice 不影响内部数据。
	got[0] = 'Z'
	got2, _ := s.Get("k")
	if string(got2) != "hello" {
		t.Fatalf("output not copied: %q", got2)
	}
}

func TestMemoryGetNotFound(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	_, err := s.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestMemoryDeleteMissingIsIdempotent(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Delete("missing"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestMemoryHas(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	_ = s.Set("k", []byte("v"))
	ok, _ := s.Has("k")
	if !ok {
		t.Fatal("Has should be true")
	}
	ok, _ = s.Has("missing")
	if ok {
		t.Fatal("Has should be false")
	}
}

func TestMemoryKeysSortedAndPrefix(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	_ = s.Set("b", []byte("1"))
	_ = s.Set("a", []byte("1"))
	_ = s.Set("session:2", []byte("1"))
	_ = s.Set("session:1", []byte("1"))
	keys, err := s.Keys("session:")
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	want := []string{"session:1", "session:2"}
	if len(keys) != 2 || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("keys=%v want=%v", keys, want)
	}
	all, _ := s.Keys("")
	if len(all) != 4 {
		t.Fatalf("all keys len=%d want 4", len(all))
	}
}

func TestMemoryTTLHiddenNotExpired(t *testing.T) {
	clock := newFakeClock()
	s, _ := NewMemory(clock)
	t.Cleanup(func() { _ = s.Close() })
	_ = s.Set("k", []byte("v"), time.Minute)
	if _, err := s.Get("k"); err != nil {
		t.Fatalf("should not be expired yet: %v", err)
	}
	clock.t = clock.t.Add(2 * time.Minute)
	if _, err := s.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after expiry err=%v want ErrNotFound", err)
	}
	ok, _ := s.Has("k")
	if ok {
		t.Fatal("Has should be false after expiry")
	}
	keys, _ := s.Keys("")
	if len(keys) != 0 {
		t.Fatalf("Keys should hide expired: %v", keys)
	}
}

func TestMemoryTTLNegativeRejected(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Set("k", []byte("v"), -time.Second); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("err=%v want ErrInvalidTTL", err)
	}
}

func TestMemoryTTLMultipleArgsRejected(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Set("k", []byte("v"), time.Second, time.Second); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("err=%v want ErrInvalidTTL", err)
	}
}

func TestMemoryValueTooLarge(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	big := make([]byte, MaxValueBytes+1)
	if err := s.Set("k", big); !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("err=%v want ErrValueTooLarge", err)
	}
}

func TestMemoryInvalidKey(t *testing.T) {
	s, _ := NewMemory(nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Set("", []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err=%v want ErrInvalidKey", err)
	}
	long := string(bytes.Repeat([]byte("k"), 513))
	if err := s.Set(long, []byte("v")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err=%v want ErrInvalidKey", err)
	}
}

func TestMemoryCloseIdempotentAndAfterClosed(t *testing.T) {
	s, _ := NewMemory(nil)
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}
	if err := s.Set("k", []byte("v")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after close err=%v want ErrClosed", err)
	}
	if _, err := s.Get("k"); !errors.Is(err, ErrClosed) {
		t.Fatalf("Get after close err=%v want ErrClosed", err)
	}
}

func TestMemoryCleanupExpiredBatch(t *testing.T) {
	clock := newFakeClock()
	s, _ := NewMemory(clock)
	t.Cleanup(func() { _ = s.Close() })
	for i := 0; i < 5; i++ {
		_ = s.Set(time.Duration(i).String(), []byte("v"), time.Second)
	}
	clock.t = clock.t.Add(2 * time.Minute)
	n := s.cleanupExpired(1000)
	if n != 5 {
		t.Fatalf("cleanup removed %d want 5", n)
	}
}
