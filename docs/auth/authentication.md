# 认证机制 (Authentication)

> 本文档描述 Yaa! Runtime 的认证系统设计，包括静态 Token、JWT、Identity 定义、认证流程及 Go 代码示例。

---

## 1. 概述

Yaa! 的 Remote API 默认受认证保护。认证系统负责验证请求方身份，授权系统负责控制身份对资源的访问权限。两者解耦设计，可独立扩展。

| 组件 | 职责 | 接口 |
|------|------|------|
| Authenticator | 验证 Token，返回 Identity | `Authenticate(token string) (*Identity, error)` |
| Authorizer | 检查 Identity 是否有权访问资源 | `Authorize(identity *Identity, action string, resource string) (bool, error)` |

**设计原则：**
- 零外部依赖，纯 Go 实现
- 支持多种认证方式，可通过配置切换
- 可配置免认证端点（如 `/health`、`/version`）
- 认证失败统一返回 `401 Unauthorized`，授权失败返回 `403 Forbidden`

---

## 2. Identity 定义

Identity 是认证通过后产生的身份对象，贯穿整个请求生命周期。

```go
// Identity 表示一个已认证的身份
type Identity struct {
    ID       string            // 唯一标识
    Name     string            // 人类可读名称
    Roles    []string          // 角色列表（用于 RBAC）
    Metadata map[string]any    // 扩展信息（如来源 IP、客户端类型等）
}

// String 用于日志输出（脱敏）
func (i *Identity) String() string {
    return fmt.Sprintf("Identity{id=%s, name=%s, roles=%v}", i.ID, i.Name, i.Roles)
}

// HasRole 检查是否拥有指定角色
func (i *Identity) HasRole(role string) bool {
    for _, r := range i.Roles {
        if r == role {
            return true
        }
    }
    return false
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 全局唯一，静态 Token 为 token name，JWT 为 subject |
| `Name` | `string` | 人类可读名称，便于日志与审计 |
| `Roles` | `[]string` | 角色列表，用于 RBAC 授权判断 |
| `Metadata` | `map[string]any` | 扩展字段，如客户端类型、请求来源等 |

---

## 3. 认证方式

### 3.1 静态 Token

最简单的认证方式，在配置文件中预声明 Token 列表。适合单用户或可信内部网络场景。

```yaml
# yaa.yaml
auth:
  enabled: true
  type: static
  tokens:
    - name: "default"
      token: "yaat-xxxxxxxxxxxxxxxx"
      roles: ["admin"]
    - name: "readonly"
      token: "yaat-yyyyyyyyyyyyyyyy"
      roles: ["viewer"]
  public_paths:
    - "/api/v1/health"
    - "/api/v1/version"
```

```go
// StaticAuthenticator 静态 Token 认证
type StaticAuthenticator struct {
    tokens map[string]*Identity // token → Identity 映射
}

func NewStaticAuthenticator(cfg StaticAuthConfig) *StaticAuthenticator {
    a := &StaticAuthenticator{tokens: make(map[string]*Identity)}
    for _, t := range cfg.Tokens {
        a.tokens[t.Token] = &Identity{
            ID:    t.Name,
            Name:  t.Name,
            Roles: t.Roles,
        }
    }
    return a
}

func (a *StaticAuthenticator) Authenticate(token string) (*Identity, error) {
    identity, ok := a.tokens[token]
    if !ok {
        return nil, ErrInvalidToken
    }
    return identity, nil
}
```

### 3.2 JWT 认证

JWT (JSON Web Token) 适合多用户、分布式或需要无状态验证的场景。

```yaml
# yaa.yaml
auth:
  enabled: true
  type: jwt
  jwt:
    secret: "${JWT_SECRET}"        # HMAC 密钥
    issuer: "yaa-runtime"          # 签发方
    audience: "yaa-client"         # 目标受众
    expire: "24h"                   # Token 有效期
```

```go
import (
    "encoding/json"
    "fmt"
    "time"
)

// JWTAuthenticator JWT 认证
type JWTAuthenticator struct {
    secret   []byte
    issuer   string
    audience string
}

type jwtClaims struct {
    Sub   string   `json:"sub"`           // Subject = Identity.ID
    Name  string   `json:"name"`          // Identity.Name
    Roles []string `json:"roles"`         // Identity.Roles
    Iss   string   `json:"iss"`           // 签发方
    Aud   string   `json:"aud"`           // 受众
    Exp   int64    `json:"exp"`           // 过期时间
    Iat   int64    `json:"iat"`           // 签发时间
}

// Authenticate 解析并验证 JWT，返回 Identity
func (a *JWTAuthenticator) Authenticate(token string) (*Identity, error) {
    claims, err := a.verify(token)
    if err != nil {
        return nil, fmt.Errorf("jwt verify failed: %w", err)
    }
    return &Identity{
        ID:    claims.Sub,
        Name:  claims.Name,
        Roles: claims.Roles,
        Metadata: map[string]any{
            "issuer": claims.Iss,
            "exp":    claims.Exp,
        },
    }, nil
}

