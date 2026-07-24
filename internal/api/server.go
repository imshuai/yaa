package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"golang.org/x/exp/slog"
)

// BuildInfo 由 ldflags 注入；缺省值为占位符。
var (
	Version   = "0.0.0-dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

// AgentCounts 是 health 响应中的 Agent 运行态快照。
type AgentCounts struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Paused  int `json:"paused"`
	Stopped int `json:"stopped"`
}

// HealthData 是 /api/v1/health 响应 data 字段。
type HealthData struct {
	Status        string            `json:"status"`
	Ready         bool              `json:"ready"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Agents        AgentCounts       `json:"agents"`
	Components    map[string]string `json:"components"`
}

// HealthProvider 由 Runtime 注入，返回当前运行态快照。
// 未注入时 Server 视为 Not Ready。
type HealthProvider interface {
	Health() HealthData
}

// Server 是 Remote API 的 HTTP 服务。
type Server struct {
	addr     string
	logger   *slog.Logger
	server   *http.Server
	health   HealthProvider
	started  time.Time
	mu       sync.Mutex
	listener *http.Server
}

// NewServer 创建 Remote API Server。addr 为监听地址；health 可为 nil（视为未就绪）。
func NewServer(addr string, health HealthProvider, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		addr:    addr,
		logger:  logger,
		health:  health,
		started: time.Now(),
	}
	mux := http.NewServeMux()
	s.register(mux)
	s.server = &http.Server{
		Addr:    addr,
		Handler: requestIDMiddleware(recoverMiddleware(mux)),
	}
	return s
}

// register 注册全部 v1 路由。
func (s *Server) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/health", s.methodGet(s.handleHealth))
	mux.HandleFunc("/api/v1/version", s.methodGet(s.handleVersion))
	mux.HandleFunc("/", s.notFound)
}

// Start 同步启动 HTTP 监听并在后台阻塞 serve。
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return errors.New("api: server already started")
	}
	s.started = time.Now()
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("api: listen %s: %w", s.addr, err)
	}
	s.server.Addr = ln.Addr().String()
	s.listener = s.server
	s.mu.Unlock()
	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("api server stopped with error", err)
		}
	}()
	return nil
}

// Shutdown 优雅关闭 HTTP 服务。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.listener
	s.listener = nil
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// handleHealth 返回 readiness；未就绪返回 503 + 50301。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	data := s.currentHealth()
	data.Status = normalizeStatus(data.Status, data.Ready)
	if !data.Ready || data.Status == "unhealthy" {
		s.writeError(w, r, http.StatusServiceUnavailable, 50301, "runtime not ready")
		return
	}
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, data)
}

// handleVersion 返回构建信息。
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeOK(w, RequestIDFromContext(r.Context()), http.StatusOK, map[string]string{
		"version":    Version,
		"git_commit": GitCommit,
		"build_time": BuildTime,
		"go_version": "go1.20.14",
	})
}

// currentHealth 取注入 provider 的快照，否则 Not Ready。
func (s *Server) currentHealth() HealthData {
	if s.health == nil {
		return HealthData{
			Status:        "not_ready",
			Ready:         false,
			UptimeSeconds: int64(time.Since(s.started).Seconds()),
			Agents:        AgentCounts{},
			Components:    map[string]string{},
		}
	}
	d := s.health.Health()
	d.UptimeSeconds = int64(time.Since(s.started).Seconds())
	return d
}

// normalizeStatus 按文档语义规整 status 文本。
func normalizeStatus(status string, ready bool) string {
	switch {
	case !ready:
		return "not_ready"
	case status == "":
		return "healthy"
	default:
		return status
	}
}


// methodGet 包装仅接受 GET 的 handler，其余返回 40513。
func (s *Server) methodGet(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, r, http.StatusMethodNotAllowed, 40501, "method not allowed")
			return
		}
		next(w, r)
	}
}

// recoverMiddleware 捕获 panic 并返回 50011。
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				debug.PrintStack()
				rid := RequestIDFromContext(r.Context())
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Request-ID", rid)
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(Envelope{Code: 50011, Message: "internal error", Data: nil, RequestID: rid})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// notFound 处理未匹配路由，返回 40401。
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, http.StatusNotFound, 40401, "resource not found")
}

// Addr 返回实际监听地址；未启动时返回配置的 addr。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil && s.server.Addr != "" {
		return s.server.Addr
	}
	return s.addr
}
