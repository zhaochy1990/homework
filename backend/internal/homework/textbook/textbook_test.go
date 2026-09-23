package textbook

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

func mkClass(t *testing.T, db *gorm.DB, createdBy uint64) model.Class {
	t.Helper()
	c := model.Class{
		Name:       fmt.Sprintf("t06-class-%d", time.Now().UnixNano()),
		Visibility: "private",
		InviteCode: fmt.Sprintf("c%012d", time.Now().UnixNano()%1e12),
		CreatedBy:  createdBy,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("class_id = ?", c.ID).Delete(&model.ClassTextbook{})
		db.Where("class_id = ?", c.ID).Delete(&model.ClassMember{})
		db.Delete(&model.Class{}, c.ID)
	})
	return c
}

func mkTextbook(t *testing.T, s *Store, userID uint64, subj string) *Textbook {
	t.Helper()
	tb, err := s.Create(context.Background(), userID, model.Textbook{
		Subject: subj,
		Name:    fmt.Sprintf("t06-%s-%d", subj, time.Now().UnixNano()),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db := s.db
		db.Where("textbook_id = ?", tb.ID).Delete(&model.TextbookUnit{})
		db.Delete(&model.Textbook{}, tb.ID)
	})
	return tb
}

func TestTextbookLifecycle(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()
	u := mkUser(t, db, "t06")
	class := mkClass(t, db, u.ID)

	tb, err := s.Create(ctx, u.ID, model.Textbook{
		Subject: "数学", Name: fmt.Sprintf("人教版数学-%d", time.Now().UnixNano()),
		Grade: "三年级", Term: "上册",
	}, []UnitInput{{Name: "Unit 1"}, {Name: "Unit 2", SortOrder: 5}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("textbook_id = ?", tb.ID).Delete(&model.TextbookUnit{})
		db.Delete(&model.Textbook{}, tb.ID)
	})
	if tb.ID == 0 || len(tb.Units) != 2 {
		t.Fatalf("Create = %+v, want 2 units", tb)
	}
	if tb.Units[0].SortOrder != 1 || tb.Units[1].SortOrder != 5 {
		t.Fatalf("sort orders = %d, %d; want 1, 5", tb.Units[0].SortOrder, tb.Units[1].SortOrder)
	}

	// 补充单元。
	updated, err := s.AddUnits(ctx, tb.ID, []UnitInput{{Name: "Unit 3"}})
	if err != nil || len(updated.Units) != 3 {
		t.Fatalf("AddUnits = %+v, %v", updated, err)
	}
	if _, err := s.AddUnits(ctx, 1<<40, []UnitInput{{Name: "x"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddUnits missing = %v, want ErrNotFound", err)
	}

	// 列表检索，返回值带单元。
	items, total, err := s.List(ctx, "数学", tb.Name, 0, 20)
	if err != nil || total < 1 {
		t.Fatalf("List = %d, %v", total, err)
	}
	found := false
	for _, it := range items {
		if it.ID == tb.ID {
			found = true
			if len(it.Units) != 3 {
				t.Fatalf("listed units = %d, want 3", len(it.Units))
			}
		}
	}
	if !found {
		t.Fatalf("created textbook %d not in list", tb.ID)
	}

	// 班级选用 + 换教材（同科目替换）。
	if err := s.SetClassTextbook(ctx, class.ID, "数学", tb.ID); err != nil {
		t.Fatal(err)
	}
	tbB := mkTextbook(t, s, u.ID, "数学")
	if err := s.SetClassTextbook(ctx, class.ID, "数学", tbB.ID); err != nil {
		t.Fatal(err)
	}
	chosen, err := s.ListClassTextbooks(ctx, class.ID)
	if err != nil || len(chosen) != 1 || chosen[0].Textbook.ID != tbB.ID {
		t.Fatalf("ListClassTextbooks = %+v, %v", chosen, err)
	}

	// 科目不一致 / 教材不存在。
	if err := s.SetClassTextbook(ctx, class.ID, "语文", tb.ID); !errors.Is(err, ErrSubjectMismatch) {
		t.Fatalf("subject mismatch = %v, want ErrSubjectMismatch", err)
	}
	if err := s.SetClassTextbook(ctx, class.ID, "数学", 1<<40); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing textbook = %v, want ErrNotFound", err)
	}
}
