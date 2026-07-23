# Auth 配置参考

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/auth/config-ref.md`
> 依赖: `docs/architecture.md` §3.12 Config, §3.14 Auth

---

## 1. 完整配置示例

```yaml
# yaa.yaml — auth 部分
runtime:
  auth:
    # 默认 false；仅监听回环地址时允许关闭
    enabled: true

    # Token 类型：static | jwt
    token_type: jwt

    # JWT 配置（token_type=jwt 时生效）
    jwt:
      secret: "${YAA_JWT_SECRET}"       # 环境变量引用
      issuer: "yaa-runtime"
      audience: "yaa-client"
      clock_skew: 30s                    # 校验时钟容差

    # 静态 Token 配置（token_type=static 时生效）
    tokens:
      - name: "admin"
        token: "${YAA_ADMIN_TOKEN}"
        roles: ["admin"]
      - name: "readonly"
        token: "${YAA_READONLY_TOKEN}"
        roles: ["viewer"]

    # RBAC 角色定义
    roles:
      - name: "admin"
        permissions:
          - action: "*"
            resource: "*"
            effect: allow
      - name: "operator"
        permissions:
          - action: read
            resource: "*"
          - action: write
            resource: agents
          - action: write
            resource: sessions
          - action: write
            resource: memory
          - action: delete
            resource: sessions
          - action: delete
            resource: memory
      - name: "viewer"
        permissions:
          - action: read
            resource: "*"

    # 公开端点（无需认证）
    public_paths:
      - "/api/v1/health"
      - "/api/v1/version"
```

## 2. 配置字段说明

下表是 [Config reference](../config/reference.md#23-runtimeauth) 的行为说明；字段、类型与默认值以该 canonical 表为准。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用认证；非回环监听时必须为 true |
| `token_type` | string | `"static"` | Token 类型 |
| `jwt.secret` | string | — | JWT 签名密钥 |
| `jwt.issuer` | string | `"yaa-runtime"` | JWT 签发者 |
| `jwt.audience` | string | `"yaa-client"` | JWT 受众 |
| `jwt.clock_skew` | duration | `30s` | `exp`/`nbf` 校验容差 |
| `tokens` | array | `[]` | 静态 Token 列表 |
| `tokens[].name` | string | — | Token 名称 |
| `tokens[].token` | string | — | Token 值 |
| `tokens[].roles` | array | `["viewer"]` | 绑定角色列表 |
| `roles` | array | `admin`/`operator`/`viewer` | 角色定义 |
| `roles[].name` | string | — | 角色名称 |
| `roles[].permissions` | array | — | 权限列表 |
| `public_paths` | array | `["/api/v1/health", "/api/v1/version"]` | 公开端点白名单 |

`public_paths` 的输入必须已经是 canonical 绝对路径：以 `/` 开头、`path.Clean(p)==p`，且不含 query 或 fragment。路由 wrapper 对 `r.URL.Path` 执行精确匹配，不先清理再接受，也不支持通配符或前缀匹配。

## 3. 启动校验

权威实现是 Config Validator 的 `validateListenAddr` 与 `validateAuthConfig`：

- `runtime.api.http.addr` 不是 loopback IP 或 `localhost` 时，`enabled=false` 拒绝启动；`0.0.0.0`、`::` 和空 host 都不是 loopback。
- `token_type` 只能是 `static|jwt`。Role name 必须非空且唯一；permission 的 action 只能是 `read|write|delete|*`，resource 必须是 Remote 路由总表中的资源或 `*`，effect 只能省略、`allow` 或 `deny`。
- `public_paths` 每项必须已经 canonical 且全局唯一。
- `static` 且认证启用时至少有一个 Token；Token name 和 Token value 分别唯一，value 非空，每个 Token 至少引用一个已定义 Role。
- `jwt` 且认证启用时使用 HS256，Secret 至少 32 bytes，issuer/audience 非空，clock skew 位于 `0..5m`。

Validator 聚合全部错误并报告完整配置路径；不得因认证关闭而跳过监听地址、枚举、Role 或 public path 的结构校验。

## 4. 权限规则格式

权限使用结构化的 `action`、`resource`、`effect` 字段；每条 HTTP 路由在注册时显式绑定资源和动作，再由 RBAC 匹配。`action` 和 `resource` 只支持完整值或整字段 `*`，路径通配不属于 Auth 配置。

| 规则 | 含义 |
|------|------|
| `action: "*", resource: "*"` | 全部权限（超级管理员） |
| `action: "read", resource: "agents"` | 读取 Agent 资源 |
| `action: "write", resource: "memory"` | 新增、提升或重建 Memory |
| `effect: deny` | 在所有 allow 之后优先拒绝 |

## 5. 最小配置（静态 Token）

```yaml
runtime:
  auth:
    enabled: true
    token_type: static
    tokens:
      - name: "default"
        token: "${YAA_AUTH_TOKEN}"
        roles: ["admin"]
    roles:
      - name: "admin"
        permissions:
          - action: "*"
            resource: "*"
            effect: allow
```

---

*最后更新: 2026-07-22*
