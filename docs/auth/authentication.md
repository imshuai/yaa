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
- 不依赖外部鉴权服务；JWT 使用固定版本的成熟 Go 库
- 支持多种认证方式，可通过配置切换
- 可配置免认证端点（如 `/api/v1/health`、`/api/v1/version`）
- 认证失败统一返回 `401 Unauthorized`，授权失败返回 `403 Forbidden`

---

## 2. Identity 定义

Identity 是认证通过后产生的身份对象，贯穿整个请求生命周期。

```go
// Identity 表示一个已认证的身份
type Identity struct {
    ID     string         // 唯一标识
    Name   string         // 人类可读名称
    Roles  []string       // 角色列表（用于 RBAC）
    Claims map[string]any // 认证扩展声明，不含原始 Token
}

func cloneIdentity(src *Identity) *Identity {
    if src == nil {
        return nil
    }
    dst := *src
    dst.Roles = append([]string(nil), src.Roles...)
    if src.Claims != nil {
        dst.Claims = make(map[string]any, len(src.Claims))
        for k, v := range src.Claims {
            dst.Claims[k] = v
        }
    }
    return &dst
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
| `Claims` | `map[string]any` | JWT/认证扩展声明；不得保存原始 Token |

v1 的 Claims 值只允许不可变标量或 `time.Time`；不放入 map/slice/pointer。这样 `cloneIdentity` 的 map 复制足以隔离请求。Auth 包还拥有唯一的 context key；调用方不能自行用 string key 注入身份：

```go
type identityContextKey struct{}

func ContextWithIdentity(ctx context.Context, identity *Identity) context.Context {
    return context.WithValue(ctx, identityContextKey{}, cloneIdentity(identity))
}

func IdentityFromContext(ctx context.Context) (*Identity, bool) {
    identity, ok := ctx.Value(identityContextKey{}).(*Identity)
    if !ok || identity == nil {
        return nil, false
    }
    return cloneIdentity(identity), true
}
```

---

## 3. 认证方式

```go
var (
    ErrInvalidToken = errors.New("invalid static token")
    ErrJWTInvalid   = errors.New("invalid jwt")
)
```

两个 sentinel 用于 Remote API 的稳定错误码映射；Authenticator 可以包装它们，但不得把底层解析错误写入响应。

### 3.1 静态 Token

最简单的认证方式，在配置文件中预声明 Token 列表。适合单用户或可信内部网络场景。

```yaml
# yaa.yaml
runtime:
  auth:
    enabled: true
    token_type: static
    tokens:
      - name: "default"
        token: "${YAA_ADMIN_TOKEN}"
        roles: ["admin"]
      - name: "readonly"
        token: "${YAA_READONLY_TOKEN}"
        roles: ["viewer"]
    public_paths:
      - "/api/v1/health"
      - "/api/v1/version"
```

```go
import (
    "crypto/sha256"
    "fmt"
)

// StaticAuthenticator 静态 Token 认证
type StaticAuthenticator struct {
    tokens map[[32]byte]Identity // SHA-256(token) → immutable Identity snapshot
}

func NewStaticAuthenticator(tokens []config.TokenConfig) (*StaticAuthenticator, error) {
    a := &StaticAuthenticator{tokens: make(map[[32]byte]Identity)}
    for _, t := range tokens {
        if t.Name == "" || t.Token == "" || len(t.Roles) == 0 {
            return nil, fmt.Errorf("invalid static token %q", t.Name)
        }
        key := sha256.Sum256([]byte(t.Token))
        if _, exists := a.tokens[key]; exists {
            return nil, fmt.Errorf("duplicate static token %q", t.Name)
        }
        a.tokens[key] = Identity{
            ID:    t.Name,
            Name:  t.Name,
            Roles: append([]string(nil), t.Roles...),
        }
    }
    return a, nil
}

func (a *StaticAuthenticator) Authenticate(token string) (*Identity, error) {
    key := sha256.Sum256([]byte(token))
    identity, ok := a.tokens[key]
    if !ok {
        return nil, ErrInvalidToken
    }
    return cloneIdentity(&identity), nil
}
```

构造时和每次返回时都复制 Roles/Claims；请求代码因此不能修改认证器缓存并污染后续授权。Config Validator 先完成角色引用和 Token 唯一性校验，构造器仍拒绝空字段与重复 Token value。

### 3.2 JWT 认证

JWT (JSON Web Token) 适合多用户、分布式或需要无状态验证的场景。

```yaml
# yaa.yaml
runtime:
  auth:
    enabled: true
    token_type: jwt
    jwt:
      secret: "${YAA_JWT_SECRET}"  # HMAC 密钥
      issuer: "yaa-runtime"         # 签发方
      audience: "yaa-client"        # 目标受众
      clock_skew: 30s                # exp/nbf 时钟容差
