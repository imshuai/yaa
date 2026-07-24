package session

import (
	"fmt"
	"time"
)

// State 是 Session 生命周期状态。
type State string

const (
	StateCreated State = "created"
	StateActive  State = "active"
	StatePaused  State = "paused"
	StateClosed  State = "closed"
)

// transitions 定义合法状态转换，不在此表中的转换返回 ErrInvalidStateTransition。
var transitions = map[State]map[State]bool{
	StateCreated: {
		StateActive: true,
		StatePaused: true,
		StateClosed: true,
	},
	StateActive: {
		StatePaused: true,
		StateClosed: true,
	},
	StatePaused: {
		StateActive: true,
		StateClosed: true,
	},
	StateClosed: {}, // 终态，但 Close 对 Closed 幂等 nil
}

// canTransition 返回是否允许从 from 转到 to。
func canTransition(from, to State) bool {
	return transitions[from][to]
}

// desiredState 按 max_lifetime 与 TTL 决定 cleanup/Restore 应当转向的状态。
func desiredState(s *Session, now time.Time) State {
	if s.Policy.MaxLifetime > 0 && !now.Before(s.CreatedAt.Add(s.Policy.MaxLifetime)) {
		return StateClosed
	}
	if s.Policy.TTL > 0 &&
		(s.State == StateCreated || s.State == StateActive) &&
		!now.Before(s.LastActivityAt.Add(s.Policy.TTL)) {
		return StatePaused
	}
	return s.State
}

// stateAllowed 报告某操作是否允许在当前状态下执行。
// 返回 nil 或适当的稳定错误（不包装 OpError，由调用方包装）。
func stateAllowed(op string, st State) error {
	switch op {
	case "run_turn":
		switch st {
		case StateCreated, StateActive:
			return nil
		case StatePaused:
			return ErrSessionPaused
		case StateClosed:
			return ErrSessionClosed
		}
	case "pause":
		if st == StateActive {
			return nil
		}
		if st == StateClosed {
			return ErrSessionClosed
		}
		return ErrInvalidStateTransition
	case "resume":
		if st == StatePaused {
			return nil
		}
		if st == StateClosed {
			return ErrSessionClosed
		}
		return ErrInvalidStateTransition
	case "delete_message", "clear_messages":
		if st == StateClosed {
			return ErrSessionClosed
		}
		return nil
	case "close":
		// Close 对任意非 Closed 合法；Closed 幂等（调用方单独处理）。
		if st == StateClosed {
			return nil
		}
		return nil
	case "delete":
		return nil
	}
	return fmt.Errorf("session: unknown op %q", op)
}
