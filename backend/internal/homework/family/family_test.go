package family

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

func TestChildLifecycle(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	a := mkUser(t, db, "t03-a")
	b := mkUser(t, db, "t03-b")

	child, err := s.CreateChild(ctx, a.ID, "小明", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("child_id = ?", child.ID).Delete(&model.Guardianship{})
		db.Delete(&model.Child{}, child.ID)
	})

	// 创建者自动成为监护人。
	if ok, err := s.IsGuardian(ctx, child.ID, a.ID); err != nil || !ok {
		t.Fatalf("creator should be guardian: %v %v", ok, err)
	}
	if ok, _ := s.IsGuardian(ctx, child.ID, b.ID); ok {
		t.Fatal("b should not be guardian yet")
	}

	// 我的孩子。
	mine, err := s.ListChildren(ctx, a.ID)
	if err != nil || len(mine) != 1 || mine[0].ID != child.ID {
		t.Fatalf("ListChildren(A) = %+v, %v", mine, err)
	}
	if other, _ := s.ListChildren(ctx, b.ID); len(other) != 0 {
		t.Fatalf("ListChildren(B) should be empty, got %+v", other)
	}

	// 改名。
	newName := "明明"
	updated, err := s.UpdateChild(ctx, child.ID, &newName, nil)
	if err != nil || updated.Name != "明明" {
		t.Fatalf("UpdateChild = %+v, %v", updated, err)
	}

	// 邀请-接受：AddGuardian 幂等（重复调用不报错）。
	if err := s.AddGuardian(ctx, child.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.AddGuardian(ctx, child.ID, b.ID); err != nil {
		t.Fatalf("AddGuardian should be idempotent: %v", err)
	}

	// 移除 A 后剩 B；再移除 B 命中 last_guardian。
	if err := s.RemoveGuardian(ctx, child.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGuardian(ctx, child.ID, b.ID); !errors.Is(err, ErrLastGuardian) {
		t.Fatalf("expected ErrLastGuardian, got %v", err)
	}
	// 目标不是监护人。
	if err := s.RemoveGuardian(ctx, child.ID, a.ID); !errors.Is(err, ErrGuardianNotFound) {
		t.Fatalf("expected ErrGuardianNotFound, got %v", err)
	}
}
