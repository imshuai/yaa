package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/imshuai/yaa/internal/config"
	"github.com/imshuai/yaa/internal/session"
	"github.com/imshuai/yaa/internal/storage"
)

// registerSessionRoutes 注册所有 session 相关路由。
// Go 1.20 ServeMux 不支持路径参数，用最长前缀匹配 + 手动解析。
func (s *Server) registerSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/agents/", s.handleAgentsSessions) // :id/sessions
	mux.HandleFunc("/api/v1/sessions/", s.handleSessions)     // :id...sub-resource
}

// handleAgentsSessions 处理 /api/v1/agents/:id/sessions 的 POST/GET。
func (s *Server) handleAgentsSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sp := s.sessions
	ae := s.agentExists
	s.mu.Unlock()
	if sp == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime not ready")
		return
	}

	// 路径: /api/v1/agents/:id/sessions
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[1] != "sessions" {
		s.writeError(w, r, http.StatusNotFound, 40401, "resource not found")
		return
	}
	agentID, err := url.PathUnescape(parts[0])
	if err != nil || agentID == "" {
		s.writeError(w, r, http.StatusBadRequest, 40001, "invalid agent id")
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleCreateSession(w, r, sp, ae, agentID)
	case http.MethodGet:
		s.handleListSessions(w, r, sp, agentID)
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, 40501, "method not allowed")
	}
}

// handleSessions 处理 /api/v1/sessions/:id/... 子路由。
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sp := s.sessions
	s.mu.Unlock()
	if sp == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime not ready")
		return
	}

	// 路径: /api/v1/sessions/:id[/pause|/resume|/close|/clear|/messages|/messages/:msgid]
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	if rest == "" {
		s.writeError(w, r, http.StatusNotFound, 40401, "resource not found")
		return
	}
	parts := strings.SplitN(rest, "/", 3)
	sessionID, err := url.PathUnescape(parts[0])
	if err != nil || sessionID == "" {
		s.writeError(w, r, http.StatusBadRequest, 40001, "invalid session id")
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	subID := ""
	if len(parts) > 2 {
		subID = parts[2]
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		s.handleGetSession(w, r, sp, sessionID)
	case sub == "" && r.Method == http.MethodDelete:
		s.handleDeleteSession(w, r, sp, sessionID)
	case sub == "pause" && r.Method == http.MethodPost:
		s.handlePauseSession(w, r, sp, sessionID)
	case sub == "resume" && r.Method == http.MethodPost:
		s.handleResumeSession(w, r, sp, sessionID)
	case sub == "close" && r.Method == http.MethodPost:
		s.handleCloseSession(w, r, sp, sessionID)
	case sub == "clear" && r.Method == http.MethodPost:
		s.handleClearMessages(w, r, sp, sessionID)
	case sub == "messages" && r.Method == http.MethodPost:
		s.handlePostMessage(w, r, sp, sessionID)
	case sub == "messages" && r.Method == http.MethodGet:
		s.handleListMessages(w, r, sp, sessionID)
	case sub == "messages" && r.Method == http.MethodDelete:
		s.handleDeleteMessage(w, r, sp, sessionID, subID)
	case sub == "events" && r.Method == http.MethodGet:
		s.handleSSEEvents(w, r, sp, sessionID)
	case sub == "stream" && r.Method == http.MethodGet:
		s.handleWSStream(w, r, sp, sessionID)
	default:
		s.writeError(w, r, http.StatusMethodNotAllowed, 40501, "method not allowed")
	}
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, ae AgentExistsProvider, agentID string) {
	if ae != nil && !ae.AgentExists(agentID) {
		s.writeError(w, r, http.StatusNotFound, 40401, "agent not found")
		return
	}
	var req createSessionRequest
	if err := decodeBody(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, 40001, "invalid request body")
		return
	}
	sessReq := session.CreateRequest{
		AgentID:  agentID,
		Policy:   req.Policy,
		Metadata: req.Metadata,
	}
	sess, err := sp.Create(r.Context(), sessReq)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusCreated, toSessionDTO(sess))
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, sp SessionProvider, agentID string) {
	q := r.URL.Query()
	page := parsePage(q.Get("page"), 1)
	pageSize := parsePageSize(q.Get("page_size"), 20, 100)
	var stateFilter *session.State
	if stStr := q.Get("state"); stStr != "" {
		st := session.State(stStr)
		if !isValidStateStr(stStr) {
			s.writeError(w, r, http.StatusBadRequest, 40001, "invalid state filter")
			return
		}
		stateFilter = &st
	}
	items, total, err := sp.List(r.Context(), agentID, session.ListQuery{
		State: stateFilter, Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	dtos := make([]sessionDTO, 0, len(items))
	for _, sess := range items {
		dtos = append(dtos, toSessionDTO(sess))
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, pagedData{
		Items: dtos, Total: total, Page: page, PageSize: pageSize,
	})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	sess, err := sp.Get(r.Context(), sessionID)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, toSessionDTO(sess))
}

func (s *Server) handlePauseSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	if err := sp.Pause(r.Context(), sessionID); err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	s.writeSessionStateResult(w, r, sp, sessionID)
}

func (s *Server) handleResumeSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	if err := sp.Resume(r.Context(), sessionID); err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	s.writeSessionStateResult(w, r, sp, sessionID)
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	if err := sp.Close(r.Context(), sessionID); err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	s.writeSessionStateResult(w, r, sp, sessionID)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	if err := sp.Delete(r.Context(), sessionID); err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, deleteSessionDTO{ID: sessionID, Deleted: true})
}

