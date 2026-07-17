# 授权机制 (Authorization)

> 本文件描述 Yaa! Runtime 的授权系统设计，包括 RBAC 模型、角色与权限定义、
> 策略引擎、端点白名单以及 Go 代码示例。
>
> 认证 (Authentication) 负责确认"你是谁"，授权 (Authorization) 负责确认"你能做什么"。
> 本文档聚焦授权部分，认证详见 [authentication.md](./authentication.md)。

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| **RBAC 核心** | 基于角色的访问控制，权限绑定角色而非个体 |
| **声明式策略** | 权限策略通过配置文件声明，无需修改代码 |
| **端点白名单** | 部分端点可免认证（如 `/health`），其余默认需要授权 |
| **细粒度控制** | 支持动作 × 资源二维权限模型 |
| **可扩展** | 支持自定义 Authorizer 实现（如 ABAC、ACL） |
| **零运行时依赖** | 纯 Go 标准库实现，不引入外部鉴权服务 |

---

## 2. RBAC 模型

Yaa! 采用经典 RBAC（Role-Based Access Control）模型：

```text
┌──────────┐      ┌──────────┐      ┌──────────┐      ┌──────────┐
│ Identity │─────▶│  Roles   │─────▶│   perms  │─────▶│ Resources│
│ (用户/Token)│      │ (角色)    │      │ (权限)    │      │ (资源)    │
└──────────┘      └──────────┘      └──────────┘      └──────────┘

  一个 Identity 可拥有多个 Role
  一个 Role 可包含多个 Permission
  一个 Permission = Action + Resource
```

### 2.1 核心概念

| 概念 | 说明 | 示例 |
|------|------|------|
| **Identity** | 通过认证后的身份标识 | `token:default`, `user:admin` |
| **Role** | 角色定义，是权限的集合 | `admin`, `operator`, `viewer` |
| **Permission** | 权限 = 动作 × 资源 | `read:agents`, `write:sessions` |
| **Resource** | 被操作的对象类型 | `agents`, `sessions`, `tools`, `config` |
| **Action** | 允许的操作类型 | `read`, `write`, `delete`, `execute` |

---

## 3. 角色与权限定义

### 3.1 内置角色

| 角色 | 说明 | 权限范围 |
|------|------|----------|
| `admin` | 超级管理员 | 所有资源的所有动作 |
| `operator` | 运维操作员 | Agent/Session/Tool 的读写执行，不含系统配置 |
| `viewer` | 只读用户 | 所有资源的只读访问 |
| `agent` | Agent 身份 | 仅限自身 Session 的读写、Tool 执行 |

### 3.2 权限矩阵

| 资源 \ 动作 | `read` | `write` | `delete` | `execute` |
|-------------|--------|---------|----------|-----------|
| `agents` | ✅ operator | ✅ admin | ✅ admin | — |
| `sessions` | ✅ viewer | ✅ operator | ✅ operator | — |
| `tools` | ✅ viewer | — | — | ✅ operator |
| `skills` | ✅ viewer | — | — | ✅ operator |
| `providers` | ✅ viewer | ✅ admin | ✅ admin | — |
| `memory` | ✅ operator | ✅ operator | ✅ operator | — |
| `mcp` | ✅ operator | ✅ admin | ✅ admin | — |
| `config` | ✅ admin | ✅ admin | — | — |
| `system` | ✅ viewer | — | — | — |

### 3.3 配置声明

角色与权限通过配置文件声明：

```yaml
# yaa.yaml - auth 配置段
auth:
  enabled: true
  rbac:
    roles:
      - name: "admin"
        permissions:
          - action: "*"
            resource: "*"
      
      - name: "operator"
        permissions:
          - action: "read"
            resource: "*"
          - action: "write"
            resource: "agents"
          - action: "write"
            resource: "sessions"
          - action: "execute"
            resource: "tools"
          - action: "execute"
            resource: "skills"
          - action: "read"
            resource: "memory"
          - action: "write"
            resource: "memory"
      
      - name: "viewer"
        permissions:
          - action: "read"
            resource: "*"
    
    # Token 到角色的映射
    token_roles:
      "yaat-admin-xxxxx": ["admin"]
      "yaat-op-yyyyy":    ["operator"]
      "yaat-view-zzzzz":  ["viewer"]
  
  # 端点白名单（无需认证/授权）
  whitelist:
    - "GET /api/v1/health"
    - "GET /api/v1/version"
```

