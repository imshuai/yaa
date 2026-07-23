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
| **端点白名单** | 部分端点可免认证（如 `/api/v1/health`），其余默认需要授权 |
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
| **Action** | 允许的操作类型 | `read`, `write`, `delete` |

---

## 3. 角色与权限定义

### 3.1 内置角色

| 角色 | 说明 | 权限范围 |
|------|------|----------|
| `admin` | 超级管理员 | 所有资源的所有动作 |
| `operator` | 运维操作员 | 读取全部资源；写 Agent、Session、Memory；删除 Session、Memory |
| `viewer` | 只读用户 | 所有资源的只读访问 |

### 3.2 权限矩阵

| 资源 \ 动作 | `read` | `write` | `delete` |
|-------------|--------|---------|----------|
| `agents` | ✅ viewer | ✅ operator | ✅ admin |
| `sessions` | ✅ viewer | ✅ operator | ✅ operator |
| `tools` | ✅ viewer | — | — |
| `skills` | ✅ viewer | — | — |
| `providers` | ✅ viewer | — | — |
| `memory` | ✅ viewer | ✅ operator | ✅ operator |
| `mcp` | ✅ viewer | — | — |
| `config` | ✅ viewer | — | — |
| `system` | ✅ viewer | — | — |

表中角色表示最低内置角色；`admin` 的 `*:*` 同样允许所有格。Tool/Skill 没有直接执行路由，它们只能在具备 `write:sessions` 的 Agent turn 内按 Agent 白名单执行。

### 3.3 配置声明

角色与权限通过配置文件声明：

```yaml
# yaa.yaml - auth 配置段
runtime:
  auth:
    enabled: true
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
          - action: "write"
            resource: "memory"
          - action: "delete"
            resource: "sessions"
          - action: "delete"
            resource: "memory"

      - name: "viewer"
        permissions:
          - action: "read"
            resource: "*"

    # Token 与角色绑定见 runtime.auth.tokens[].roles
    public_paths:
      - "/api/v1/health"
      - "/api/v1/version"
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
| `*` (action) | 匹配所有动作 | `action: "*"` → read/write/delete |
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
runtime:
  auth:
    public_paths:
      - "/api/v1/health"
      - "/api/v1/version"
```

**白名单匹配规则：** 使用规范化后的 URL 路径精确匹配；不接受通配符，也不按前缀豁免，避免意外公开子路径。

---

## 6. Go 代码示例

### 6.1 核心类型定义

```go
package auth

var ErrUnauthenticated = errors.New("identity is required")

// Permission 权限定义
type Permission struct {
    Action   string `yaml:"action"`   // read / write / delete / *
    Resource string `yaml:"resource"` // agents / sessions / tools / *
    Effect   string `yaml:"effect"`   // allow / deny，默认 allow
}

// Role 角色定义
type Role struct {
    Name        string       `yaml:"name"`
    Permissions []Permission `yaml:"permissions"`
}

// RBACAuthorizer 基于角色的授权器
type RBACAuthorizer struct {
    roles map[string]*Role // 角色注册表
}

// Authorizer 授权接口
type Authorizer interface {
    Authorize(identity *Identity, action string, resource string) (bool, error)
}

func NewRBACAuthorizer(cfg []config.RoleConfig) (*RBACAuthorizer, error) {
    a := &RBACAuthorizer{roles: make(map[string]*Role, len(cfg))}
    for _, in := range cfg {
        if in.Name == "" || a.roles[in.Name] != nil {
            return nil, fmt.Errorf("invalid or duplicate role %q", in.Name)
        }
        role := &Role{Name: in.Name, Permissions: make([]Permission, len(in.Permissions))}
        for i, p := range in.Permissions {
            role.Permissions[i] = Permission{
                Action: p.Action, Resource: p.Resource, Effect: p.Effect,
            }
        }
        a.roles[in.Name] = role
    }
    return a, nil
}
```

构造器接收已经通过 Config Validator 的 canonical 角色配置并深拷贝全部 Permission；运行期配置变化需要重启，不替换 `roles` map。

### 6.2 授权逻辑实现

```go
// Authorize 检查 Identity 是否有权对 resource 执行 action
func (a *RBACAuthorizer) Authorize(identity *Identity, action, resource string) (bool, error) {
    if identity == nil {
        return false, ErrUnauthenticated
    }

    var allowed, denied bool

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
            case "deny":
                denied = true
            case "allow", "":
                allowed = true
            default:
                return false, fmt.Errorf("invalid permission effect %q", perm.Effect)
            }
        }
    }

    // 扫描完所有角色后再决策，保证任意 deny 优先于 allow。
    if denied {
        return false, nil
    }
    return allowed, nil
}

// matchPattern 通配符匹配
func matchPattern(pattern, target string) bool {
    if pattern == "*" {
        return true
    }
    return pattern == target
}

```

### 6.3 中间件集成

RBACAuthorizer 不持有 Token 到角色映射、public path 或 HTTP route。Identity 的 Roles 由 Authenticator 产生；Remote API Server 以唯一 `RouteSpec` 调用 `Authorize`。嵌套路由例如 `POST /agents/:id/memory` 必须绑定 `write:memory`，不得根据第一段 `agents` 猜测。完整 wrapper 见 [integration.md](integration.md#3-唯一路由-wrapper)。

---

## 7. 扩展点

| 扩展点 | 接口 | 说明 |
|--------|------|------|
| 自定义 Authorizer | `Authorizer` | 实现 ABAC、ACL 等自定义授权模型 |
| 审计日志 | `AuditLogger` | 记录每次授权决策（允许/拒绝），不记录凭据原文 |

```go
// AuditLogger 审计日志接口
type AuditLogger interface {
    LogAccess(identity *Identity, action, resource string, allowed bool)
}
```

v1 的角色只由启动 Config 提供；`runtime.auth.*` 变更需要重启，不提供动态角色管理 API。

---

## 8. 安全注意事项

1. **默认拒绝**：未显式配置的权限一律拒绝
2. **最小权限原则**：角色只授予必要的最小权限集
3. **Token 安全**：非回环监听必须放在 TLS 终止的反向代理后；Token 在 Runtime 内只使用哈希索引，不记录原文
4. **审计追踪**：生产环境建议启用 AuditLogger 记录所有授权决策
5. **通配符谨慎使用**：`*` 通配符仅在可信角色（如 admin）中使用

---

*最后更新: 2025-07-17*
