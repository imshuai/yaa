# Auth 实现检查清单

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/checklist.md`
> 依赖: `docs/auth/` 全系列

---

## 核心接口实现

- [ ] 定义 `Authenticator` 接口（`Authenticate(token) → Identity`）
- [ ] 定义 `Authorizer` 接口（`Authorize(identity, action, resource) → bool`）
- [ ] 定义 `Identity` 结构体（ID、Name、Role、Metadata）

## Token 认证

- [ ] 实现 `StaticTokenAuthenticator`（静态 Token 比对）
- [ ] 实现 `JWTAuthenticator`（签发 + 验证 + 刷新）
- [ ] Token 提取支持 Header / Query / Cookie 三种方式
- [ ] Token 不存在时返回 `401 Unauthorized`
- [ ] Token 无效/过期时返回 `401 Unauthorized` 并附带错误信息

## RBAC 授权

- [ ] 实现基于角色的权限匹配引擎
- [ ] 支持通配符权限规则（`*:*`、`GET:/api/v1/agents/*`）
- [ ] 权限拒绝时返回 `403 Forbidden`
- [ ] `auth.enabled=false` 时跳过所有认证与授权

## 中间件集成

- [ ] 实现 `authMiddleware`（认证层）
- [ ] 实现 `rbacMiddleware`（授权层）
- [ ] 公开端点白名单（`public_paths`）跳过认证
- [ ] WebSocket 握手阶段认证（Query 参数传 Token）
- [ ] Identity 注入 `context.Context`，Handler 可读取

## 配置

- [ ] 解析 `auth.enabled` 字段
- [ ] 解析 `auth.token_type` 并选择对应 Authenticator
- [ ] 解析 `auth.jwt.*` 配置（secret、issuer、expiry）
- [ ] 解析 `auth.tokens` 静态 Token 列表
- [ ] 解析 `auth.roles` 角色与权限定义
- [ ] 环境变量引用支持（`${VAR_NAME}`）

## 测试

- [ ] 单元测试：静态 Token 认证通过/失败
- [ ] 单元测试：JWT 签发、验证、过期、刷新
- [ ] 单元测试：RBAC 权限匹配（精确、通配符、拒绝）
- [ ] 集成测试：中间件链完整流程
- [ ] 集成测试：公开端点无需认证
- [ ] 集成测试：WebSocket 认证流程

---

*最后更新: 2025-07-17*
