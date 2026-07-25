package auth

import "errors"

// Auth 包的三个 sentinel（docs/auth/authentication.md §3）。
//
// Remote API 的稳定错误码映射使用这些 sentinel：40101 / 40102 / 40301。
// Authenticator 可以包装它们，但不得把底层解析错误写入响应。
var (
	// ErrInvalidToken 表示静态 Token 不匹配。
	ErrInvalidToken = errors.New("invalid static token")
	// ErrJWTInvalid 表示 JWT 校验（签名/算法/issuer/audience/subject/roles/exp/nbf）失败。
	ErrJWTInvalid = errors.New("invalid jwt")
	// ErrUnauthenticated 由 RBACAuthorizer.Authorize 在 identity==nil 时返回，
	// 为 AuthZ 层防御性错误码；常规路径 wrapper 已确保 AuthN 成功或 disabled/public bypass。
	ErrUnauthenticated = errors.New("identity is required")
)
