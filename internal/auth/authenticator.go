package auth

// Authenticator 是认证接口：把原始凭据转成 Identity（docs/auth/authentication.md §2/§3）。
//
// 实现需保证 Authenticate 返回的 Identity 与 Authenticator 内部缓存相互独立
// （通过 cloneIdentity）。
type Authenticator interface {
	Authenticate(token string) (*Identity, error)
}

// Authorizer 是授权接口：检查 Identity 是否有权对 resource 执行 action
// （docs/auth/authorization.md §6.1）。
//
// identity==nil 时实现返回 ErrUnauthenticated；其他失败由实现按 sentinel 包装。
type Authorizer interface {
	Authorize(identity *Identity, action, resource string) (bool, error)
}
