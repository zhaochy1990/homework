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
		Env:               "test",
		InviteTokenSecret: "test-invite-secret",
		Auth:              config.Auth{Issuer: testIssuer, Audience: testAudience, PublicKeyFile: path},
	}
	if base != nil {
		cfg.DB = base.DB
		cfg.WeChat = base.WeChat
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
	db, base := integrationDB(t)
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

func integrationDB(t *testing.T) (*gorm.DB, *config.Config) {
	t.Helper()
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
	return db, base
}

// TestGuardianFlow 是 T03 验收：两账号邀请-接受闭环 + 最后一个监护人不可移除。
func TestGuardianFlow(t *testing.T) {
	db, base := integrationDB(t)
	h, priv := newHandler(t, db, base)

	suffix := time.Now().UnixNano()
	subA := fmt.Sprintf("t03-a-%d", suffix)
	subB := fmt.Sprintf("t03-b-%d", suffix)
	t.Cleanup(func() { db.Where("stride_user_id IN ?", []string{subA, subB}).Delete(&model.User{}) })
	tokA := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = subA })
	tokB := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = subB })

	// A 建孩子，自动成为监护人。
	rec := do(h, http.MethodPost, "/api/v1/children", tokA, strings.NewReader(`{"name":"小明"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /children = %d, body=%s", rec.Code, rec.Body)
	}
	var child struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("child_id = ?", child.ID).Delete(&model.Guardianship{})
		db.Delete(&model.Child{}, child.ID)
	})

	// B 还看不到。
	if n := listCount(t, h, tokB); n != 0 {
		t.Fatalf("B should see no children, got %d", n)
	}

	// A 签发邀请，B 接受。
	rec = do(h, http.MethodPost, fmt.Sprintf("/api/v1/children/%d/guardian-invites", child.ID), tokA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("invite = %d, body=%s", rec.Code, rec.Body)
	}
	var invite struct {
		InviteToken string    `json:"inviteToken"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &invite); err != nil {
		t.Fatal(err)
	}
	if invite.InviteToken == "" || !invite.ExpiresAt.After(time.Now()) {
		t.Fatalf("bad invite: %+v", invite)
	}

	rec = do(h, http.MethodPost, "/api/v1/guardianships/accept", tokB,
		strings.NewReader(fmt.Sprintf(`{"inviteToken":%q}`, invite.InviteToken)))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept = %d, body=%s", rec.Code, rec.Body)
	}
	if n := listCount(t, h, tokB); n != 1 {
		t.Fatalf("B should see the child after accept, got %d", n)
	}

	// A 移除 B（需要 B 的本地 user id）。
	meB := getMeID(t, h, tokB)
	rec = do(h, http.MethodDelete, fmt.Sprintf("/api/v1/children/%d/guardians/%d", child.ID, meB), tokA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove guardian = %d, body=%s", rec.Code, rec.Body)
	}
	if n := listCount(t, h, tokB); n != 0 {
		t.Fatalf("B should lose access, got %d children", n)
	}

	// B 已非监护人：改孩子应 404。
	rec = do(h, http.MethodPatch, fmt.Sprintf("/api/v1/children/%d", child.ID), tokB, strings.NewReader(`{"name":"x"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-guardian PATCH = %d, want 404", rec.Code)
	}

	// A 是最后一个监护人：退出 → 409 last_guardian。
	rec = do(h, http.MethodDelete, fmt.Sprintf("/api/v1/children/%d/guardians/me", child.ID), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "last_guardian" {
		t.Fatalf("last guardian quit = %d %s", rec.Code, rec.Body)
	}
}

func listCount(t *testing.T, h http.Handler, token string) int {
	t.Helper()
	rec := do(h, http.MethodGet, "/api/v1/children", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /children = %d, body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Items []struct{} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return len(body.Items)
}

func getMeID(t *testing.T, h http.Handler, token string) uint64 {
	t.Helper()
	rec := do(h, http.MethodGet, "/api/v1/me", token, nil)
	var me struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	return me.ID
}
