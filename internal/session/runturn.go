package session

import (
	"context"
	"fmt"
	"sync"
)

// ErrAgentStopped 是业务层在关停时使用的 cause。
var ErrAgentStopped = fmt.Errorf("agent stopped")

// RunTurn 是 Manager 的核心调用：在 session runner FIFO 内执行一个完整 Agent turn。
//
// 约定（docs/session/integration.md §2）：
//   - callback 第一个写操作必须是 turn.AppendUser；
//   - Turn.Append/AppendUser 在 callback 生命周期内有效，直接提交（不二次排队）；
//   - 排队等待可由 ctx 取消；cancel 后 callback 不再执行，预留被释放；
//   - 同 session 的后续 task 等 callback 完成后才执行（runner FIFO）；
//   - RunTurn 返回 context.Cause(turnCtx)，保留 caller/agent/shutdown cause。
func (m *Manager) RunTurn(
	ctx context.Context,
	sessionID, turnID string,
	onQueued func(position int),
	fn func(context.Context, *Turn) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if turnID == "" {
		return fmt.Errorf("%w: empty turn id", ErrInvalidTurnID)
	}

	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return ErrManagerClosed
	}
	sess := m.sessions[sessionID]
	r := m.runners[sessionID]
	m.mu.RUnlock()
	if sess == nil {
		return fmt.Errorf("%w: session %s", ErrSessionNotFound, sessionID)
	}
	if r == nil {
		return fmt.Errorf("%w: session %s", ErrSessionNotFound, sessionID)
	}

	// 在 activeTurns 预留 turnID；重复则拒绝不入队。
	m.mu.Lock()
	turns := m.activeTurns[sessionID]
	if turns == nil {
		turns = map[string]*turnControl{}
		m.activeTurns[sessionID] = turns
	}
	if _, ok := turns[turnID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrTurnIDConflict, turnID)
	}
	position := len(turns) // 接受时前方任务数（运行+排队）
	m.mu.Unlock()

	turnCtx, cancel := context.WithCancelCause(ctx)
	tc := &turnControl{
		agentID: sess.AgentID,
		ctx:     turnCtx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel(context.Canceled)
		return ErrManagerClosed
	}
	m.activeTurns[sessionID][turnID] = tc
	m.mu.Unlock()

	// onQueued 在所有 Manager/runner 锁外调用。
	if onQueued != nil {
		onQueued(position)
	}

	// 唯一 defer：无论 enqueue 失败、queued cancel、Delete、panic 或 callback 完成，
	// 都从 activeTurns 移除并 close(done)。
	var (
		turn    *Turn
		runErr  error
		done    = make(chan error, 1)
	)
	defer func() {
		close(tc.done)
		m.mu.Lock()
		if activeMap := m.activeTurns[sessionID]; activeMap != nil {
			delete(activeMap, turnID)
		}
		m.mu.Unlock()
	}()

	task := func() (perr error) {
		defer func() {
			if rec := recover(); rec != nil {
				perr = fmt.Errorf("panic in turn: %v", rec)
			}
		}()
		if err := turnCtx.Err(); err != nil {
			return err
		}
		// session 可能 已被 Delete：若没 snapshot 则返回 not found
		m.mu.RLock()
		stillExists := m.sessions[sessionID] != nil
		m.mu.RUnlock()
		if !stillExists {
			return fmt.Errorf("%w: session %s deleted", ErrSessionNotFound, sessionID)
		}
		turn = &Turn{
			manager:   m,
			sessionID: sessionID,
			turnID:    turnID,
		}
		callbackErr := fn(turnCtx, turn)
		turn.mu.Lock()
		turn.closed = true
		turn.mu.Unlock()
		return callbackErr
	}

	// 投递 task 到 runner；同时监测 closing 和 r.stop 避免 send-on-closed-channel panic。
	select {
	case <-m.closing:
		cancel(ErrAgentStopped)
		return ErrManagerClosed
	case <-r.stop:
		cancel(ErrAgentStopped)
		return fmt.Errorf("%w: session %s deleted", ErrSessionNotFound, sessionID)
	case r.tasks <- func() error {
		err := task()
		done <- err
		return err
	}:
	}

	select {
	case err := <-done:
		runErr = err
		cancel(nil) // 回收 context 资源
	case <-m.closing:
		cancel(ErrAgentStopped)
		<-done
		runErr = ErrAgentStopped
	}

	if runErr == nil {
		return nil
	}
	// RunTurn 对调用方返回 context.Cause(turnCtx)，保留 caller/agent/shutdown cause
	if cause := context.Cause(turnCtx); cause != nil {
		return cause
	}
	return runErr
}

// CancelTurn 查找精确 (sessionID, turnID) 并调用其保存的 CancelCauseFunc。
// 不存在或已终态返回 ErrTurnNotActive。
// 不在 Manager.mu 内执行 cancel；只查 handle 后释放锁。
func (m *Manager) CancelTurn(sessionID, turnID string, cause error) error {
	m.mu.RLock()
	var tc *turnControl
	turns := m.activeTurns[sessionID]
	if turns != nil {
		tc = turns[turnID]
	}
	m.mu.RUnlock()
	if tc == nil {
		return fmt.Errorf("%w: %s/%s", ErrTurnNotActive, sessionID, turnID)
	}
	tc.cancel(cause)
	return nil
}

// CancelAgentTurns 收集某 Agent 的全部在途 turn 并逐一取消，等待它们退出或 ctx 到期。
// Manager closing 后仍可调用；无活动时幂等返回 nil。
func (m *Manager) CancelAgentTurns(ctx context.Context, agentID string, cause error) error {
	m.mu.RLock()
	controls := []*turnControl{}
	for _, turns := range m.activeTurns {
		for _, tc := range turns {
			if tc.agentID == agentID {
				controls = append(controls, tc)
			}
		}
	}
	m.mu.RUnlock()
	if len(controls) == 0 {
		return nil
	}
	for _, tc := range controls {
		tc.cancel(cause)
	}
	for _, tc := range controls {
		select {
		case <-tc.done:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	return nil
}

var _ = sync.Mutex{}