// verify 验证签名与过期时间（简化示意，实际应使用 HMAC-SHA256）
func (a *JWTAuthenticator) verify(token string) (*jwtClaims, error) {
    parts := strings.Split(token, ".")
    if len(parts) != 3 {
        return nil, ErrInvalidToken
    }
    // 验证签名（伪代码，省略 base64 解码与 HMAC 比对）
    // 验证 iss、aud、exp
    var claims jwtClaims
    payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
    if err := json.Unmarshal(payload, &claims); err != nil {
        return nil, err
    }
    if claims.Iss != a.issuer {
        return nil, ErrInvalidIssuer
    }
    if time.Now().Unix() > claims.Exp {
        return nil, ErrTokenExpired
    }
    return &claims, nil
}
```

### 3.3 认证方式对比

| 特性 | 静态 Token | JWT |
|------|-----------|-----|
| 复杂度 | 低 | 中 |
| 无状态 | ✅ | ✅ |
| 多用户 | ❌（需手动管理） | ✅ |
| 过期控制 | ❌ | ✅ |
| 角色信息 | 配置中声明 | Token 内携带 |
| 吊销能力 | 删除配置即可 | 需黑名单机制 |
| 适用场景 | 单机/内部 | 多用户/分布式 |

---

## 4. 中间件集成

认证通过 HTTP 中间件拦截所有 Remote API 请求。

```go
// AuthMiddleware 认证中间件
func AuthMiddleware(auth Authenticator, publicPaths []string) func(http.Handler) http.Handler {
    publicSet := make(map[string]bool)
    for _, p := range publicPaths {
        publicSet[p] = true
    }
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 跳过免认证路径
            if publicSet[r.URL.Path] {
                next.ServeHTTP(w, r)
                return
            }
            // 提取 Token
            token := extractToken(r)
            if token == "" {
                http.Error(w, `{"error":"missing token"}`, http.StatusUnauthorized)
                return
            }
            // 认证
            identity, err := auth.Authenticate(token)
            if err != nil {
                http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
                return
            }
            // 注入 Identity 到 Context
            ctx := context.WithValue(r.Context(), identityKey{}, identity)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// extractToken 从 Authorization Header 提取 Token
func extractToken(r *http.Request) string {
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    // 兼容 query 参数（WebSocket 场景）
    return r.URL.Query().Get("token")
}
```

---

## 5. 认证流程图

```text
┌────────┐     HTTP/WS 请求     ┌──────────────────┐
│ Client │ ────────────────────▶ │ Remote API Server │
└────────┘                       └────────┬─────────┘
                                          │
                                          ▼
                               ┌─────────────────────┐
                               │  是否为免认证路径？    │
                               └────────┬───┬─────────┘
                                   Yes  │   │ No
                                       ▼   │
                                  放行请求 │
                                         ▼
                            ┌────────────────────────┐
                            │  提取 Authorization     │
                            │  Header / Query Token   │
                            └────────┬───────────────┘
                                     │
                                     ▼
                          ┌────────────────────────┐
                          │  Token 是否存在？        │
                          └────────┬───┬───────────┘
                            不存在  │   │ 存在
                                  ▼   │
                           401 Unauthorized
                                     ▼
                    ┌──────────────────────────────┐
                    │  Authenticator.Authenticate  │
                    │  (Static / JWT)              │
                    └────────┬─────────────────────┘
                             │
                    ┌────────┴────────┐
                    │  认证成功？      │
                    └───┬─────────┬───┘
                   失败  │         │ 成功
                       ▼         ▼
              401 Unauthorized  生成 Identity
                                 │
                                 ▼
                    ┌────────────────────────────┐
                    │  Identity 注入 Context     │
                    │  → 传递给下游 Handler      │
                    │  → Authorizer 做权限检查    │
                    └────────┬───────────────────┘
                             │
                             ▼
                    ┌────────────────────────────┐
                    │  Authorizer.Authorize      │
                    │  (RBAC 权限检查)            │
                    └────────┬───────────────────┘
                             │
                    ┌────────┴────────┐
                    │  授权成功？      │
                    └───┬─────────┬───┘
                   失败  │         │ 成功
                       ▼         ▼
              403 Forbidden   处理请求并返回
```

---

## 6. 错误处理

| 错误 | HTTP 状态码 | 说明 |
|------|-------------|------|
| `ErrMissingToken` | 401 | 未携带 Token |
| `ErrInvalidToken` | 401 | Token 格式错误或不存在 |
| `ErrTokenExpired` | 401 | JWT 已过期 |
| `ErrInvalidIssuer` | 401 | JWT 签发方不匹配 |
| `ErrUnauthorized` | 403 | 认证通过但无权限访问资源 |

所有错误均以 JSON 格式返回，便于客户端解析：

```json
{"error": "invalid token", "code": "ERR_INVALID_TOKEN"}
```

---

## 7. 配置参考

```yaml
auth:
  enabled: true              # 关闭则所有请求无需认证
  type: static              # static | jwt
  public_paths:             # 免认证路径
    - "/api/v1/health"
    - "/api/v1/version"
  tokens:                   # 静态 Token 列表（type=static 时生效）
    - name: "default"
      token: "yaat-xxxxx"
      roles: ["admin"]
  jwt:                      # JWT 配置（type=jwt 时生效）
    secret: "${JWT_SECRET}"
    issuer: "yaa-runtime"
    audience: "yaa-client"
    expire: "24h"
```

---

## 8. 未来扩展

| 扩展方向 | 说明 |
|----------|------|
| OAuth 2.0 | 支持第三方 IdP（GitHub、Google） |
| mTLS | 客户端证书双向认证，适合高安全场景 |
| API Key 轮换 | 静态 Token 支持自动轮换与过渡期 |
| JWT 黑名单 | 支持 JWT 主动吊销 |
| 速率限制 | 基于 Identity 的请求限流 |

---

*最后更新: 2025-07-17*
