package session

import (
	"context"
	"fmt"
)

// ListMessages 分页读取已提交消息，按 CreatedAt、再按 Message ID 升序返回。
// 支持 role 过滤、after 增量读取、page/page_size。查询返回深拷贝。
func (m *Manager) ListMessages(ctx context.Context, sessionID string, q ListMessagesQuery) ([]SessionMessage, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if q.After != "" && page > 1 {
		return nil, 0, fmt.Errorf("%w: after cannot be used with page>1", ErrInvalidMessageSequence)
	}

	m.mu.RLock()
	s := m.sessions[sessionID]
	m.mu.RUnlock()
	if s == nil {
		return nil, 0, fmt.Errorf("%w: session %s", ErrSessionNotFound, sessionID)
	}
	snap := s.clone()

	// 排序：按 CreatedAt 再 ID 升序
	var ordered []SessionMessage
	for _, m2 := range snap.Messages {
		ordered = append(ordered, m2)
	}
	sortMessagesByTimeID(ordered)

	// after 增量
	if q.After != "" {
		idx := -1
		for i, m2 := range ordered {
			if m2.ID == q.After {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, 0, fmt.Errorf("%w: message %s", ErrMessageNotFound, q.After)
		}
		ordered = ordered[idx+1:]
		// after 模式：page 始终从 start with 1, page=1
		total := len(ordered)
		start := (page - 1) * pageSize
		if start >= total {
			return []SessionMessage{}, total, nil
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		return deepCopyMessages(ordered[start:end]), total, nil
	}

	// role 过滤
	if q.Role != "" {
		filtered := ordered[:0]
		for _, m2 := range ordered {
			if m2.Payload.Role == q.Role {
				filtered = append(filtered, m2)
			}
		}
		ordered = filtered
	}

	total := len(ordered)
	start := (page - 1) * pageSize
	if start >= total {
		return []SessionMessage{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return deepCopyMessages(ordered[start:end]), total, nil
}

// DeleteMessage 删除目标消息；若属于 Tool unit，则原子删除 assistant Tool call 与全部 tool results。
// 操作只允许 created/active/paused；Closed 返回 ErrSessionClosed。
func (m *Manager) DeleteMessage(ctx context.Context, sessionID, messageID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var deleted []string
	err := m.runInSession(sessionID, func() error {
		s, err := m.getSnapshotLocked(sessionID)
		if err != nil {
			return err
		}
		if err := stateAllowed("delete_message", s.State); err != nil {
			return err
		}
		idx := -1
		for i, m2 := range s.Messages {
			if m2.ID == messageID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: message %s", ErrMessageNotFound, messageID)
		}
		ids := []string{messageID}
		msg := s.Messages[idx]
		// 若是 assistant tool call 消息，删除其后所有对应 tool results
		if msg.Payload.Role == "assistant" && len(msg.Payload.ToolCalls) > 0 {
			calls := msg.Payload.ToolCalls
			callIDs := make(map[string]bool, len(calls))
			for _, c := range calls {
				callIDs[c.ID] = true
			}
			// 删除从 idx 开始的紧邻的 tool 消息中 ToolCallID 匹配的
			// 注意文档说一一对应顺序，所以紧邻的该段是连续的。这里保守做：覆盖连续的。
			j := idx + 1
			for ; j < len(s.Messages); j++ {
				tm := s.Messages[j]
				if tm.Payload.Role != "tool" {
					break
				}
				if !callIDs[tm.Payload.ToolCallID] {
					break
				}
				ids = append(ids, tm.ID)
			}
		}
		// 反向 若 是 tool 消息，需 校验 对应 assistant tool call 同时删除
		if msg.Payload.Role == "tool" {
			// 回看上一个 assistant 是否有收益该 tool call
			for i := idx - 1; i >= 0; i-- {
				pm := s.Messages[i]
				if pm.Payload.Role == "tool" {
					continue
				}
				if pm.Payload.Role == "assistant" && len(pm.Payload.ToolCalls) > 0 {
					// 是否包含本 tool call
					has := false
					for _, c := range pm.Payload.ToolCalls {
						if c.ID == msg.Payload.ToolCallID {
							has = true
							break
						}
					}
					if has {
						ids = append([]string{pm.ID}, ids...)
						// 删除该 assistant 的所有 tool results
						for k := i + 1; k < len(s.Messages); k++ {
							tm := s.Messages[k]
							if tm.Payload.Role != "tool" {
								break
							}
							// 是否属于该 assistant tool calls
							belongs := false
							for _, c := range pm.Payload.ToolCalls {
								if c.ID == tm.Payload.ToolCallID {
									belongs = true
									break
								}
							}
							if !belongs {
								break
							}
							if !containsString(ids, tm.ID) {
								ids = append(ids, tm.ID)
							}
						}
					}
				}
				break // 只看紧邻上一条 assistant
			}
		}
		// 构造候选消息集
		delSet := make(map[string]bool, len(ids))
		for _, id := range ids {
			delSet[id] = true
		}
		cand := s.clone()
		filtered := cand.Messages[:0]
		for _, m2 := range cand.Messages {
			if !delSet[m2.ID] {
				filtered = append(filtered, m2)
			}
		}
		cand.Messages = filtered
		// 重新校验完整序列：空或首条为 user，且无悬空 tool
		if err := validateBatchSequence(cand.Messages); err != nil {
			return err
		}
		cand.UpdatedAt = m.clock.Now().UTC()
		if err := m.commit(cand); err != nil {
			return err
		}
		deleted = ids
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// ClearMessages 删除全部消息，保留 Session state/policy/metadata。
// Closed 返回 ErrSessionClosed；空历史是 no-op（返回 0）。
func (m *Manager) ClearMessages(ctx context.Context, sessionID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var cleared int
	err := m.runInSession(sessionID, func() error {
		s, err := m.getSnapshotLocked(sessionID)
		if err != nil {
			return err
		}
		if err := stateAllowed("clear_messages", s.State); err != nil {
			return err
		}
		if len(s.Messages) == 0 {
			return nil // no-op
		}
		cleared = len(s.Messages)
		cand := s.clone()
		cand.Messages = nil
		now := m.clock.Now().UTC()
		cand.UpdatedAt = now
		return m.commit(cand)
	})
	return cleared, err
}

// ListMessagesQuery 是 ListMessages 的参数。
type ListMessagesQuery struct {
	Role     string // user|assistant|tool；空表示全部
	After    string // 增量读取的 message ID
	Page     int
	PageSize int
}
