# Remote API 认证协议

> [返回索引](INDEX.md) · 配置定义见 [Auth 配置](../auth/config-ref.md)

## 1. Bearer 认证

当 `runtime.auth.enabled=true` 时，除 `runtime.auth.public_paths` 中精确匹配的路径外，HTTP、SSE 和 WebSocket 握手都必须提供：

```text
Authorization: Bearer <token>
```

Runtime 支持两种互斥模式：

- `token_type: static`：Token 值来自 `runtime.auth.tokens[]`，通过环境变量注入；每个 Token 绑定一个或多个 RBAC role。
- `token_type: jwt`：外部签发 HS256 JWT，Runtime 使用 `secret`、`issuer`、`audience` 和 `clock_skew` 校验 `exp`/`nbf`。Runtime 不签发、刷新或撤销 JWT。

`runtime.auth.enabled=false` 时 Remote route wrapper 同时跳过 AuthN/AuthZ，所有路由仍按业务层校验；非回环监听时配置校验必须拒绝该不安全组合。`public_paths` 只在认证启用时有意义。

认证成功后得到 `auth.Identity`，原始 Token 不进入 request context、日志或事件。

## 2. 失败响应

- 缺少 Header、scheme 不是 `Bearer` 或静态 Token 不匹配：HTTP 401 / `40101`。
- JWT 签名、算法、issuer、audience、subject、roles、`exp` 或 `nbf` 任一校验失败：HTTP 401 / `40102`。
- 凭据有效但 route metadata 授权失败：HTTP 403 / `40301`。

错误使用 [统一 REST envelope](INDEX.md#4-rest-envelope)。SSE/WS 在握手失败前返回 HTTP 错误；握手成功后的业务错误使用 conversation frame。

## 3. WebSocket 与浏览器

服务端只接受握手 Header 中的 Authorization；不接受把长期 Token 放在 URL query、SSE body 或普通消息 frame 中。不能设置 Header 的浏览器客户端应由同源后端代理，或使用能设置握手 Header 的客户端库。v1 不定义 query ticket、TicketManager 或 Token 兑换接口。

## 4. 授权绑定

Router 注册端点时显式写入 `(action, resource)`，Remote API 的唯一 route wrapper 使用已匹配的 metadata。资源归属（例如 `agents/:id/memory` 的 resource 是 `memory`）不得由字符串切分推导。详见 [授权机制](../auth/authorization.md)。

## 5. 明确没有的接口

v1 没有 `GET/POST/DELETE /api/v1/tokens`。动态 Token 的 scopes、expiry、revoke、黑名单和持久化均未定义，因此客户端必须通过配置或外部 JWT issuer 管理凭据。

---

*最后更新: 2026-07-22*
