// Package calendar 提供上学日历（school_calendar）读写与 kind 判定（T09）。
//
// 判定规则（research/005、homework-parsing §5.2）：
//   - verified=false 的记录不参与业务判定；
//   - 调休补班日（adjusted_workday）→ day；
//   - 法定节假日（holiday）与周末 → holiday，并自动定位连续休息段的 start/end/name；
//   - 其余工作日 → day。
package calendar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KindDay     = "day"
	KindHoliday = "holiday"

	TypeHoliday         = "holiday"
	TypeAdjustedWorkday = "adjusted_workday"

	// windowDays 决定定位连续休息段时向外查看的天数；远大于任何真实假期。
	windowDays = 45
)

// cst 用固定偏移避免依赖 tzdata（业务时区硬编码 Asia/Shanghai）。
var cst = time.FixedZone("CST", 8*3600)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// DayKind 是某个日期的判定结果。Kind=day 时其余字段为零值。
type DayKind struct {
	Kind        string    `json:"kind"`
	StartDate   time.Time `json:"startDate,omitempty"`
	EndDate     time.Time `json:"endDate,omitempty"`
	HolidayName string    `json:"holidayName,omitempty"`
}

func (s *Store) List(ctx context.Context, from, to time.Time) ([]model.SchoolCalendar, error) {
	var out []model.SchoolCalendar
	err := s.db.WithContext(ctx).
		Where("date >= ? AND date <= ?", dateOnly(from), dateOnly(to)).
		Order("date").
		Find(&out).Error
	return out, err
}

// Resolve 判定日期是上学日还是休息日；只读 verified=true 记录。
func (s *Store) Resolve(ctx context.Context, date time.Time) (DayKind, error) {
	date = dateOnly(date)
	var entries []model.SchoolCalendar
	err := s.db.WithContext(ctx).
		Where("verified = ? AND date >= ? AND date <= ?", true,
			date.AddDate(0, 0, -windowDays), date.AddDate(0, 0, windowDays)).
		Find(&entries).Error
	if err != nil {
		return DayKind{}, err
	}
	return resolve(date, entries), nil
}

// resolve 是纯函数：entries 中 verified=false 的一律忽略。
func resolve(date time.Time, entries []model.SchoolCalendar) DayKind {
	byDate := make(map[string]model.SchoolCalendar, len(entries))
	for _, e := range entries {
		if e.Verified {
			byDate[key(e.Date)] = e
		}
	}
	isRest := func(d time.Time) bool {
		if e, ok := byDate[key(d)]; ok {
			return e.Type == TypeHoliday // adjusted_workday 是上学日
		}
		wd := d.Weekday()
		return wd == time.Saturday || wd == time.Sunday
	}

	if !isRest(date) {
		return DayKind{Kind: KindDay}
	}

	start := date
	for isRest(start.AddDate(0, 0, -1)) {
		start = start.AddDate(0, 0, -1)
	}
	end := date
	for isRest(end.AddDate(0, 0, 1)) {
		end = end.AddDate(0, 0, 1)
	}

	name := ""
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if e, ok := byDate[key(d)]; ok && e.Type == TypeHoliday && e.Name != "" {
			name = e.Name
			break
		}
	}
	return DayKind{Kind: KindHoliday, StartDate: start, EndDate: end, HolidayName: name}
}

// SeedEntry 是种子 JSON 的一条（research/005 §4）。
type SeedEntry struct {
	Date     string `json:"date"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
}

// Import 把种子条目 upsert 进 school_calendar；verified=true 时整批标记为已核验。
func (s *Store) Import(ctx context.Context, entries []SeedEntry, verified bool) (int, error) {
	count := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, e := range entries {
			d, err := ParseDate(e.Date)
			if err != nil {
				return fmt.Errorf("calendar: 种子日期 %q: %w", e.Date, err)
			}
			t := e.Type
			if t == "" {
				t = TypeHoliday
			}
			row := model.SchoolCalendar{
				Date:     d,
				Type:     t,
				Name:     e.Name,
				Verified: verified || e.Verified,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "date"}},
				DoUpdates: clause.AssignmentColumns([]string{"type", "name", "verified"}),
			}).Create(&row).Error; err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// Today 返回 CST 当日（以 UTC 零点表示，与库内 DATE 约定一致）。
func Today() time.Time {
	return dateOnly(time.Now().In(cst))
}

// ParseDate 解析 YYYY-MM-DD 为 UTC 零点。
func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New("calendar: 日期格式应为 YYYY-MM-DD")
	}
	return dateOnly(t), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func key(t time.Time) string { return t.Format("2006-01-02") }
