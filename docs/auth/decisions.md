# Auth 设计决策

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/decisions.md`
> 依赖: `docs/architecture.md` §3.14 Auth

---

## 设计决策记录

### AD-001: 认证与授权分离

- **决策**：将 `Authenticator` 和 `Authorizer` 拆分为独立接口
- **理由**：认证（你是谁）与授权（你能做什么）关注点不同，独立后可分别替换实现（如 LDAP 认证 + RBAC 授权）
- **影响**：接口数量增加，但灵活性和可测试性显著提升

### AD-002: 默认静态 Token，可选 JWT

- **决策**：`token_type` 默认 `static`，JWT 为可选升级
- **理由**：静态 Token 零依赖、配置即用，适合单机嵌入式场景；JWT 适合多实例、有用户登录需求的部署
- **影响**：两种实现共存，通过配置切换，`Authenticator` 接口屏蔽差异

### AD-003: RBAC 而非 ACL

- **决策**：采用基于角色的权限控制（RBAC），不直接为每个 Token 配置 ACL
- **理由**：角色抽象减少配置复杂度，Token 绑定角色，角色定义权限，易于批量管理
- **影响**：权限粒度由角色定义决定，不支持 Token 级别的细粒度覆盖（未来可扩展）

### AD-004: 中间件模式集成 Remote API

- **决策**：Auth 作为 HTTP 中间件运行，而非侵入 Handler
- **理由**：中间件模式与 HTTP 框架解耦，认证逻辑可独立测试，业务 Handler 无需感知认证细节
- **影响**：每个请求经过完整中间件链，有微小性能开销（可忽略）

### AD-005: 公开端点白名单而非黑名单

- **决策**：使用 `public_paths` 白名单声明无需认证的端点
- **理由**：白名单更安全，默认拒绝；新增端点如果忘记配置认证，不会被意外暴露
- **影响**：新增公开端点需显式添加到白名单

---

## 模块关系

```text
┌──────────────────────────────────────────────┐
│              Remote API Server               │
│                                              │
│   Request → Auth MW → RBAC MW → Handler     │
│                │            │                │
│                ▼            ▼                │
│          ┌──────────┐  ┌──────────┐          │
│          │Authentic-│  │Authorizer│          │
│          │  ator    │  │ (RBAC)   │          │
│          └────┬─────┘  └────┬─────┘          │
│               │             │                │
│               ▼             ▼                │
│          ┌──────────────────────────┐       │
│          │      Config (YAML)        │       │
│          │  auth.enabled             │       │
│          │  auth.token_type          │       │
│          │  auth.jwt / auth.tokens   │       │
│          │  auth.roles               │       │
│          │  auth.public_paths        │       │
│          └──────────────────────────┘       │
└──────────────────────────────────────────────┘
```

| 模块 | 职责 | 依赖 |
|------|------|------|
| Authenticator | 验证 Token，返回 Identity | Config（token 定义） |
| Authorizer | 检查 Identity 是否有权限执行操作 | Config（roles 定义） |
| Auth Middleware | 提取 Token，调用 Authenticator | Authenticator |
| RBAC Middleware | 调用 Authorizer 做权限检查 | Authorizer |
| Config | 提供 auth 配置 | YAML / 环境变量 |

---

*最后更新: 2025-07-17*
