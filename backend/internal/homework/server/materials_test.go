package server_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/material"
	"github.com/zhaochy1990/homework/backend/internal/homework/media"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/server"
	"gorm.io/gorm"
)

const testMsgPushToken = "test-msgpush-token"

// fakeWechat 模拟 stable_token / msg_sec_check / media_check_async。
func fakeWechat(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tk", "expires_in": 7200})
		case "/wxa/msg_sec_check":
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			suggest := "pass"
			if strings.Contains(req.Content, "违规") {
				suggest = "risky"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "result": map[string]any{"suggest": suggest, "label": 100}})
		case "/wxa/media_check_async":
			_ = json.NewEncoder(w).Encode(map[string]any{"errcode": 0, "trace_id": "tr-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fakeIssuer struct{}

func (fakeIssuer) Issue(ctx context.Context, policy string, ttl time.Duration) (media.Credentials, error) {
	return media.Credentials{TmpSecretID: "id", TmpSecretKey: "key", SessionToken: "tok", ExpiredTime: time.Now().Add(ttl)}, nil
}

type fakeObjects struct{}

func (fakeObjects) Head(ctx context.Context, key string) (int64, error) { return 100, nil }
func (fakeObjects) Delete(ctx context.Context, key string) error        { return nil }
func (fakeObjects) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "https://cos.example/" + key, nil
}

func fakeMedia(db *gorm.DB) *media.Service {
	return media.NewWithDeps(media.Config{
		Bucket: "test-bucket", Region: "ap-shanghai", AppID: "1250000000",
		STSTTL: time.Hour, PlaybackTTL: time.Hour,
	}, media.NewStore(db), fakeIssuer{}, fakeObjects{})
}

func signCallback(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func callbackPath(token, traceID, suggest string) (string, string) {
	ts, nonce := fmt.Sprintf("%d", time.Now().Unix()), "nonce"
	sig := signCallback(token, ts, nonce)
	body := fmt.Sprintf(`{"MsgType":"event","Event":"wxa_media_check","trace_id":%q,"result":{"suggest":%q,"label":100}}`, traceID, suggest)
	return fmt.Sprintf("/api/v1/wx/callback?signature=%s&timestamp=%s&nonce=%s", sig, ts, nonce), body
}

func materialCount(t *testing.T, h http.Handler, token string, classID uint64) int {
	t.Helper()
	rec := do(h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d/materials", classID), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list materials = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Items []struct {
			ID uint64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return len(body.Items)
}

func TestWxCallbackEchoStr(t *testing.T) {
	base := &config.Config{WeChat: config.WeChat{MsgPushToken: testMsgPushToken}}
	h, _ := newHandler(t, nil, base)
	ts, nonce := "1700000000", "nonce"
	sig := signCallback(testMsgPushToken, ts, nonce)

	rec := do(h, http.MethodGet,
		fmt.Sprintf("/api/v1/wx/callback?signature=%s&timestamp=%s&nonce=%s&echostr=hello", sig, ts, nonce), "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("echostr = %d %q", rec.Code, rec.Body.String())
	}
	rec = do(h, http.MethodGet, "/api/v1/wx/callback?signature=bad&timestamp=1&nonce=2&echostr=hello", "", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad signature = %d, want 403", rec.Code)
	}
}

func TestMaterialTextContentSecurity(t *testing.T) {
	db, base := integrationDB(t)
	base.WeChat = config.WeChat{AppID: "app", AppSecret: "secret", APIBase: fakeWechat(t).URL, MsgPushToken: testMsgPushToken}
	h, priv := newHandler(t, db, base)
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)

	tokA := e.token("t08a-a")
	tokB := e.token("t08a-b")
	tokC := e.token("t08a-c")
	cls := e.createClass(tokA, "资料班", "public", false)
	if rec := do(h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(cls))); rec.Code != http.StatusOK {
		t.Fatalf("B join = %d", rec.Code)
	}

	// 文本同步拦截：命中 → 422，不入库。
	rec := do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/materials", cls), tokA,
		strings.NewReader(`{"type":"text","title":"笔记","body":"这段是违规内容"}`))
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "content_blocked" {
		t.Fatalf("blocked text = %d %s", rec.Code, rec.Body)
	}

	// 通过 → 201 pass，成员可见。
	rec = do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/materials", cls), tokA,
		strings.NewReader(`{"type":"text","title":"笔记","body":"数学错题整理 P1"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("pass text = %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID        uint64 `json:"id"`
		SecStatus string `json:"secStatus"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.SecStatus != material.StatusPass {
		t.Fatalf("secStatus = %q, want pass", created.SecStatus)
	}
	if n := materialCount(t, h, tokB, cls); n != 1 {
		t.Fatalf("member list = %d, want 1", n)
	}
	if rec := do(h, http.MethodGet, fmt.Sprintf("/api/v1/materials/%d", created.ID), tokB, nil); rec.Code != http.StatusOK {
		t.Fatalf("member detail = %d", rec.Code)
	}

	// 非 admin 不能发布；非成员看不到。
	if rec := do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/materials", cls), tokB,
		strings.NewReader(`{"type":"text","body":"x"}`)); rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin create = %d, want 403", rec.Code)
	}
	if rec := do(h, http.MethodGet, fmt.Sprintf("/api/v1/materials/%d", created.ID), tokC, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("non-member detail = %d, want 404", rec.Code)
	}
}

