package calendar

import (
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
)

func d(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func holiday(date, name string) model.SchoolCalendar {
	v, _ := ParseDate(date)
	return model.SchoolCalendar{Date: v, Type: TypeHoliday, Name: name, Verified: true}
}

func TestResolveNationalDaySegment(t *testing.T) {
	// 2026 国庆连休 10/01–10/08（8 天），10/10 调休补班。
	entries := []model.SchoolCalendar{}
	for _, day := range []string{
		"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04",
		"2026-10-05", "2026-10-06", "2026-10-07", "2026-10-08",
	} {
		entries = append(entries, holiday(day, "国庆节"))
	}

	got := resolve(d(t, "2026-10-04"), entries)
	if got.Kind != KindHoliday {
		t.Fatalf("kind = %q, want holiday", got.Kind)
	}
	if !got.StartDate.Equal(d(t, "2026-10-01")) || !got.EndDate.Equal(d(t, "2026-10-08")) {
		t.Fatalf("segment = %v..%v, want 10-01..10-08", got.StartDate, got.EndDate)
	}
	if got.HolidayName != "国庆节" {
		t.Fatalf("name = %q", got.HolidayName)
	}
}

func TestResolveAdjustedWorkdayIsSchoolDay(t *testing.T) {
	// 2026-10-10 是周六，但为调休补班日 → day。
	entries := []model.SchoolCalendar{{
		Date: d(t, "2026-10-10"), Type: TypeAdjustedWorkday, Verified: true,
	}}
	if got := resolve(d(t, "2026-10-10"), entries); got.Kind != KindDay {
		t.Fatalf("adjusted workday kind = %q, want day", got.Kind)
	}
}

func TestResolvePlainWeekdayAndWeekend(t *testing.T) {
	// 2026-10-09 周五 → day。
	if got := resolve(d(t, "2026-10-09"), nil); got.Kind != KindDay {
		t.Fatalf("friday kind = %q, want day", got.Kind)
	}
	// 2026-10-10/11 周末 → holiday，段为周六..周日，无名称。
	got := resolve(d(t, "2026-10-10"), nil)
	if got.Kind != KindHoliday || !got.StartDate.Equal(d(t, "2026-10-10")) || !got.EndDate.Equal(d(t, "2026-10-11")) {
		t.Fatalf("weekend = %+v", got)
	}
	if got.HolidayName != "" {
		t.Fatalf("weekend name = %q, want empty", got.HolidayName)
	}
}

func TestResolveIgnoresUnverified(t *testing.T) {
	// 未核验的工作日假日不生效：2026-11-02 周一 → day。
	unverifiedHoliday := holiday("2026-11-02", "未核验")
	unverifiedHoliday.Verified = false
	if got := resolve(d(t, "2026-11-02"), []model.SchoolCalendar{unverifiedHoliday}); got.Kind != KindDay {
		t.Fatalf("unverified holiday kind = %q, want day", got.Kind)
	}

	// 未核验的调休补班日不生效：2026-10-10 周六仍是休息日。
	unverifiedWorkday := model.SchoolCalendar{Date: d(t, "2026-10-10"), Type: TypeAdjustedWorkday, Verified: false}
	if got := resolve(d(t, "2026-10-10"), []model.SchoolCalendar{unverifiedWorkday}); got.Kind != KindHoliday {
		t.Fatalf("unverified workday kind = %q, want holiday", got.Kind)
	}
}

func TestParseDate(t *testing.T) {
	if _, err := ParseDate("2026-10-01"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "2026/10/01", "2026-13-01", "not-a-date"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) should fail", bad)
		}
	}
}

func TestSeed2026(t *testing.T) {
	entries, err := Seed2026()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 13 {
		t.Fatalf("seed entries = %d, want 13", len(entries))
	}
	for _, e := range entries {
		if e.Date == "" || e.Type != TypeHoliday || e.Verified {
			t.Fatalf("unexpected seed entry: %+v", e)
		}
	}
}