---

## 4. 权限策略引擎

### 4.1 策略匹配流程

```text
请求到达
  │
  ▼
┌─────────────────────┐
│ 1. 端点白名单检查     │─── 命中 ──▶ 放行（跳过认证与授权）
└─────────┬───────────┘
          │ 未命中
          ▼
┌─────────────────────┐
│ 2. 认证 (AuthN)      │─── 失败 ──▶ 401 Unauthorized
└─────────┬───────────┘
          │ 成功，获得 Identity
          ▼
┌─────────────────────┐
│ 3. 授权 (AuthZ)      │─── 拒绝 ──▶ 403 Forbidden
│   解析 Action+Resource│
│   查询角色权限         │
│   通配符匹配           │
└─────────┬───────────┘
          │ 允许
          ▼
      请求继续处理
```

### 4.2 通配符规则

| 模式 | 含义 | 示例 |
|------|------|------|
| `*` (action) | 匹配所有动作 | `action: "*"` → read/write/delete/execute |
| `*` (resource) | 匹配所有资源 | `resource: "*"` → agents/sessions/tools/... |
| 精确值 | 精确匹配 | `action: "read", resource: "agents"` |

### 4.3 决策优先级

1. **显式拒绝**：如果任何角色权限中存在显式 `deny`，优先拒绝
2. **显式允许**：匹配到 `allow` 权限则放行
3. **默认拒绝**：未匹配到任何权限则拒绝

---

## 5. 端点白名单

白名单中的端点跳过认证与授权，适用于健康检查、版本信息等公开端点。

```yaml
auth:
  whitelist:
    - "GET /api/v1/health"
    - "GET /api/v1/version"
```

**白名单匹配规则：**
- 格式为 `METHOD PATH`，支持路径前缀通配符 `*`
- 示例：`GET /api/v1/health` 精确匹配
- 示例：`GET /api/v1/public/*` 前缀匹配

---

## 6. Go 代码示例

### 6.1 核心类型定义

```go
package auth

// Permission 权限定义
type Permission struct {
    Action   string `yaml:"action"`   // read / write / delete / execute / *
    Resource string `yaml:"resource"` // agents / sessions / tools / *
    Effect   string `yaml:"effect"`   // allow / deny，默认 allow
}

// Role 角色定义
type Role struct {
    Name        string       `yaml:"name"`
    Permissions []Permission `yaml:"permissions"`
}

// Identity 认证后的身份
type Identity struct {
    ID    string   // 身份标识
    Name  string   // 可读名称
    Roles []string // 角色列表
}

// RBACAuthorizer 基于角色的授权器
type RBACAuthorizer struct {
    roles      map[string]*Role       // 角色注册表
    tokenRoles map[string][]string    // Token → 角色映射
    whitelist  map[string]bool        // 端点白名单
}

// Authorizer 授权接口
type Authorizer interface {
    Authorize(identity *Identity, action string, resource string) (bool, error)
    IsWhitelisted(method, path string) bool
}
```

### 6.2 授权逻辑实现

