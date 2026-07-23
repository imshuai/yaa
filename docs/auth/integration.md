# Auth 与 Remote API 集成

> 文档路径: `docs/auth/integration.md`
> 依赖: [Auth 设计](README.md)、[Remote API 路由总表](../remote-api/INDEX.md)

---

## 1. 所有权

Auth 包只提供 `Authenticator`、`Authorizer`、`Identity` 和 RBAC 实现，不拥有 HTTP router、REST envelope 或 public path 匹配。Remote API Server 是下列对象的唯一 owner：

- `RouteSpec` 与 37 条路由注册；
- `auth.enabled` / 精确 public path bypass；
- Bearer Header 提取和 Identity context；
- 一次 AuthN -> AuthZ 调用；
- 统一 REST error envelope。

全局 middleware 只安装 logging、recovery 和 rate limit。不得再安装独立 auth middleware、RBAC middleware 或在 handler 内重复鉴权。

```text
matched RouteSpec
  -> auth disabled or exact public path? -> business handler
  -> extract Bearer
  -> Authenticator.Authenticate
  -> Authorizer.Authorize(Action, Resource)
  -> business handler
```

## 2. RouteSpec

```go
type Transport string

const (
    TransportHTTP      Transport = "http"
    TransportWebSocket Transport = "websocket"
)

type RouteSpec struct {
    Method    string    // wire HTTP method; WebSocket is GET
    Pattern   string    // gorilla/mux form, for example /sessions/{id}
    Action    string
    Resource  string
    Transport Transport
}
```

`RouteSpec` 只定义在 `internal/api`。Remote 总表把 WebSocket 路由列为 wire `GET`，注册时设置 `Method=http.MethodGet, Transport=TransportWebSocket`；其余路由使用表中 method + `TransportHTTP`。总表的 `:id` 只用于文档展示，注册前统一转换为 mux 的 `{id}`，注册测试比较规范化后的 method、pattern、action、resource、transport。

## 3. 唯一路由 wrapper

```go
func (s *Server) registerRoute(r *mux.Router, spec RouteSpec, h http.Handler) {
    protected := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
        if !s.authEnabled || s.publicPaths[req.URL.Path] {
            h.ServeHTTP(w, req)
            return
        }

        token := bearerToken(req.Header.Get("Authorization"))
        if token == "" {
            s.writeError(w, req, http.StatusUnauthorized, 40101, "unauthorized")
            return
        }
        identity, err := s.authn.Authenticate(token)
        if err != nil {
            s.writeError(w, req, http.StatusUnauthorized, credentialCode(err), "unauthorized")
            return
        }
        allowed, err := s.authz.Authorize(identity, spec.Action, spec.Resource)
        if err != nil || !allowed {
            s.writeError(w, req, http.StatusForbidden, 40301, "forbidden")
            return
        }
        ctx := auth.ContextWithIdentity(req.Context(), identity)
        h.ServeHTTP(w, req.WithContext(ctx))
    })

    r.Handle(spec.Pattern, protected).Methods(spec.Method)
}
```

Disabled/public bypass 必须同时跳过认证和授权。非 public 的受保护路由不可能在没有 Identity 时进入业务 handler。Authorizer error 对客户端统一为 403，详细分类只写脱敏日志。

`s.publicPaths` 来自 Config Validator 已规范化的 `runtime.auth.public_paths`，构造后只读。匹配只使用 URL path 精确比较，不匹配前缀、query 或 route template；不得硬编码 health/version。

## 4. Bearer 与错误码

```go
func bearerToken(header string) string {
    scheme, token, ok := strings.Cut(header, " ")
    if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" ||
        strings.ContainsAny(token, " \t") {
        return ""
    }
    return token
}

func credentialCode(err error) int {
    if errors.Is(err, auth.ErrJWTInvalid) {
        return 40102
    }
    return 40101
}
```

HTTP、SSE 和 WebSocket upgrade 都只接受 `Authorization: Bearer <token>`。不接受 query、Cookie、应用层首帧 Token 或兑换 ticket。缺失/格式错误/静态 Token 无效使用 `40101`；任何 JWT 签名、算法、issuer、audience、subject、roles、exp 或 nbf 校验失败都包装 `auth.ErrJWTInvalid` 并使用 `40102`。不得按错误字符串判断。

`Server.writeError` 的唯一 envelope 定义见 [Remote API](../remote-api/INDEX.md#4-rest-envelope)；Auth 包不依赖 Remote API 类型。

## 5. WebSocket

WebSocket route 使用 wire GET 经过同一 wrapper，再调用 upgrader。启用 Auth 的非 public WS 请求必有 Identity：

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
    identity, ok := auth.IdentityFromContext(r.Context())
    if s.authEnabled && !s.publicPaths[r.URL.Path] && !ok {
        s.writeError(w, r, http.StatusUnauthorized, 40101, "unauthorized")
        return
    }
    conn, err := s.upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    principalID := "anonymous"
    if ok {
        principalID = identity.ID
    }
    s.wsHub.Register(principalID, conn)
}
```

不能设置 Authorization Header 的浏览器客户端必须通过同源后端代理；v1 不增加另一种凭据通道。

---

*最后更新: 2026-07-22*
