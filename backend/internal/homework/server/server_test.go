package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/server"
)

func TestHealthz(t *testing.T) {
	h := server.New(&config.Config{Env: "test"}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id header")
	}
}

func TestBusinessRouteRequiresAuth(t *testing.T) {
	h := server.New(&config.Config{Env: "test"}, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var got struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "unauthorized" {
		t.Fatalf("error = %q, want unauthorized", got.Error)
	}
}

func TestBusinessRouteWithTokenReachesHandler(t *testing.T) {
	h := server.New(&config.Config{Env: "test"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer placeholder")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (placeholder)", rec.Code)
	}
}
