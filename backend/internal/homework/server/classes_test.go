package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

// env 管理一组 T04 集成测试的账号/班级/孩子，并在结束时清理。
type env struct {
	t        *testing.T
	db       *gorm.DB
	h        http.Handler
	priv     []byte
	classIDs []uint64
	childIDs []uint64
	subs     []string
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	db, base := integrationDB(t)
	for _, m := range mutate {
		m(base)
	}
	h, priv := newHandler(t, db, base)
	e := &env{t: t, db: db, h: h, priv: priv}
	t.Cleanup(e.cleanup)
	return e
}

func (e *env) cleanup() {
	if len(e.classIDs) > 0 {
		e.db.Where("class_id IN ?", e.classIDs).Delete(&model.ClassMember{})
		e.db.Where("class_id IN ?", e.classIDs).Delete(&model.ClassJoinRequest{})
		e.db.Where("class_id IN ?", e.classIDs).Delete(&model.ChildEnrollment{})
		e.db.Unscoped().Where("id IN ?", e.classIDs).Delete(&model.Class{})
	}
	if len(e.childIDs) > 0 {
		e.db.Where("child_id IN ?", e.childIDs).Delete(&model.TodoTick{})
		e.db.Where("child_id IN ?", e.childIDs).Delete(&model.Checkin{})
		e.db.Where("child_id IN ?", e.childIDs).Delete(&model.Guardianship{})
		e.db.Where("child_id IN ?", e.childIDs).Delete(&model.ChildEnrollment{})
		e.db.Unscoped().Where("id IN ?", e.childIDs).Delete(&model.Child{})
	}
	if len(e.subs) > 0 {
		e.db.Where("stride_user_id IN ?", e.subs).Delete(&model.User{})
	}
}

func (e *env) token(prefix string) string {
	sub := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	e.subs = append(e.subs, sub)
	return tokenFor(e.t, e.priv, func(c *auth.Claims) { c.Subject = sub })
}

func (e *env) createClass(token, name, visibility string, approval bool) uint64 {
	e.t.Helper()
	body := fmt.Sprintf(`{"name":%q,"visibility":%q,"joinApproval":%t}`, name, visibility, approval)
	rec := do(e.h, http.MethodPost, "/api/v1/classes", token, strings.NewReader(body))
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("POST /classes = %d, body=%s", rec.Code, rec.Body)
	}
	var c struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		e.t.Fatal(err)
	}
	e.classIDs = append(e.classIDs, c.ID)
	return c.ID
}

// enroll 给 userID 建一个孩子并在 classID 入班（用于 member_has_children /
// class_not_empty 分支，T05 端点落地前直接写表）。
func (e *env) enroll(userID, classID uint64) {
	e.t.Helper()
	child := model.Child{Name: "T04孩子", CreatedBy: userID}
	if err := e.db.Create(&child).Error; err != nil {
		e.t.Fatal(err)
	}
	e.childIDs = append(e.childIDs, child.ID)
	if err := e.db.Create(&model.Guardianship{ChildID: child.ID, UserID: userID}).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.Create(&model.ChildEnrollment{ChildID: child.ID, ClassID: classID}).Error; err != nil {
		e.t.Fatal(err)
	}
}

func joinBody(classID uint64) string {
	return fmt.Sprintf(`{"classId":%d}`, classID)
}

func TestClassJoinThreeBranches(t *testing.T) {
	e := newEnv(t)
	tokA := e.token("t04-creator")
	tokB := e.token("t04-b")
	tokC := e.token("t04-c")

	pubOpen := e.createClass(tokA, "公开无审批", "public", false)
	pubApproval := e.createClass(tokA, "公开有审批", "public", true)
	private := e.createClass(tokA, "私密班", "private", false)

	// 分支一：公开无审批 → 直接 member。
	rec := do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(pubOpen)))
	if rec.Code != http.StatusOK || statusOf(t, rec) != "approved" {
		t.Fatalf("public open join = %d %s", rec.Code, rec.Body)
	}
	rec = do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", pubOpen), tokB, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("B should be member: %d", rec.Code)
	}
	// 重复申请 → 409 already_exists。
	rec = do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(pubOpen)))
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "already_exists" {
		t.Fatalf("duplicate join = %d %s", rec.Code, rec.Body)
	}

	// 分支二：公开有审批 → pending，审批后成为 member。
	rec = do(e.h, http.MethodPost, "/api/v1/join-requests", tokC, strings.NewReader(joinBody(pubApproval)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("pending join = %d %s", rec.Code, rec.Body)
	}
	var pending struct {
		RequestID uint64 `json:"requestId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	rec = do(e.h, http.MethodPost, "/api/v1/join-requests", tokC, strings.NewReader(joinBody(pubApproval)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate pending = %d", rec.Code)
	}
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/join-requests/%d/approve", pending.RequestID), tokA, nil)
	if rec.Code != http.StatusOK || statusOf(t, rec) != "approved" {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body)
	}
	rec = do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", pubApproval), tokC, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("C should be member after approve: %d", rec.Code)
	}

	// 分支三：私密班凭邀请码 → 绕过审批直接 member。
	rec = do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(private)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("private without code = %d, want 403", rec.Code)
	}
	var cls model.Class
	if err := e.db.First(&cls, private).Error; err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"classId":%d,"inviteCode":%q}`, private, cls.InviteCode)
	rec = do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(body))
	if rec.Code != http.StatusOK || statusOf(t, rec) != "approved" {
		t.Fatalf("private with code = %d %s", rec.Code, rec.Body)
	}
}

