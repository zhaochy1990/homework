package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := Auth(ok)

	cases := []struct {
		header string
		want   int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer ", http.StatusUnauthorized},
		{"Token abc", http.StatusUnauthorized},
		{"Bearer abc", http.StatusOK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.header != "" {
			req.Header.Set("Authorization", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("header %q: status = %d, want %d", c.header, rec.Code, c.want)
		}
	}
}
