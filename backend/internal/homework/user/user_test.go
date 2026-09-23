package user

import (
	"context"
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

func TestEnsureCreatesThenReuses(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	sub := fmt.Sprintf("user-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Where("stride_user_id = ?", sub).Delete(&model.User{}) })

	u1, err := s.Ensure(ctx, sub, "小明")
	if err != nil {
		t.Fatal(err)
	}
	if u1.ID == 0 || u1.WxNickname != "小明" {
		t.Fatalf("unexpected created user: %+v", u1)
	}

	u2, err := s.Ensure(ctx, sub, "别的名字")
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID != u1.ID {
		t.Fatalf("Ensure created a second row: %d vs %d", u1.ID, u2.ID)
	}
	if u2.WxNickname != "小明" {
		t.Fatalf("existing nickname should not be overwritten, got %q", u2.WxNickname)
	}

	u3, err := s.UpdateNickname(ctx, sub, "小红")
	if err != nil {
		t.Fatal(err)
	}
	if u3.WxNickname != "小红" {
		t.Fatalf("nickname = %q, want 小红", u3.WxNickname)
	}
}
