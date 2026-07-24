package session

import (
	"sync"

	"golang.org/x/exp/slog"
)

// Hub 是 Session 级事件总线。订阅者各自拥有固定容量 256 的独立输出队列；
// 发布者只做非阻塞 enqueue，队列已满时原子注销该订阅者并关闭其输出。
// Ponytail: v1 不保存 frame replay buffer，也不实现 sequence cursor。
const hubBufSize = 256

// Hub 接收任意类型事件（agent.TurnEvent 或 session.SessionEvent），由订阅者自行断言。
// 之所以用 any 而非强类型接口：agent 与 session 互相 import 会形成循环，hub 作为 conduit 与两端解耦。
type Hub struct {
	mu     sync.Mutex
	subs   map[*Subscriber]struct{}
	logger *slog.Logger
	closed bool
}

// Subscriber 是 Hub 的一个订阅者。Events 在 Hub 关闭或队列已满被注销后 close。
type Subscriber struct {
	events chan any
	done   chan struct{}
	closed bool // hub 侧已关闭
}

// NewHub 构造一个空 Hub。logger 可为 nil。
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		subs:   make(map[*Subscriber]struct{}),
		logger: logger,
	}
}

// Subscribe 创建一个新订阅者。返回值持有容量 hubBufSize 的缓冲队列和 done 信号。
func (h *Hub) Subscribe() *Subscriber {
	s := &Subscriber{
		events: make(chan any, hubBufSize),
		done:   make(chan struct{}),
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(s.events)
		close(s.done)
		return s
	}
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Publish 向所有订阅者非阻塞发送事件。队列已满的订阅者立刻被注销并关闭其输出。
// 不阻塞任何发布者（包括 Session runner）。
func (h *Hub) Publish(ev any) {
	var drop []*Subscriber
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	for s := range h.subs {
		select {
		case s.events <- ev:
		default:
			// 队列已满：注销该订阅者，稍后关闭。
			s.closed = true
			delete(h.subs, s)
			drop = append(drop, s)
		}
	}
	h.mu.Unlock()
	for _, s := range drop {
		close(s.events)
		close(s.done)
	}
}

// Close 注销并关闭所有订阅者；后续 Publish 不再有效。幂等。
// reason 会作为最后一个事件投递（类型由调用方解释）。
func (h *Hub) Close(reason any) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := h.subs
	h.subs = nil
	h.mu.Unlock()
	for s := range subs {
		if reason != nil {
			select {
			case s.events <- reason:
			default:
			}
		}
		s.closed = true
		close(s.events)
		close(s.done)
	}
}

// Unsubscribe 移除单个订阅者并关闭其输出。幂等。
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	if _, ok := h.subs[s]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.subs, s)
	h.mu.Unlock()
	s.closed = true
	close(s.events)
	close(s.done)
}

// Events 返回订阅者的事件通道；Hub 关闭或被注销后该通道 close。
func (s *Subscriber) Events() <-chan any { return s.events }

// Done 在订阅者被注销或 Hub 关闭后 close。
func (s *Subscriber) Done() <-chan struct{} { return s.done }

// IsClosed 返回该订阅者是否已由 Hub 侧关闭。
func (s *Subscriber) IsClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// SessionEndEvent 是 Hub 注销/关闭时发布的终态事件，SSE/WS 端可借此生成 ConversationFrame{Type:"session_end"}。
// Reason 取值 "closed"（Session 转入 Closed）或 "deleted"（Session 物理删除）。
type SessionEndEvent struct {
	Reason string
}
