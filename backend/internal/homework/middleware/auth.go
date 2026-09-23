package middleware

import (
	"net/http"
	"strings"

	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
)

// Auth 是 T01 的路由骨架占位：仅校验 Authorization: Bearer 头存在。
//
// TODO(T02): 替换为 RS256 本地验签（白名单 iss、exp 必填、aud 自校验、
// 公钥拉取缓存 + 本地文件 fallback），并把 sub 写入 request context。真实
// 实现前该中间件不构成任何身份保证，任何非空 Bearer 都会放行。
func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			httpx.Write(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "缺少登录凭证")
			return
		}
		next.ServeHTTP(w, r)
	})
}