```

```go
import (
    "fmt"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

// JWTAuthenticator JWT 认证
type JWTAuthenticator struct {
    secret     []byte
    issuer     string
    audience   string
    clockSkew  time.Duration
}

func NewJWTAuthenticator(cfg config.JWTConfig) (*JWTAuthenticator, error) {
    if len(cfg.Secret) < 32 || cfg.Issuer == "" || cfg.Audience == "" ||
        cfg.ClockSkew < 0 || cfg.ClockSkew > 5*time.Minute {
        return nil, fmt.Errorf("invalid jwt configuration")
    }
    return &JWTAuthenticator{
        secret:    append([]byte(nil), cfg.Secret...),
        issuer:    cfg.Issuer,
        audience:  cfg.Audience,
        clockSkew: cfg.ClockSkew,
    }, nil
}

type jwtClaims struct {
    Name  string   `json:"name"`
    Roles []string `json:"roles"`
    jwt.RegisteredClaims
}

// Authenticate 解析并验证 JWT，返回 Identity
func (a *JWTAuthenticator) Authenticate(token string) (*Identity, error) {
    claims := new(jwtClaims)
    parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
        if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
            return nil, fmt.Errorf("%w: unexpected alg %q", ErrJWTInvalid, t.Method.Alg())
        }
        return a.secret, nil
    },
        jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
        jwt.WithIssuer(a.issuer),
        jwt.WithAudience(a.audience),
        jwt.WithLeeway(a.clockSkew),
        jwt.WithExpirationRequired(),
    )
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrJWTInvalid, err)
    }
    if !parsed.Valid || claims.Subject == "" || len(claims.Roles) == 0 {
        return nil, ErrJWTInvalid
    }
    return &Identity{
        ID:    claims.Subject,
        Name:  claims.Name,
        Roles: append([]string(nil), claims.Roles...),
        Claims: map[string]any{
            "issuer": claims.Issuer,
            "expires_at": claims.ExpiresAt.Time,
        },
    }, nil
}
```

MVP 只验证外部签发的 HS256 JWT，不提供登录、签发或刷新端点。角色固定读取 `roles` claim。`github.com/golang-jwt/jwt/v5` 是实现依赖；禁止自行解析 payload 或在验签前信任 `alg`。

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

## 4. HTTP 边界

Auth 包不实现 HTTP middleware、Bearer 解析、public path 或 REST envelope。Remote API 的唯一 route wrapper 调用 `Authenticator.Authenticate` 并把 Identity 放入 context，详见 [集成契约](integration.md)。缺失/无效静态凭据映射 `40101`；任何包装 `ErrJWTInvalid` 的 JWT 校验失败映射 `40102`。

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
                            │  Bearer Header          │
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

| 错误 | HTTP / code | 说明 |
|------|-------------|------|
| 缺少 Bearer、`ErrInvalidToken` | 401 / `40101` | 缺少或无效的静态 Token |
| `ErrJWTInvalid` | 401 / `40102` | JWT 签名、算法、issuer、audience、subject、roles、exp 或 nbf 无效 |
| Authorizer 返回 false/error | 403 / `40301` | 已认证但 RBAC 拒绝 |

所有 REST 握手错误使用统一 envelope：

```json
{"code":40101,"message":"unauthorized","data":null,"request_id":"req_01J..."}
```

---

## 7. 配置参考

```yaml
runtime:
  auth:
    enabled: true              # 关闭则所有请求无需认证
    token_type: static         # static | jwt
    public_paths:              # 免认证路径
      - "/api/v1/health"
      - "/api/v1/version"
    tokens:                    # 静态 Token 列表（token_type=static 时生效）
      - name: "default"
        token: "${YAA_AUTH_TOKEN}"
        roles: ["admin"]
    jwt:                       # JWT 配置（token_type=jwt 时生效）
      secret: "${YAA_JWT_SECRET}"
      issuer: "yaa-runtime"
      audience: "yaa-client"
      clock_skew: 30s
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
