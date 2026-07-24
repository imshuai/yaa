package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

type requestIDKey struct{}

// NewRequestID 生成一个有序且唯一请求 ID，格式 req_ + 13 位毫秒时间戳 + 16 位随机 hex。
// 满足 middleware 保证非空且在日志中可关联。
func NewRequestID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	ts := time.Now().UnixMilli()
	return "req_" + formatBase62(ts) + "_" + hex.EncodeToString(buf[:])
}

// formatBase62 返回非负整数的不定长 base62 字符串。
func formatBase62(n int64) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	out := make([]byte, 0, 11)
	for n > 0 {
		out = append([]byte{alphabet[n%62]}, out...)
		n /= 62
	}
	return string(out)
}

// requestIDMiddleware 为每个请求注入请求 ID：优先复用入站 X-Request-ID，否则生成新 ID。
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = NewRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		next.ServeHTTP(w, r)
	})
}

// RequestIDFromContext 取出请求 ID；缺失时返回空串（handler 经 middleware 保证非空）。
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
