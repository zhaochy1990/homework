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
