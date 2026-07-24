package api

import (
	"encoding/json"
	"net/http"
)

// Envelope 是所有 REST 响应的统一外壳。
type Envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// writeOK 写入成功 envelope（HTTP status 由调用方先 WriteHeader 或默认 200）。
func writeOK(w http.ResponseWriter, requestID string, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Code:      0,
		Message:   "ok",
		Data:      data,
		RequestID: requestID,
	})
}

// writeError 写入错误 envelope。code 使用 HTTP 状态加两位子码。
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status, code int, message string) {
	requestID := RequestIDFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Envelope{
		Code: code, Message: message, Data: nil, RequestID: requestID,
	}); err != nil {
		s.logger.Warn("api error response write failed", "request_id", requestID, "error", err)
	}
}
