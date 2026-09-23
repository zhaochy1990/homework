package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/media"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/server"
	"gorm.io/gorm"
)

// mediaFakeIssuer 记录会话策略，模拟 STS 换票。
type mediaFakeIssuer struct{ lastPolicy string }

func (f *mediaFakeIssuer) Issue(_ context.Context, policy string, ttl time.Duration) (media.Credentials, error) {
	f.lastPolicy = policy
	return media.Credentials{
		TmpSecretID: "tmp-id", TmpSecretKey: "tmp-key", SessionToken: "tmp-token",
		ExpiredTime: time.Now().Add(ttl),
	}, nil
}

// mediaFakeObjects 内存私有桶：Put 模拟直传，Head/Delete/Presign 按存在性响应。
type mediaFakeObjects struct {
	objects map[string]int64
}

func newMediaFakeObjects() *mediaFakeObjects { return &mediaFakeObjects{objects: map[string]int64{}} }

func (f *mediaFakeObjects) Put(key string, size int64) { f.objects[key] = size }

func (f *mediaFakeObjects) Head(_ context.Context, key string) (int64, error) {
	if size, ok := f.objects[key]; ok {
		return size, nil
	}
	return 0, media.ErrObjectNotFound
}

func (f *mediaFakeObjects) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (f *mediaFakeObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://cos.example.com/" + key, nil
}

func newMediaHandler(t *testing.T, db *gorm.DB, base *config.Config) (http.Handler, []byte, *mediaFakeIssuer, *mediaFakeObjects) {
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
	}
	issuer := &mediaFakeIssuer{}
	objects := newMediaFakeObjects()
	svc := media.NewWithDeps(media.Config{
		Bucket: "homework-1250000000", Region: "ap-shanghai", AppID: "1250000000",
		STSTTL: 45 * time.Minute, PlaybackTTL: 2 * time.Hour,
	}, media.NewStore(db), issuer, objects)
	h, err := server.New(cfg, db, server.WithMedia(svc))
	if err != nil {
		t.Fatal(err)
	}
	return h, priv, issuer, objects
}

// TestMediaUploadFlow 是 T07 验收：直传 ticket → 模拟直传 → confirm 成功/失败两分支。
func TestMediaUploadFlow(t *testing.T) {
	db, base := integrationDB(t)
	h, priv, issuer, objects := newMediaHandler(t, db, base)

	sub := fmt.Sprintf("t07-%d", time.Now().UnixNano())
	tok := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = sub })
	t.Cleanup(func() {
		var u model.User
		if err := db.Where("stride_user_id = ?", sub).First(&u).Error; err == nil {
			db.Where("user_id = ?", u.ID).Delete(&model.UploadTicket{})
		}
		db.Where("stride_user_id = ?", sub).Delete(&model.User{})
	})

	ticket := createTicket(t, h, tok, media.KindCheckinMedia, "video/mp4", 1<<20)
	if ticket.UploadID == "" || !strings.HasPrefix(ticket.ObjectKey, media.UploadPrefix) {
		t.Fatalf("bad ticket: %+v", ticket)
	}
	if ticket.STS.TmpSecretID == "" || ticket.STS.ExpiredTime <= time.Now().Unix() {
		t.Fatalf("bad sts: %+v", ticket.STS)
	}
	// 临时密钥策略必须只覆盖 uploads/ 前缀下的该对象。
	if !strings.Contains(issuer.lastPolicy, media.UploadPrefix) || !strings.Contains(issuer.lastPolicy, ticket.ObjectKey) {
		t.Fatalf("policy not scoped: %s", issuer.lastPolicy)
	}
	if strings.Contains(issuer.lastPolicy, "cos:GetObject") || strings.Contains(issuer.lastPolicy, "cos:DeleteObject") {
		t.Fatalf("policy leaks read/delete: %s", issuer.lastPolicy)
	}

	// 失败分支：对象不存在 → 422 upload_mismatch。
	rec := confirmTicket(t, h, tok, ticket.UploadID, 1<<20)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "upload_mismatch" {
		t.Fatalf("missing object confirm = %d %s", rec.Code, rec.Body)
	}

	// 成功分支：模拟直传后 confirm。
	objects.Put(ticket.ObjectKey, 1<<20)
	rec = confirmTicket(t, h, tok, ticket.UploadID, 1<<20)
	if rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d, body=%s", rec.Code, rec.Body)
	}
	var confirmed struct {
		ObjectKey   string `json:"objectKey"`
		PlaybackURL string `json:"playbackUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed.ObjectKey != ticket.ObjectKey || confirmed.PlaybackURL == "" {
		t.Fatalf("bad confirm body: %s", rec.Body)
	}

	// 失败分支：大小谎报（对象存在但大小不符）→ 422。
	short := createTicket(t, h, tok, media.KindMaterialImage, "image/jpeg", 5000)
	objects.Put(short.ObjectKey, 1000)
	rec = confirmTicket(t, h, tok, short.UploadID, 5000)
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "upload_mismatch" {
		t.Fatalf("size mismatch confirm = %d %s", rec.Code, rec.Body)
	}
}

func TestMediaUploadValidatesInput(t *testing.T) {
	db, base := integrationDB(t)
	h, priv, _, _ := newMediaHandler(t, db, base)
	sub := fmt.Sprintf("t07-invalid-%d", time.Now().UnixNano())
	tok := tokenFor(t, priv, func(c *auth.Claims) { c.Subject = sub })
	t.Cleanup(func() { db.Where("stride_user_id = ?", sub).Delete(&model.User{}) })

	// 未登录 → 401。
	rec := do(h, http.MethodPost, "/api/v1/media/upload-tickets", "", strings.NewReader(`{}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", rec.Code)
	}
	// 未知 kind / 未知 contentType / 超限大小 → 400。
	for _, body := range []string{
		`{"kind":"bogus","contentType":"image/png","sizeBytes":10}`,
		`{"kind":"avatar","contentType":"application/pdf","sizeBytes":10}`,
		`{"kind":"avatar","contentType":"image/png","sizeBytes":999999999}`,
	} {
		rec := do(h, http.MethodPost, "/api/v1/media/upload-tickets", tok, strings.NewReader(body))
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "bad_request" {
			t.Fatalf("body %s = %d %s, want 400 bad_request", body, rec.Code, rec.Body)
		}
	}
}

type uploadTicketBody struct {
	UploadID  string `json:"uploadId"`
	ObjectKey string `json:"objectKey"`
	Bucket    string `json:"bucket"`
	Region    string `json:"region"`
	STS       struct {
		TmpSecretID  string `json:"tmpSecretId"`
		TmpSecretKey string `json:"tmpSecretKey"`
		SessionToken string `json:"sessionToken"`
		ExpiredTime  int64  `json:"expiredTime"`
	} `json:"sts"`
}

func createTicket(t *testing.T, h http.Handler, tok, kind, contentType string, size int64) uploadTicketBody {
	t.Helper()
	body := fmt.Sprintf(`{"kind":%q,"contentType":%q,"sizeBytes":%d}`, kind, contentType, size)
	rec := do(h, http.MethodPost, "/api/v1/media/upload-tickets", tok, strings.NewReader(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("create ticket = %d, body=%s", rec.Code, rec.Body)
	}
	var ticket uploadTicketBody
	if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	return ticket
}

func confirmTicket(t *testing.T, h http.Handler, tok, uploadID string, size int64) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"sizeBytes":%d,"etag":"e"}`, size)
	return do(h, http.MethodPost, "/api/v1/media/upload-tickets/"+uploadID+"/confirm", tok, strings.NewReader(body))
}
