# Auth 与 Remote API 集成

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/integration.md`
> 依赖: `docs/architecture.md` §3.11 Remote API, §3.14 Auth

---

## 1. 集成架构

Auth 系统作为 Remote API 中间件运行，在请求到达业务 Handler 之前完成认证与授权。

```text
HTTP Request
  │
  ▼
┌─────────────────────────────────┐
│         Remote API Server       │
│                                 │
│  ┌───────────┐  ┌───────────┐  │
│  │  Auth MW  │→ │ RBAC MW   │  │
│  │ (认证)     │  │ (授权)    │  │
│  └─────┬─────┘  └─────┬─────┘  │
│        │              │        │
│        ▼              ▼        │
│  ┌─────────────────────────┐   │
│  │    Business Handler     │   │
│  └─────────────────────────┘   │
└─────────────────────────────────┘
```

## 2. 中间件链

```go
// 构建中间件链
func (s *Server) setupMiddleware(r *mux.Router) {
    r.Use(s.loggingMiddleware())   // 1. 请求日志
    r.Use(s.recoveryMiddleware()) // 2. Panic 恢复
    r.Use(s.authMiddleware())      // 3. Token 认证
    r.Use(s.rbacMiddleware())      // 4. RBAC 授权
    r.Use(s.rateLimitMiddleware()) // 5. 速率限制
}
```

## 3. 认证中间件

```go
func (s *Server) authMiddleware() mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 跳过白名单端点
            if s.auth.IsPublicPath(r.URL.Path) {
                next.ServeHTTP(w, r)
                return
            }
            // 提取 Token
            token := extractToken(r)
            if token == "" {
                writeError(w, http.StatusUnauthorized, "missing token")
                return
            }
            // 认证
            identity, err := s.auth.Authenticate(token)
            if err != nil {
                writeError(w, http.StatusUnauthorized, "invalid token")
                return
            }
            // 注入 Identity 到 Context
            ctx := context.WithValue(r.Context(), identityKey{}, identity)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

## 4. RBAC 授权中间件

```go
func (s *Server) rbacMiddleware() mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            identity, ok := r.Context().Value(identityKey{}).(*Identity)
            if !ok {
                next.ServeHTTP(w, r) // 公开端点无 Identity
                return
            }
            action := r.Method + ":" + r.URL.Path
            allowed, err := s.auth.Authorize(identity, action, r.URL.Path)
            if err != nil || !allowed {
                writeError(w, http.StatusForbidden, "access denied")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## 5. Token 提取

```go
// 支持多种 Token 传递方式
func extractToken(r *http.Request) string {
    // 1. Authorization Header
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    // 2. Query 参数（WebSocket 场景）
    if token := r.URL.Query().Get("token"); token != "" {
        return token
    }
    // 3. Cookie（WebUI 场景）
    if c, err := r.Cookie("yaa_token"); err == nil {
        return c.Value
    }
    return ""
}
```

## 6. 公开端点白名单

| 端点 | 说明 |
|------|------|
| `GET /api/v1/health` | 健康检查 |
| `GET /api/v1/version` | 版本信息 |
| `POST /api/v1/auth/login` | 登录获取 Token |

```go
func (a *AuthService) IsPublicPath(path string) bool {
    publicPaths := []string{
        "/api/v1/health",
        "/api/v1/version",
        "/api/v1/auth/login",
    }
    for _, p := range publicPaths {
        if path == p {
            return true
        }
    }
    return false
}
```

## 7. WebSocket 认证

WebSocket 连接在握手阶段通过 Query 参数传递 Token：

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    token := r.URL.Query().Get("token")
    identity, err := s.auth.Authenticate(token)
    if err != nil {
        w.WriteHeader(http.StatusUnauthorized)
        return
    }
    conn, _ := s.upgrader.Upgrade(w, r, nil)
    defer conn.Close()
    // 绑定 Identity 到连接
    s.wsHub.Register(identity.ID, conn)
}
```

---

*最后更新: 2025-07-17*
