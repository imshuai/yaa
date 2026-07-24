package session

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idGenerator 生成 ses_/msg_ 前缀 ULID。
// Turn ID 由调用方传入 RunTurn，不由 Manager 生成。
type idGenerator interface {
	NewSessionID() string
	NewMessageID() string
}

// ulidGen 是默认实现，使用 crypto/rand 保证全局唯一。
type ulidGen struct {
	entropy *ulid.MonotonicEntropy
	mu      sync.Mutex
}

func newULIDGen() *ulidGen {
	return &ulidGen{entropy: ulid.Monotonic(rand.Reader, 0)}
}

func (g *ulidGen) NewSessionID() string { return "ses_" + g.next() }
func (g *ulidGen) NewMessageID() string { return "msg_" + g.next() }

func (g *ulidGen) next() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), g.entropy).String()
}
