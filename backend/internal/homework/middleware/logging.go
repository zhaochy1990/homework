package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type requestIDKey struct{}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// RequestID 返回当前请求的 trace id；不存在时返回空串。
// 后续 LLM 结构化日志请调用 Logger 以自动带上该字段。
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Logger 返回带 request_id 的 logger，供业务与 LLM 调用打结构化日志。
//
// LLM 日志字段约定（T10 起使用）：llm_model、llm_prompt_version、
// llm_latency_ms、llm_prompt_tokens、llm_completion_tokens。
func Logger(ctx context.Context) *slog.Logger {
	return slog.Default().With("request_id", RequestID(ctx))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Logging 记录每个请求的结构化访问日志（JSON）。
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set("X-Request-Id", rid)
		ctx := context.WithValue(r.Context(), requestIDKey{}, rid)

		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(ctx))
		if sw.status == 0 {
			sw.status = http.StatusOK
		}

		slog.InfoContext(ctx, "http request",
			"request_id", rid,
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}
