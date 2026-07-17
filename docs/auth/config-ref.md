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
    # 是否启用认证（默认 false，开发模式可关闭）
    enabled: true

    # Token 类型：static | jwt
    token_type: jwt

    # JWT 配置（token_type=jwt 时生效）
    jwt:
      secret: "${YAA_JWT_SECRET}"       # 环境变量引用
      issuer: "yaa-runtime"
      expiry: 24h                        # Token 有效期
      refresh: true                      # 是否支持刷新
      refresh_expiry: 168h               # 刷新 Token 有效期

    # 静态 Token 配置（token_type=static 时生效）
    tokens:
      - name: "admin"
        token: "yaat-admin-xxxxx"
        role: "admin"
      - name: "readonly"
        token: "yaat-readonly-xxxxx"
        role: "viewer"

    # RBAC 角色定义
    roles:
      - name: "admin"
        permissions:
          - "*:*"                        # 全部权限
      - name: "operator"
        permissions:
          - "GET:/api/v1/agents"
          - "POST:/api/v1/agents"
          - "PUT:/api/v1/agents/*"
          - "DELETE:/api/v1/sessions/*"
          - "POST:/api/v1/sessions/*/messages"
      - name: "viewer"
        permissions:
          - "GET:/api/v1/agents"
          - "GET:/api/v1/agents/*"
          - "GET:/api/v1/sessions/*"

    # 公开端点（无需认证）
    public_paths:
      - "/api/v1/health"
      - "/api/v1/version"
      - "/api/v1/auth/login"
```

## 2. 配置字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用认证 |
| `token_type` | string | `"static"` | Token 类型 |
| `jwt.secret` | string | — | JWT 签名密钥 |
| `jwt.issuer` | string | `"yaa-runtime"` | JWT 签发者 |
| `jwt.expiry` | duration | `24h` | Token 有效期 |
| `jwt.refresh` | bool | `false` | 是否启用刷新 |
| `jwt.refresh_expiry` | duration | `168h` | 刷新 Token 有效期 |
| `tokens` | array | — | 静态 Token 列表 |
| `tokens[].name` | string | — | Token 名称 |
| `tokens[].token` | string | — | Token 值 |
| `tokens[].role` | string | `"viewer"` | 绑定角色 |
| `roles` | array | — | 角色定义 |
| `roles[].name` | string | — | 角色名称 |
| `roles[].permissions` | array | — | 权限列表 |
| `public_paths` | array | — | 公开端点白名单 |

## 3. 权限规则格式

权限字符串格式为 `HTTP_METHOD:PATH_PATTERN`，支持通配符：

| 规则 | 含义 |
|------|------|
| `*:*` | 全部权限（超级管理员） |
| `GET:/api/v1/agents` | 精确匹配 |
| `PUT:/api/v1/agents/*` | 路径通配 |
| `POST:/api/v1/sessions/*/messages` | 中间段通配 |

## 4. 最小配置（静态 Token）

```yaml
runtime:
  auth:
    enabled: true
    token_type: static
    tokens:
      - name: "default"
        token: "yaat-my-secret-token"
        role: "admin"
    roles:
      - name: "admin"
        permissions: ["*:*"]
```

---

*最后更新: 2025-07-17*
