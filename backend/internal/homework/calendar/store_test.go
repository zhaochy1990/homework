package calendar

import (
	"context"
	"os"
	"testing"

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

func TestStoreResolveAndList(t *testing.T) {
	db := testDB(t)
	s := NewStore(db)
	ctx := context.Background()

	days := []string{
		"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04",
		"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08",
		"2026-10-10", "2026-11-02",
	}
	t.Cleanup(func() { db.Where("date IN ?", days).Delete(&model.SchoolCalendar{}) })

	for _, day := range days {
		row := model.SchoolCalendar{Date: d(t, day), Type: TypeHoliday, Name: "国庆节", Verified: true}
		if day == "2026-10-10" {
			row = model.SchoolCalendar{Date: d(t, day), Type: TypeAdjustedWorkday, Verified: true}
		}
		if day == "2026-11-02" {
			row = model.SchoolCalendar{Date: d(t, day), Type: TypeHoliday, Name: "未核验", Verified: false}
		}
		if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Resolve(ctx, d(t, "2026-10-04"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindHoliday || !got.StartDate.Equal(d(t, "2026-10-01")) || !got.EndDate.Equal(d(t, "2026-10-08")) {
		t.Fatalf("national day resolve = %+v", got)
	}
	if got, _ := s.Resolve(ctx, d(t, "2026-10-10")); got.Kind != KindDay {
		t.Fatalf("adjusted workday resolve = %+v, want day", got)
	}
	if got, _ := s.Resolve(ctx, d(t, "2026-11-02")); got.Kind != KindDay {
		t.Fatalf("unverified holiday resolve = %+v, want day", got)
	}

	rows, err := s.List(ctx, d(t, "2026-10-01"), d(t, "2026-10-31"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 9 {
		t.Fatalf("List = %d rows, want 9", len(rows))
	}
}
