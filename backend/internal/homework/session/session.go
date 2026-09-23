// Package session 提供作业 Session（班级 × 科目 × 日期）的读写（T10 起，T11 扩展入库）。
package session

import (
	"context"
	"errors"
	"time"

	cal "github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("session: not found")

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// FindForDate 探测某班某科在日期 d（CST 日历日）已存在的 Session：
// 上学日按 date 精确匹配；假期 Session 按 [start_date, end_date] 区间覆盖。
// 解析端点用它回填 target=append（homework-parsing §5.3）。
func (s *Store) FindForDate(ctx context.Context, classID uint64, subject string, d time.Time) (*model.HomeworkSession, error) {
	var row model.HomeworkSession
	err := s.db.WithContext(ctx).
		Where("class_id = ? AND subject = ? AND "+
			"((kind = ? AND date = ?) OR "+
			"(kind = ? AND start_date <= ? AND end_date >= ?))",
			classID, subject, cal.KindDay, d, cal.KindHoliday, d, d).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// TodosBySession 返回 Session 内全部待办，按确认页最终顺序（sort_order）。
func (s *Store) TodosBySession(ctx context.Context, sessionID uint64) ([]model.Todo, error) {
	var todos []model.Todo
	err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("sort_order").
		Find(&todos).Error
	return todos, err
}
