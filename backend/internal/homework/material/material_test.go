package material

import (
	"context"
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

func TestVisibilityAndTraceIdempotency(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	classID := uint64(time.Now().UnixNano())
	t.Cleanup(func() { db.Where("class_id = ?", classID).Delete(&model.Material{}) })

	text, err := s.Create(ctx, CreateInput{
		ClassID: classID, Type: TypeText, Title: "笔记", Body: "正常内容",
		SecStatus: StatusPass, UploadedBy: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Create(ctx, CreateInput{
		ClassID: classID, Type: TypeImage, Title: "图", CosKey: "uploads/1/x.jpg",
		SizeBytes: 10, SecStatus: StatusPending, SecTraceID: "tr-x", UploadedBy: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 先审后显：只看到 pass。
	items, total, err := s.List(ctx, classID, nil, "", 0, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != text.ID {
		t.Fatalf("List = %+v total=%d err=%v", items, total, err)
	}

	// trace_id 幂等：第一次改，第二次 no-op。
	changed, err := s.SetSecStatusByTrace(ctx, "tr-x", StatusPass)
	if err != nil || !changed {
		t.Fatalf("first SetSecStatusByTrace = %v %v", changed, err)
	}
	changed, err = s.SetSecStatusByTrace(ctx, "tr-x", StatusBlocked)
	if err != nil || changed {
		t.Fatalf("second SetSecStatusByTrace should be no-op, got %v %v", changed, err)
	}
	if _, total, _ = s.List(ctx, classID, nil, "", 0, 20); total != 2 {
		t.Fatalf("after callback total = %d, want 2", total)
	}

	// 过滤：type=image 只回图片。
	onlyImages, _, err := s.List(ctx, classID, nil, TypeImage, 0, 20)
	if err != nil || len(onlyImages) != 1 || onlyImages[0].ID != pending.ID {
		t.Fatalf("image filter = %+v err=%v", onlyImages, err)
	}

	if err := s.Delete(ctx, text.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, text.ID); err != ErrNotFound {
		t.Fatalf("deleted Get = %v, want ErrNotFound", err)
	}
}
