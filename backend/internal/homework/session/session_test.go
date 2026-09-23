package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// day 把 YYYY-MM-DD 转成 UTC 零点（与库内 DATE 约定一致）。
func day(s string) time.Time {
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return tm
}

func TestFindForDate(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()

	const classID = uint64(9001)
	daySession := model.HomeworkSession{
		ClassID: classID, Subject: "数学", Kind: "day", Date: ptr(day("2026-09-22")), CreatedBy: 1,
	}
	holidaySession := model.HomeworkSession{
		ClassID: classID, Subject: "语文", Kind: "holiday",
		StartDate: ptr(day("2026-10-01")), EndDate: ptr(day("2026-10-08")),
		HolidayName: "国庆节", CreatedBy: 1,
	}
	t.Cleanup(func() {
		// todos 无 class_id 列，经 session 子查询清理（固定 ID 的残留也一并清）。
		db.Where("session_id IN (?)", db.Model(&model.HomeworkSession{}).Select("id").Where("class_id = ?", classID)).Delete(&model.Todo{})
		db.Where("id = ?", "01TESTSESSIONTODO000000000").Delete(&model.Todo{})
		db.Where("class_id = ?", classID).Delete(&model.HomeworkSession{})
	})
	for _, row := range []*model.HomeworkSession{&daySession, &holidaySession} {
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	todo := model.Todo{ID: "01TESTSESSIONTODO000000000", SessionID: daySession.ID, Content: "口算天天练 P12", SortOrder: 0}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		date    string
		subject string
		want    *uint64 // nil = not found
	}{
		{"2026-09-22", "数学", &daySession.ID},
		{"2026-09-21", "数学", nil}, // 相邻日不算
		{"2026-10-04", "语文", &holidaySession.ID},
		{"2026-10-04", "数学", nil}, // 假期里另一科无 session
		{"2026-10-09", "语文", nil}, // 假期结束次日
	}
	for _, c := range cases {
		got, err := s.FindForDate(ctx, classID, c.subject, day(c.date))
		if c.want == nil {
			if err == nil {
				t.Errorf("%s/%s: want not found, got session %d", c.date, c.subject, got.ID)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: %v", c.date, c.subject, err)
			continue
		}
		if got.ID != *c.want {
			t.Errorf("%s/%s: session = %d, want %d", c.date, c.subject, got.ID, *c.want)
		}
	}

	todos, err := s.TodosBySession(ctx, daySession.ID)
	if err != nil || len(todos) != 1 || todos[0].Content != "口算天天练 P12" {
		t.Fatalf("TodosBySession = %v, %v", todos, err)
	}
}

func ptr(t time.Time) *time.Time { return &t }