func (s *Server) handleClearMessages(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	n, err := sp.ClearMessages(r.Context(), sessionID)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	sess, err := sp.Get(r.Context(), sessionID)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, clearMessagesDTO{
		ID:           sessionID,
		DeletedCount: n,
		MessageCount: 0,
		UpdatedAt:    sess.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	})
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	q := r.URL.Query()
	page := parsePage(q.Get("page"), 1)
	pageSize := parsePageSize(q.Get("page_size"), 50, 200)
	role := q.Get("role")
	if role != "" && role != "user" && role != "assistant" && role != "tool" {
		s.writeError(w, r, http.StatusBadRequest, 40001, "invalid role filter")
		return
	}
	after := q.Get("after")
	if after != "" && page > 1 {
		s.writeError(w, r, http.StatusBadRequest, 40001, "after cannot be used with page>1")
		return
	}
	msgs, total, err := sp.ListMessages(r.Context(), sessionID, session.ListMessagesQuery{
		Role: role, After: after, Page: page, PageSize: pageSize,
	})
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	dtos := make([]messageDTO, 0, len(msgs))
	for _, m := range msgs {
		dtos = append(dtos, toMessageDTO(sessionID, m))
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, messagesPagedData{
		Items: dtos, Total: total, Page: page, PageSize: pageSize,
	})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID, msgID string) {
	if msgID == "" {
		s.writeError(w, r, http.StatusBadRequest, 40001, "message id required")
		return
	}
	ids, err := sp.DeleteMessage(r.Context(), sessionID, msgID)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, map[string]any{
		"deleted":       true,
		"message_ids":   ids,
		"deleted_count": len(ids),
	})
}

// writeSessionStateResult 对 pause/resume/close 成功后返回简短 DTO。
func (s *Server) writeSessionStateResult(w http.ResponseWriter, r *http.Request, sp SessionProvider, sessionID string) {
	sess, err := sp.Get(r.Context(), sessionID)
	if err != nil {
		s.writeSessionError(w, r, err)
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, toSessionShortDTO(sess))
}

// writeSessionError 把 session 稳定错误映射到 HTTP / code。
func (s *Server) writeSessionError(w http.ResponseWriter, r *http.Request, err error) {
	// 先检查更具体的 SnapshotTooLarge，再检查 PersistenceFailed
	if errors.Is(err, session.ErrSessionSnapshotTooLarge) {
		s.writeError(w, r, http.StatusUnprocessableEntity, 42201, err.Error())
		return
	}
	if errors.Is(err, session.ErrPersistenceFailed) {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime unavailable")
		return
	}
	if errors.Is(err, session.ErrRestoreFailed) {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime unavailable")
		return
	}
	if errors.Is(err, session.ErrManagerClosed) {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime unavailable")
		return
	}
	if errors.Is(err, session.ErrSchemaUnsupported) {
		s.writeError(w, r, http.StatusInternalServerError, 50001, "unsupported schema")
		return
	}
	if errors.Is(err, session.ErrSessionNotFound) || errors.Is(err, session.ErrMessageNotFound) || errors.Is(err, session.ErrAgentNotFound) {
		s.writeError(w, r, http.StatusNotFound, 40401, "resource not found")
		return
	}
	if errors.Is(err, session.ErrTurnNotActive) {
		s.writeError(w, r, http.StatusNotFound, 40401, "resource not found")
		return
	}
	if errors.Is(err, session.ErrSessionClosed) || errors.Is(err, session.ErrSessionPaused) || errors.Is(err, session.ErrSessionExpired) || errors.Is(err, session.ErrInvalidStateTransition) {
		s.writeError(w, r, http.StatusConflict, 40901, "state not allowed")
		return
	}
	if errors.Is(err, session.ErrInvalidMessage) || errors.Is(err, session.ErrSessionConfigInvalid) || errors.Is(err, session.ErrInvalidTurnID) || errors.Is(err, session.ErrTurnIDConflict) {
		s.writeError(w, r, http.StatusBadRequest, 40001, "invalid request")
		return
	}
	if errors.Is(err, session.ErrInvalidMessageSequence) || errors.Is(err, session.ErrMessageTooLarge) || errors.Is(err, session.ErrMessageLimitExceeded) {
		s.writeError(w, r, http.StatusUnprocessableEntity, 42201, "session semantic error")
		return
	}
	if errors.Is(err, session.ErrCapacityExceeded) {
		s.writeError(w, r, http.StatusTooManyRequests, 42901, "capacity exceeded")
		return
	}
	// Storage ErrValueTooLarge
	if errors.Is(err, storage.ErrValueTooLarge) {
		s.writeError(w, r, http.StatusUnprocessableEntity, 42201, "snapshot too large")
		return
	}
	s.writeError(w, r, http.StatusInternalServerError, 50001, "internal error")
}

// decodeBody 解析 JSON 请求。空 body 视为空结构。
func decodeBody(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// pagedData 是返回标准分页结构。
type pagedData struct {
	Items    any `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type messagesPagedData struct {
	Items    []messageDTO `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

func parsePage(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	return n
}

func parsePageSize(s string, def, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func isValidStateStr(s string) bool {
	return s == "created" || s == "active" || s == "paused" || s == "closed"
}

// 确保 config import 被使用（createSessionRequest 引用 config.SessionOverride）。
var _ = config.SessionOverride{}