```go
// Authorize 检查 Identity 是否有权对 resource 执行 action
func (a *RBACAuthorizer) Authorize(identity *Identity, action, resource string) (bool, error) {
    if identity == nil {
        return false, ErrUnauthenticated
    }

    var denied bool

    // 遍历 Identity 拥有的所有角色
    for _, roleName := range identity.Roles {
        role, ok := a.roles[roleName]
        if !ok {
            continue
        }

        // 检查角色下的每条权限
        for _, perm := range role.Permissions {
            if !matchPattern(perm.Action, action) {
                continue
            }
            if !matchPattern(perm.Resource, resource) {
                continue
            }

            switch perm.Effect {
            case "deny", "Deny":
                denied = true // 显式拒绝，记录但继续检查
            case "allow", "Allow", "":
                return true, nil // 显式允许，立即通过
            }
        }
    }

    // 显式拒绝优先于默认拒绝
    if denied {
        return false, nil
    }

    // 默认拒绝
    return false, nil
}

// matchPattern 通配符匹配
func matchPattern(pattern, target string) bool {
    if pattern == "*" {
        return true
    }
    return pattern == target
}

// IsWhitelisted 检查端点是否在白名单中
func (a *RBACAuthorizer) IsWhitelisted(method, path string) bool {
    key := method + " " + path
    if a.whitelist[key] {
        return true
    }
    // 前缀通配匹配：如 "GET /api/v1/public/*"
    for w := range a.whitelist {
        if strings.HasSuffix(w, "/*") {
            prefix := strings.TrimSuffix(w, "/*")
            if strings.HasPrefix(key, prefix+"/") || key == prefix {
                return true
            }
        }
    }
    return false
}
```

### 6.3 中间件集成

```go
// AuthMiddleware 将认证与授权集成到 HTTP 中间件链
func AuthMiddleware(authn Authenticator, authz Authorizer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. 白名单检查
            if authz.IsWhitelisted(r.Method, r.URL.Path) {
                next.ServeHTTP(w, r)
                return
            }

            // 2. 提取 Token
            token := extractToken(r)
            if token == "" {
                writeError(w, http.StatusUnauthorized, "missing token")
                return
            }

            // 3. 认证
            identity, err := authn.Authenticate(token)
            if err != nil {
                writeError(w, http.StatusUnauthorized, "invalid token")
                return
            }

            // 4. 授权（将路由信息映射为 action + resource）
            action, resource := routeToPermission(r.Method, r.URL.Path)
            allowed, err := authz.Authorize(identity, action, resource)
            if err != nil || !allowed {
                writeError(w, http.StatusForbidden, "access denied")
                return
            }

            // 5. 注入 Identity 到上下文，继续处理
            ctx := context.WithValue(r.Context(), identityKey{}, identity)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// routeToPermission 将 HTTP 方法+路径映射为 RBAC action+resource
func routeToPermission(method, path string) (action, resource string) {
    // POST   /api/v1/agents         → write, agents
    // GET    /api/v1/agents         → read, agents
    // DELETE /api/v1/agents/:id     → delete, agents
    // POST   /api/v1/tools/:name/execute → execute, tools
    // ...
    segments := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
    if len(segments) == 0 {
        return "", ""
    }
    resource = segments[0] // agents / sessions / tools / ...
    switch method {
    case http.MethodGet:
        action = "read"
    case http.MethodPost:
        if len(segments) > 1 && segments[len(segments)-1] == "execute" {
            action = "execute"
        } else {
            action = "write"
        }
    case http.MethodPut:
        action = "write"
    case http.MethodDelete:
        action = "delete"
    }
    return action, resource
}
```

---

## 7. 扩展点

| 扩展点 | 接口 | 说明 |
|--------|------|------|
| 自定义 Authorizer | `Authorizer` | 实现 ABAC、ACL 等自定义授权模型 |
| 权限策略加载 | `PolicyLoader` | 从数据库、外部服务动态加载策略 |
| 审计日志 | `AuditLogger` | 记录每次授权决策（允许/拒绝） |
| 运行时角色变更 | `RoleManager` | 通过 API 动态增删角色与权限 |

```go
// PolicyLoader 策略加载器接口
type PolicyLoader interface {
    Load() (*RBACConfig, error)
    Watch() <-chan *RBACConfig // 策略变更通知
}

// AuditLogger 审计日志接口
type AuditLogger interface {
    LogAccess(identity *Identity, action, resource string, allowed bool)
}
```

---

## 8. 安全注意事项

1. **默认拒绝**：未显式配置的权限一律拒绝
2. **最小权限原则**：角色只授予必要的最小权限集
3. **Token 安全**：Token 在传输中使用 TLS 加密，存储时哈希处理
4. **审计追踪**：生产环境建议启用 AuditLogger 记录所有授权决策
5. **通配符谨慎使用**：`*` 通配符仅在可信角色（如 admin）中使用

---

*最后更新: 2025-07-17*
