# Auth / Token API

> [← 返回索引](./INDEX.md)

---

## GET /api/v1/tokens

列出所有 Token（需管理员权限）。Token 值不返回，仅返回元信息。

**响应 data:**

```json
{
  "items": [
    {
      "name": "admin",
      "scopes": ["*"],
      "created_at": "2025-07-15T10:00:00Z",
      "last_used_at": "2025-07-15T12:00:00Z",
      "expires_at": null,
      "active": true
    },
    {
      "name": "readonly",
      "scopes": ["read"],
      "created_at": "2025-07-15T10:00:00Z",
      "last_used_at": null,
      "expires_at": "2025-08-15T10:00:00Z",
      "active": true
    }
  ],
  "total": 2
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | Token 名称（标识） |
| `scopes` | []string | 权限范围：`*`（全部）/ `read`（只读）/ `write`（读写） |
| `last_used_at` | string\|null | 最后使用时间 |
| `expires_at` | string\|null | 过期时间，null 表示永不过期 |
| `active` | bool | 是否有效 |

---

## POST /api/v1/tokens

创建新 Token。Token 明文仅在创建时返回一次。

**请求 Body:**

```json
{
  "name": "mobile-app",
  "scopes": ["read", "write"],
  "expires_in": 86400
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | ✅ | Token 名称 |
| `scopes` | []string | ❌ | 权限范围，默认 `["*"]` |
| `expires_in` | int | ❌ | 有效期（秒），不填则永不过期 |

**响应 data:**

```json
{
  "name": "mobile-app",
  "token": "yaa_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "scopes": ["read", "write"],
  "expires_at": "2025-07-16T10:00:00Z",
  "created_at": "2025-07-15T10:00:00Z"
}
```

> ⚠️ `token` 字段仅在创建时返回，后续不可再获取，请妥善保存。

---

## DELETE /api/v1/tokens/:name

撤销 Token。撤销后立即失效。

**响应 data:**

```json
{ "revoked": true }
```