func TestClassConflictBranches(t *testing.T) {
	e := newEnv(t)
	tokA := e.token("t04-owner")
	tokB := e.token("t04-member")
	cls := e.createClass(tokA, "冲突班", "public", false)
	if rec := do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(cls))); rec.Code != http.StatusOK {
		t.Fatalf("B join = %d", rec.Code)
	}
	userA := getMeID(t, e.h, tokA)
	userB := getMeID(t, e.h, tokB)
	e.enroll(userB, cls)

	// member_has_children：踢人 / 自己退出。
	rec := do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d/members/%d", cls, userB), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "member_has_children" {
		t.Fatalf("kick member with child = %d %s", rec.Code, rec.Body)
	}
	rec = do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d/members/me", cls), tokB, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "member_has_children" {
		t.Fatalf("quit with child = %d %s", rec.Code, rec.Body)
	}

	// admin_immutable：踢 admin（创建者自己）与降级创建者。
	rec = do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d/members/%d", cls, userA), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "admin_immutable" {
		t.Fatalf("kick admin = %d %s", rec.Code, rec.Body)
	}
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/members/%d/demote", cls, userA), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "admin_immutable" {
		t.Fatalf("demote creator = %d %s", rec.Code, rec.Body)
	}
	// promote 后 B 也是 admin，踢它同样 admin_immutable。
	if rec := do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/members/%d/promote", cls, userB), tokA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body)
	}
	rec = do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d/members/%d", cls, userB), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "admin_immutable" {
		t.Fatalf("kick promoted admin = %d %s", rec.Code, rec.Body)
	}
	// 非创建者不能 promote/demote。
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/classes/%d/members/%d/promote", cls, userA), tokB, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator promote = %d, want 403", rec.Code)
	}

	// class_not_empty：班内有孩子时禁止解散。
	rec = do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d", cls), tokA, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "class_not_empty" {
		t.Fatalf("dissolve non-empty = %d %s", rec.Code, rec.Body)
	}
}

func TestClassLeaveAndDissolve(t *testing.T) {
	e := newEnv(t)
	tokA := e.token("t04-owner2")
	tokB := e.token("t04-leaver")
	cls := e.createClass(tokA, "解散班", "public", false)
	if rec := do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(cls))); rec.Code != http.StatusOK {
		t.Fatalf("join = %d", rec.Code)
	}

	if rec := do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d/members/me", cls), tokB, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("quit = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("ex-member should 404, got %d", rec.Code)
	}

	if rec := do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/classes/%d", cls), tokA, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("dissolve = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("dissolved class should 404 (soft delete), got %d", rec.Code)
	}
}

func TestInviteQRCode(t *testing.T) {
	var tokenFx, qrFx bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/stable_token":
			tokenFx = true
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 7200})
		case "/wxa/getwxacodeunlimit":
			qrFx = true
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := newEnv(t, func(c *config.Config) {
		c.WeChat = config.WeChat{AppID: "app", AppSecret: "secret", APIBase: srv.URL, EnvVersion: "release"}
	})
	tokA := e.token("t04-qr-owner")
	tokB := e.token("t04-qr-member")
	cls := e.createClass(tokA, "二维码班", "public", false)

	rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d/invite-qrcode", cls), tokA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("qrcode = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Fatalf("body is not a PNG")
	}
	if !tokenFx || !qrFx {
		t.Fatalf("wechat endpoints not called: token=%v qr=%v", tokenFx, qrFx)
	}

	// 非 admin 成员无权限。
	if rec := do(e.h, http.MethodPost, "/api/v1/join-requests", tokB, strings.NewReader(joinBody(cls))); rec.Code != http.StatusOK {
		t.Fatalf("join = %d", rec.Code)
	}
	rec = do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d/invite-qrcode", cls), tokB, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member qrcode = %d, want 403", rec.Code)
	}
}

func statusOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Status
}
