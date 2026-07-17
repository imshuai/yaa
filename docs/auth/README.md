# Auth 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/` (原计划单文件 `docs/auth.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.14, Remote API 全系列

---

## 1. 概述

### 1.1 为什么需要 Auth

Yaa! 通过 Remote API 对外暴露所有能力（HTTP / WebSocket / SSE）。
Auth 系统负责在请求进入 Runtime 之前完成 **身份认证** 与 **权限授权**，
确保只有合法客户端才能访问对应资源。

| 层级 | 职责 | 类比 |
|------|------|------|
| **Authenticator** | 识别"你是谁" | 门禁刷卡 |
| **Authorizer** | 判断"你能做什么" | 权限矩阵 |

### 1.2 设计理念

Yaa! 的 Auth 系统遵循以下原则：

| 特性 | 说明 |
|------|------|
| Provider Independent | 认证逻辑与 LLM Provider 无关 |
| Config over Code | 通过配置文件定义 Token、角色、策略，无需改代码 |
| Pluggable | 认证/授权均为 interface，可替换为自定义实现 |
| Minimal by Default | 默认提供静态 Token，开箱即用 |
| Zero CGO | 纯 Go 实现，Windows 7 兼容 |

### 1.3 核心原则

1. **Auth Before Route** — 认证在路由之前执行，未认证请求直接拒绝
2. **Deny by Default** — 未显式允许的操作默认拒绝
3. **Stateless First** — 优先无状态认证（静态 Token / JWT），减少存储依赖
4. **Public Endpoints** — 可配置豁免认证的端点（如 `/health`）
5. **Fail Fast** — 认证/授权失败立即返回 401/403，不进入业务逻辑

---

## 2. 核心接口

### 2.1 Authenticator — 身份认证

```go
// Authenticator 验证请求中的 Token，返回身份信息。
type Authenticator interface {
    // Authenticate 解析 Token 并返回对应的 Identity。
    // 如果 Token 无效或过期，返回 error。
    Authenticate(token string) (*Identity, error)
}

// Identity 表示经过认证的身份。
type Identity struct {
    ID     string         // 身份唯一标识
    Name   string         // 可读名称
    Roles  []string       // 角色列表（用于 RBAC）
    Claims map[string]any // 扩展声明（JWT Payload 等）
}
```

### 2.2 Authorizer — 权限授权

```go
// Authorizer 判断身份是否有权执行某操作。
type Authorizer interface {
    // Authorize 检查 identity 是否可以对 resource 执行 action。
    // 返回 true 表示允许，false 表示拒绝。
    Authorize(identity *Identity, action string, resource string) (bool, error)
}
```

### 2.3 中间件集成

```go
// AuthMiddleware 是 HTTP 中间件，串联 Authenticator + Authorizer。
func AuthMiddleware(auth Authenticator, authz Authorizer, public []string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. 检查是否为公开端点
            for _, p := range public {
                if strings.HasPrefix(r.URL.Path, p) {
                    next.ServeHTTP(w, r)
                    return
                }
            }
            // 2. 提取 Token
            token := extractToken(r) // Authorization: Bearer <token>
            if token == "" {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            // 3. 认证
            identity, err := auth.Authenticate(token)
            if err != nil {
                http.Error(w, "unauthorized", http.StatusUnauthorized)
                return
            }
            // 4. 授权
            action := methodToAction(r.Method) // GET → read, POST → write...
            allowed, err := authz.Authorize(identity, action, r.URL.Path)
            if err != nil || !allowed {
                http.Error(w, "forbidden", http.StatusForbidden)
                return
            }
            // 5. 注入身份并放行
            ctx := context.WithValue(r.Context(), identityKey{}, identity)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

---

## 3. 认证方式

### 3.1 方式对比

| 方式 | 适用场景 | 状态 | 复杂度 |
|------|----------|------|--------|
| 静态 Token | 单用户 / 内网部署 | 无状态 | ⭐ |
| JWT | 多用户 / 跨服务 | 无状态 | ⭐⭐ |
| OAuth 2.0 | 第三方接入 | 有状态 | ⭐⭐⭐（未来） |

### 3.2 静态 Token

```yaml
# yaa.yaml
auth:
  enabled: true
  type: static_token
  tokens:
    - name: "admin"
      token: "yaat-a1b2c3d4e5f6"
      roles: ["admin"]
    - name: "readonly"
      token: "yaat-readonly-xxxx"
      roles: ["viewer"]
  public_endpoints:
    - "/api/v1/health"
    - "/api/v1/version"
```

### 3.3 JWT

```yaml
auth:
  enabled: true
  type: jwt
  jwt:
    secret: "${YAA_JWT_SECRET}"
    issuer: "yaa"
    # 可选：从 JWKS 端点验证
    # jwks_url: "https://example.com/.well-known/jwks.json"
  roles_claim: "roles"      # JWT Payload 中角色字段名
  public_endpoints:
    - "/api/v1/health"
```

---

## 4. RBAC 权限模型

### 4.1 角色与权限

| 角色 | action 范围 | 说明 |
|------|------------|------|
| `admin` | read / write / delete | 全部权限 |
| `operator` | read / write | 可操作，不可删除 |
| `viewer` | read | 只读 |

### 4.2 权限矩阵示例

| 资源 | admin | operator | viewer |
|------|-------|----------|--------|
| `/api/v1/agents` (GET) | ✅ | ✅ | ✅ |
| `/api/v1/agents` (POST) | ✅ | ✅ | ❌ |
| `/api/v1/agents/:id` (DELETE) | ✅ | ❌ | ❌ |
| `/api/v1/sessions/:id/messages` (POST) | ✅ | ✅ | ❌ |

### 4.3 配置示例

```yaml
auth:
  rbac:
    enabled: true
    roles:
      admin:
        permissions:
          - action: "*"
            resource: "*"
      operator:
        permissions:
          - action: "read"
            resource: "*"
          - action: "write"
            resource: "*"
      viewer:
        permissions:
          - action: "read"
            resource: "*"
```

---

## 5. 公开端点

某些端点不需要认证，可在配置中声明：

```yaml
auth:
  public_endpoints:
    - "/api/v1/health"      # 健康检查
    - "/api/v1/version"     # 版本信息
```

匹配规则：**前缀匹配**。`/api/v1/health` 会豁免该路径及其子路径。

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [authentication.md](authentication.md) | 认证机制 — 静态 Token / JWT 实现、Identity 结构 |
| [authorization.md](authorization.md) | 授权机制（RBAC）— 角色与权限矩阵、策略引擎 |
| [integration.md](integration.md) | 与 Remote API 中间件集成 — HTTP/WS/SSE 认证流程、公开端点匹配 |
| [config-ref.md](config-ref.md) | 配置参考 — 全部 Auth 配置项、默认值、示例 |
| [decisions.md](decisions.md) | 设计决策（AD-001 ~ AD-NNN）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |
