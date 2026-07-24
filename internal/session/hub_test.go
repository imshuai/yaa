package session

import (
	"sync"
	"testing"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub(nil)
	s := h.Subscribe()
	h.Publish("hello")
	got, ok := <-s.Events()
	if !ok || got != "hello" {
		t.Fatalf("got=%v ok=%v", got, ok)
	}
	h.Unsubscribe(s)
	if _, ok := <-s.Events(); ok {
		t.Fatal("events should be closed after unsubscribe")
	}
}

func TestHubSlowSubscriberDropped(t *testing.T) {
	h := NewHub(nil)
	s := h.Subscribe()
	// 填满缓冲后多发一个，触发 drop。
	for i := 0; i < hubBufSize; i++ {
		h.Publish(i)
	}
	h.Publish("overflow") // 此时队列已满，该订阅者被注销。
	if !s.IsClosed() {
		t.Fatal("slow subscriber should be dropped")
	}
}

func TestHubCloseBroadcasts(t *testing.T) {
	h := NewHub(nil)
	s1, s2 := h.Subscribe(), h.Subscribe()
	var wg sync.WaitGroup
	wg.Add(2)
	got := make([]any, 0, 4)
	var mu sync.Mutex
	recv := func(s *Subscriber) {
		defer wg.Done()
		for ev := range s.Events() {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		}
	}
	go recv(s1)
	go recv(s2)
	h.Publish("a")
	h.Close("end")
	wg.Wait()
	if len(got) != 4 {
		t.Fatalf("expected a+end x2 subs=4, got %v", got)
	}
}

func TestHubPublishAfterCloseNoop(t *testing.T) {
	h := NewHub(nil)
	s := h.Subscribe()
	h.Close("end")
	h.Publish("late")
	// Close 投递 end；late 不应再入队。
	var saw int
	for range s.Events() {
		saw++
	}
	if saw != 1 {
		t.Fatalf("expected 1 (end), got %d", saw)
	}
}
