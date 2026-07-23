# Auth 实现检查清单

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/checklist.md`
> 依赖: `docs/auth/` 全系列

---

## 核心接口实现

- [ ] 定义 `Authenticator` 接口（`Authenticate(token) → Identity`）
- [ ] 定义 `Authorizer` 接口（`Authorize(identity, action, resource) → bool`）
- [ ] 定义 `Identity` 结构体（ID、Name、Roles、Claims；不保存原始 Token）

## Token 认证

- [ ] 实现 `StaticTokenAuthenticator`（静态 Token 比对）
- [ ] 实现 `JWTAuthenticator`（仅验证外部签发的 JWT；固定允许算法并校验签名、iss、aud、exp、nbf、sub）
- [ ] HTTP/SSE/WS 仅从 `Authorization: Bearer` 提取 Token；不接受 query 或 Cookie 凭据
- [ ] Token 不存在时返回 `401 Unauthorized`
- [ ] Token 无效/过期时返回统一 envelope，不回显认证失败细节

## RBAC 授权

- [ ] 实现基于角色的权限匹配引擎
- [ ] 支持结构化权限规则（`action`/`resource`/`effect`，字段级 `*`）
- [ ] 权限拒绝时返回 `403 Forbidden`
- [ ] `auth.enabled=false` 时跳过所有认证与授权
- [ ] `auth.enabled=true` 且请求不在精确 `public_paths` 时，缺少 Identity 必须拒绝（deny-by-default）

## Remote route wrapper 集成

- [ ] Remote API 实现唯一 route wrapper，并按 disabled/public → AuthN → AuthZ → handler 的顺序执行
- [ ] 每条受保护路由注册时显式绑定 `RouteSpec.Action/Resource`，并与 Remote API 总表逐项测试
- [ ] 公开端点白名单（`public_paths`）同时跳过认证和授权
- [ ] WebSocket handler 使用 route wrapper 注入的 Identity；浏览器适配通过同源后端代理
- [ ] Identity 注入 `context.Context`，Handler 可读取

## 配置

- [ ] 解析 `auth.enabled` 字段
- [ ] 解析 `auth.token_type` 并选择对应 Authenticator
- [ ] 解析 `auth.jwt.*` 配置（secret、issuer、audience、clock_skew）
- [ ] 解析 `auth.tokens` 静态 Token 列表
- [ ] 解析 `auth.roles` 角色与权限定义
- [ ] 环境变量引用支持（`${VAR_NAME}`）

## 测试

- [ ] 单元测试：静态 Token 认证通过/失败
- [ ] 单元测试：JWT 签名/算法、issuer/audience、过期、nbf、sub、角色 claim
- [ ] 单元测试：RBAC 权限匹配（精确、通配符、拒绝）
- [ ] 集成测试：唯一 route wrapper 的完整流程
- [ ] 集成测试：公开端点无需认证
- [ ] 集成测试：WebSocket 认证流程

---

*最后更新: 2025-07-17*
