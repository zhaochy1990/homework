package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/server"
	"gorm.io/gorm"
)

const (
	testIssuer   = "auth-service"
	testAudience = "app_test"
)

// newHandler 生成密钥对、写公钥文件并装配 handler，返回私钥供测试签 token。
func newHandler(t *testing.T, db *gorm.DB, base *config.Config) (http.Handler, []byte) {
	t.Helper()
	priv, pub, err := auth.GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(path, pub, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env:  "test",
		Auth: config.Auth{Issuer: testIssuer, Audience: testAudience, PublicKeyFile: path},
	}
	if base != nil {
		cfg.DB = base.DB
	}
	h, err := server.New(cfg, db)
	if err != nil {
		t.Fatal(err)
	}
	return h, priv
}

func tokenFor(t *testing.T, priv []byte, mutate func(*auth.Claims)) string {
	t.Helper()
	now := time.Now()
	c := auth.Claims{
		Subject:   "user-1",
		Audience:  testAudience,
		Issuer:    testIssuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(&c)
	}
	tok, err := auth.SignDevToken(priv, c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func do(h http.Handler, method, path, token string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Error
}

func TestHealthz(t *testing.T) {
	h, _ := newHandler(t, nil, nil)
	rec := do(h, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMeRejectsBadAuth(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	otherPriv, _, err := auth.GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"missing", "", "unauthorized"},
		{"garbage", "not-a-jwt", "invalid_token"},
		{"expired", tokenFor(t, priv, func(c *auth.Claims) { c.ExpiresAt = time.Now().Add(-time.Minute).Unix() }), "invalid_token"},
		{"wrong_aud", tokenFor(t, priv, func(c *auth.Claims) { c.Audience = "other_app" }), "invalid_token"},
		{"wrong_signature", tokenFor(t, otherPriv, nil), "invalid_token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(h, http.MethodGet, "/api/v1/me", c.token, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := errorCode(t, rec); got != c.want {
				t.Fatalf("error = %q, want %q", got, c.want)
			}
		})
	}
}

// TestMeFlow 是 T02 验收的全链路：工具签的 token 建用户、读用户、改昵称。
func TestMeFlow(t *testing.T) {
	if os.Getenv("TEST_MYSQL") == "" {
		t.Skip("set TEST_MYSQL=1 (and DB_*) to run against a real MySQL")
	}
	base, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(base.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	h, priv := newHandler(t, db, base)

	sub := fmt.Sprintf("me-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Where("stride_user_id = ?", sub).Delete(&model.User{}) })
	tok := tokenFor(t, priv, func(c *auth.Claims) {
		c.Subject = sub
		c.Name = "小明"
	})

	rec := do(h, http.MethodGet, "/api/v1/me", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me status = %d, body = %s", rec.Code, rec.Body)
	}
	var me struct {
		ID        uint64 `json:"id"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.ID == 0 || me.Nickname != "小明" {
		t.Fatalf("unexpected /me: %+v", me)
	}

	rec = do(h, http.MethodPatch, "/api/v1/me", tok, strings.NewReader(`{"nickname":"小红"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /me status = %d, body = %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Nickname != "小红" {
		t.Fatalf("nickname = %q, want 小红", me.Nickname)
	}

	rec = do(h, http.MethodGet, "/api/v1/me", tok, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Nickname != "小红" {
		t.Fatalf("persisted nickname = %q, want 小红", me.Nickname)
	}
}

func TestPatchMeValidatesNickname(t *testing.T) {
	h, priv := newHandler(t, nil, nil)
	tok := tokenFor(t, priv, nil)
	rec := do(h, http.MethodPatch, "/api/v1/me", tok, strings.NewReader(`{"nickname":"  "}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if got := errorCode(t, rec); got != "bad_request" {
		t.Fatalf("error = %q, want bad_request", got)
	}
}
