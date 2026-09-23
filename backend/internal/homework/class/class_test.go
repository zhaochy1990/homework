package class

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("TEST_MYSQL") == "" {
		t.Skip("set TEST_MYSQL=1 (and DB_*) to run against a real MySQL")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(cfg.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func mkUser(t *testing.T, db *gorm.DB, prefix string) model.User {
	t.Helper()
	u := model.User{StrideUserID: fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Delete(&model.User{}, u.ID) })
	return u
}

func mkChild(t *testing.T, db *gorm.DB, createdBy uint64) model.Child {
	t.Helper()
	c := model.Child{Name: "T05孩子", CreatedBy: createdBy}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("child_id = ?", c.ID).Delete(&model.Guardianship{})
		db.Where("child_id = ?", c.ID).Delete(&model.ChildEnrollment{})
		db.Unscoped().Delete(&model.Child{}, c.ID)
	})
	return c
}

func addGuardianRow(t *testing.T, db *gorm.DB, childID, userID uint64) {
	t.Helper()
	if err := db.Create(&model.Guardianship{ChildID: childID, UserID: userID}).Error; err != nil {
		t.Fatal(err)
	}
}

func cleanupClass(t *testing.T, db *gorm.DB, classID uint64) {
	db.Where("class_id = ?", classID).Delete(&model.ClassMember{})
	db.Where("class_id = ?", classID).Delete(&model.ClassJoinRequest{})
	db.Unscoped().Delete(&model.Class{}, classID)
}

func TestClassStoreLifecycle(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	owner := mkUser(t, db, "t04s-owner")
	other := mkUser(t, db, "t04s-other")

	c, err := s.CreateClass(ctx, owner.ID, "三班", "public", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupClass(t, db, c.ID) })

	if c.InviteCode == "" {
		t.Fatal("invite code should be generated")
	}
	if role, _ := s.MemberRole(ctx, c.ID, owner.ID); role != RoleAdmin {
		t.Fatalf("creator role = %q, want admin", role)
	}

	mine, err := s.ListMyClasses(ctx, owner.ID)
	if err != nil || len(mine) != 1 || mine[0].Role != RoleAdmin {
		t.Fatalf("ListMyClasses = %+v, %v", mine, err)
	}

	found, total, err := s.SearchPublicClasses(ctx, "三班", 0, 20)
	if err != nil || total < 1 || len(found) == 0 {
		t.Fatalf("SearchPublicClasses = %+v total=%d err=%v", found, total, err)
	}

	newName := "三班（改）"
	updated, err := s.UpdateClass(ctx, c.ID, &newName, nil, nil)
	if err != nil || updated.Name != newName {
		t.Fatalf("UpdateClass = %+v, %v", updated, err)
	}

	// 审批流：reject 不入班，approve 入班。
	jr, err := s.CreateJoinRequest(ctx, c.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.PendingJoinRequest(ctx, c.ID, other.ID); pending == nil || pending.ID != jr.ID {
		t.Fatal("pending request not found")
	}
	if _, err := s.DecideJoinRequest(ctx, jr.ID, owner.ID, false); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, other.ID); role != "" {
		t.Fatal("rejected user should not be a member")
	}
	if _, err := s.DecideJoinRequest(ctx, jr.ID, owner.ID, true); !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("re-decide = %v, want ErrAlreadyDecided", err)
	}

	jr2, err := s.CreateJoinRequest(ctx, c.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecideJoinRequest(ctx, jr2.ID, owner.ID, true); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, other.ID); role != RoleMember {
		t.Fatalf("approved role = %q, want member", role)
	}
	members, total, err := s.ListMembers(ctx, c.ID, 0, 20)
	if err != nil || total != 2 || len(members) != 2 {
		t.Fatalf("ListMembers = %+v total=%d err=%v", members, total, err)
	}

	// 软删：GetClass 不再可见。
	if err := s.Dissolve(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetClass(ctx, c.ID); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("dissolved GetClass = %v, want ErrClassNotFound", err)
	}
}

func TestEnrollmentGuardianSync(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	owner := mkUser(t, db, "t05-owner")
	guardian := mkUser(t, db, "t05-guardian")

	c, err := s.CreateClass(ctx, owner.ID, "五年二班", "public", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupClass(t, db, c.ID) })

	child1 := mkChild(t, db, owner.ID)
	addGuardianRow(t, db, child1.ID, owner.ID)
	addGuardianRow(t, db, child1.ID, guardian.ID)

	// 入班：owner 已是 admin（保留），guardian 自动成为 member。
	if err := s.EnrollChild(ctx, c.ID, child1.ID); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, owner.ID); role != RoleAdmin {
		t.Fatalf("owner role = %q, want admin", role)
	}
	if role, _ := s.MemberRole(ctx, c.ID, guardian.ID); role != RoleMember {
		t.Fatalf("guardian role = %q, want member", role)
	}
	children, total, err := s.ListChildrenInClass(ctx, c.ID, 0, 20)
	if err != nil || total != 1 || len(children) != 1 || children[0].ID != child1.ID {
		t.Fatalf("ListChildrenInClass = %+v total=%d err=%v", children, total, err)
	}
	if err := s.EnrollChild(ctx, c.ID, child1.ID); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate enroll = %v, want ErrAlreadyExists", err)
	}

	// guardian 的另一个孩子也入班：退掉 child1 后权限保留。
	child2 := mkChild(t, db, owner.ID)
	addGuardianRow(t, db, child2.ID, guardian.ID)
	if err := s.EnrollChild(ctx, c.ID, child2.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnenrollChild(ctx, c.ID, child1.ID); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, guardian.ID); role != RoleMember {
		t.Fatalf("guardian should keep membership via child2, got %q", role)
	}

	// 最后一个孩子退班：guardian 权限回收；owner（admin）保留。
	if err := s.UnenrollChild(ctx, c.ID, child2.ID); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, guardian.ID); role != "" {
		t.Fatalf("guardian membership should be reclaimed, got %q", role)
	}
	if role, _ := s.MemberRole(ctx, c.ID, owner.ID); role != RoleAdmin {
		t.Fatalf("admin membership should stay, got %q", role)
	}
	if err := s.UnenrollChild(ctx, c.ID, child2.ID); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("unenroll twice = %v, want ErrNotEnrolled", err)
	}

	// 新监护人接受邀请后补为已入班孩子的成员。
	late := mkUser(t, db, "t05-late")
	child3 := mkChild(t, db, owner.ID)
	addGuardianRow(t, db, child3.ID, owner.ID)
	if err := s.EnrollChild(ctx, c.ID, child3.ID); err != nil {
		t.Fatal(err)
	}
	addGuardianRow(t, db, child3.ID, late.ID)
	if err := s.AddGuardianToChildClasses(ctx, child3.ID, late.ID); err != nil {
		t.Fatal(err)
	}
	if role, _ := s.MemberRole(ctx, c.ID, late.ID); role != RoleMember {
		t.Fatalf("late guardian role = %q, want member", role)
	}
}
