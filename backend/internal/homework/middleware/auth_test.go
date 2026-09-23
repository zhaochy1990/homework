package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
)

func TestAuth(t *testing.T) {
	verify := func(raw string) (*auth.Claims, error) {
		if raw == "good" {
			return &auth.Claims{Subject: "user-1"}, nil
		}
		return nil, errors.New("bad token")
	}

	var gotSubject string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := Claims(r.Context())
		if !ok {
			t.Error("claims missing from context")
		} else {
			gotSubject = c.Subject
		}
		w.WriteHeader(http.StatusOK)
	})
	h := Auth(verify)(next)

	cases := []struct {
		name    string
		header  string
		want    int
		wantErr string
	}{
		{"no header", "", http.StatusUnauthorized, "unauthorized"},
		{"empty bearer", "Bearer ", http.StatusUnauthorized, "unauthorized"},
		{"not bearer", "Token abc", http.StatusUnauthorized, "unauthorized"},
		{"bad token", "Bearer bad", http.StatusUnauthorized, "invalid_token"},
		{"valid", "Bearer good", http.StatusOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.header != "" {
				req.Header.Set("Authorization", c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
			if c.wantErr != "" {
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error != c.wantErr {
					t.Fatalf("error = %q, want %q", body.Error, c.wantErr)
				}
			}
		})
	}
	if gotSubject != "user-1" {
		t.Fatalf("subject = %q, want user-1", gotSubject)
	}
}
