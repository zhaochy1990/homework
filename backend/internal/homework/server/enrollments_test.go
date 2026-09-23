package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
)

// TestEnrollmentGuardianMembership 覆盖 T05 验收：监护人未入班被拒 →
// 入班后全部监护人自动 member → 退班回收权限且历史保留。
func TestEnrollmentGuardianMembership(t *testing.T) {
	e := newEnv(t)
	tokA := e.token("t05-a")
	tokB := e.token("t05-b")

	// A 建孩子。
	rec := do(e.h, http.MethodPost, "/api/v1/children", tokA, strings.NewReader(`{"name":"小明"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create child = %d %s", rec.Code, rec.Body)
	}
	var child struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &child); err != nil {
		t.Fatal(err)
	}
	e.childIDs = append(e.childIDs, child.ID)

	// A 建班（admin）。
	cls := e.createClass(tokA, "一年一班", "public", false)

	// B 接受监护人邀请 → B 是监护人但不是班级成员。
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/children/%d/guardian-invites", child.ID), tokA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("invite = %d %s", rec.Code, rec.Body)
	}
	var invite struct {
		InviteToken string `json:"inviteToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &invite); err != nil {
		t.Fatal(err)
	}
	rec = do(e.h, http.MethodPost, "/api/v1/guardianships/accept", tokB,
		strings.NewReader(fmt.Sprintf(`{"inviteToken":%q}`, invite.InviteToken)))
	if rec.Code != http.StatusOK {
		t.Fatalf("accept = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("B should not be a member yet: %d", rec.Code)
	}

	// 监护人未入班时报班被拒。
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/children/%d/enrollments", child.ID), tokB,
		strings.NewReader(fmt.Sprintf(`{"classId":%d}`, cls)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("guardian-not-member enroll = %d, want 403", rec.Code)
	}

	// A（已是成员）报班成功，B 自动成为 member。
	rec = do(e.h, http.MethodPost, fmt.Sprintf("/api/v1/children/%d/enrollments", child.ID), tokA,
		strings.NewReader(fmt.Sprintf(`{"classId":%d}`, cls)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("enroll = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokB, nil); rec.Code != http.StatusOK {
		t.Fatalf("B should be auto member: %d", rec.Code)
	}
	rec = do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d/children", cls), tokB, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("class children = %d", rec.Code)
	}
	var listed struct {
		Items []struct {
			ID uint64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != child.ID {
		t.Fatalf("class children = %+v", listed.Items)
	}

	// 插入勾选/打卡历史，退班后应保留。
	now := time.Now()
	if err := e.db.Create(&model.TodoTick{TodoID: "01TEST00000000000000000001", ChildID: child.ID, TickedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Create(&model.Checkin{
		SessionID: uint64(now.UnixNano()), ChildID: child.ID,
		CheckinDate: now, CheckedInAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 退班：B 权限回收，A（admin）保留。
	rec = do(e.h, http.MethodDelete, fmt.Sprintf("/api/v1/children/%d/enrollments/%d", child.ID, cls), tokA, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unenroll = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokB, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("B membership should be reclaimed: %d", rec.Code)
	}
	if rec := do(e.h, http.MethodGet, fmt.Sprintf("/api/v1/classes/%d", cls), tokA, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin membership should stay: %d", rec.Code)
	}

	// 历史保留（不删行），只是不再经班级视图展示。
	var ticks, checkins int64
	e.db.Model(&model.TodoTick{}).Where("child_id = ?", child.ID).Count(&ticks)
	e.db.Model(&model.Checkin{}).Where("child_id = ?", child.ID).Count(&checkins)
	if ticks != 1 || checkins != 1 {
		t.Fatalf("history not retained: ticks=%d checkins=%d", ticks, checkins)
	}
}
