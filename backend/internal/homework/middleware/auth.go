package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
)

type claimsKey struct{}

// Claims 取回已验签的 JWT claims（由 Auth 写入）。
func Claims(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*auth.Claims)
	return c, ok
}

// VerifyFunc 校验原始 token，失败一律映射为 401 invalid_token。
type VerifyFunc func(rawToken string) (*auth.Claims, error)

// Auth 校验 Bearer JWT（RS256 本地验签）并把 claims 写入 request context。
// 缺 Bearer → 401 unauthorized；校验失败 → 401 invalid_token（与 auth-service 一致）。
func Auth(verify VerifyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			raw = strings.TrimSpace(raw)
			if !ok || raw == "" {
				httpx.Write(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "缺少登录凭证")
				return
			}
			claims, err := verify(raw)
			if err != nil {
				httpx.Write(w, http.StatusUnauthorized, httpx.CodeInvalidToken, "登录凭证无效")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