func TestMaterialMediaCallbackFlow(t *testing.T) {
	db, base := integrationDB(t)
	base.WeChat = config.WeChat{AppID: "app", AppSecret: "secret", APIBase: fakeWechat(t).URL, MsgPushToken: testMsgPushToken}
	svc := fakeMedia(db)
	h, priv := newHandler(t, db, base, server.WithMedia(svc))
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)

	tokA := e.token("t08b-a")
	tokB := e.token("t08b-b")
	cls := e.createClass(tokA, "媒体资料班", "public", false)
	if rec := do(h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(cls))); rec.Code != http.StatusOK {
		t.Fatalf("B join = %d", rec.Code)
	}
	userA := getMeID(t, h, tokA)

	// 造一条已 confirm 的直传凭证。
	ctx := context.Background()
	ticket, err := svc.CreateTicket(ctx, userA, media.KindMaterialImage, "image/jpeg", 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Where("upload_id = ?", ticket.UploadID).Delete(&model.UploadTicket{}) })
	if _, err := svc.Confirm(ctx, userA, ticket.UploadID, 100, "", ""); err != nil {
		t.Fatal(err)
	}

	// 媒体资料落 pending，成员不可见。
	body := fmt.Sprintf(`{"type":"image","title":"作业照片","uploadId":%q}`, ticket.UploadID)
	rec := do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/materials", cls), tokA, strings.NewReader(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create media material = %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID        uint64 `json:"id"`
		SecStatus string `json:"secStatus"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if n := materialCount(t, h, tokB, cls); n != 0 {
		t.Fatalf("pending should be invisible, list = %d", n)
	}
	if rec := do(h, http.MethodGet, fmt.Sprintf("/api/v1/materials/%d", created.ID), tokB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("pending detail = %d, want 404", rec.Code)
	}

	// 伪造签名被拒。
	if rec := do(h, http.MethodPost, "/api/v1/wx/callback?signature=bad&timestamp=1&nonce=2", "", strings.NewReader("{}")); rec.Code != http.StatusForbidden {
		t.Fatalf("bad signature = %d, want 403", rec.Code)
	}

	// 回调 pass → 可见，detail 带播放 URL。
	path, cbBody := callbackPath(testMsgPushToken, "tr-1", "pass")
	if rec := do(h, http.MethodPost, path, "", strings.NewReader(cbBody)); rec.Code != http.StatusOK {
		t.Fatalf("callback = %d %s", rec.Code, rec.Body)
	}
	if n := materialCount(t, h, tokB, cls); n != 1 {
		t.Fatalf("after callback list = %d, want 1", n)
	}
	rec = do(h, http.MethodGet, fmt.Sprintf("/api/v1/materials/%d", created.ID), tokB, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail after pass = %d", rec.Code)
	}
	var detail struct {
		PlaybackURL string `json:"playbackUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.PlaybackURL == "" {
		t.Fatal("expected playbackUrl")
	}

	// 重复回调幂等（仍 200）。
	if rec := do(h, http.MethodPost, path, "", strings.NewReader(cbBody)); rec.Code != http.StatusOK {
		t.Fatalf("idempotent callback = %d", rec.Code)
	}
}

func TestMaterialMediaBlockedStaysInvisible(t *testing.T) {
	db, base := integrationDB(t)
	base.WeChat = config.WeChat{AppID: "app", AppSecret: "secret", APIBase: fakeWechat(t).URL, MsgPushToken: testMsgPushToken}
	svc := fakeMedia(db)
	h, priv := newHandler(t, db, base, server.WithMedia(svc))
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)

	tokA := e.token("t08c-a")
	cls := e.createClass(tokA, "拦截班", "public", false)
	userA := getMeID(t, h, tokA)

	ctx := context.Background()
	ticket, err := svc.CreateTicket(ctx, userA, media.KindMaterialVideo, "video/mp4", 100)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Where("upload_id = ?", ticket.UploadID).Delete(&model.UploadTicket{}) })
	if _, err := svc.Confirm(ctx, userA, ticket.UploadID, 100, "", ""); err != nil {
		t.Fatal(err)
	}
	rec := do(h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/materials", cls), tokA,
		strings.NewReader(fmt.Sprintf(`{"type":"video","uploadId":%q}`, ticket.UploadID)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}

	path, cbBody := callbackPath(testMsgPushToken, "tr-1", "risky")
	if rec := do(h, http.MethodPost, path, "", strings.NewReader(cbBody)); rec.Code != http.StatusOK {
		t.Fatalf("blocked callback = %d", rec.Code)
	}
	if n := materialCount(t, h, tokA, cls); n != 0 {
		t.Fatalf("blocked material should stay invisible, list = %d", n)
	}
}
