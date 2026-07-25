package auth

import (
	"context"
	"fmt"
)

// Identity 表示一个已认证的身份（详见 docs/auth/authentication.md §2）。
//
// v1 Claims 值只允许不可变标量或 time.Time；cloneIdentity 做浅 map 复制即可隔离。
type Identity struct {
	ID     string
	Name   string
	Roles  []string
	Claims map[string]any
}

// cloneIdentity 深拷贝 Roles/Claims；构造与返回 Identity 时一律走此 helper，
// 避免调用方修改 Authenticator/Context 缓存的内容而导致其他请求被污染。
func cloneIdentity(src *Identity) *Identity {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Roles = append([]string(nil), src.Roles...)
	if src.Claims != nil {
		dst.Claims = make(map[string]any, len(src.Claims))
		for k, v := range src.Claims {
			dst.Claims[k] = v
		}
	}
	return &dst
}

// String 用于日志输出（脱敏：不含 Claims/Token）。
func (i *Identity) String() string {
	if i == nil {
		return "Identity{<nil>}"
	}
	return fmt.Sprintf("Identity{id=%s, name=%s, roles=%v}", i.ID, i.Name, i.Roles)
}

// HasRole 检查是否拥有指定角色。
func (i *Identity) HasRole(role string) bool {
	if i == nil {
		return false
	}
	for _, r := range i.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// identityContextKey 是 Auth 包拥有的唯一 context key；
// 调用方不得用 string key 注入身份（docs/auth/authentication.md §2）。
type identityContextKey struct{}

// ContextWithIdentity 写入 clone(arr)，避免调用方持有 Authenticator 缓存引用。
func ContextWithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, cloneIdentity(identity))
}

// IdentityFromContext 读出 clone，Ok 时返回的 identity 与 ctx 内副本互不影响。
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	identity, ok := ctx.Value(identityContextKey{}).(*Identity)
	if !ok || identity == nil {
		return nil, false
	}
	return cloneIdentity(identity), true
}
