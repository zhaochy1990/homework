package media

import (
	"context"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

// Store 持久化上传凭证。
type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, t *model.UploadTicket) error {
	return s.db.WithContext(ctx).Create(t).Error
}

// Get 按对外 uploadId 取记录；不存在返回 gorm.ErrRecordNotFound。
func (s *Store) Get(ctx context.Context, uploadID string) (*model.UploadTicket, error) {
	var t model.UploadTicket
	if err := s.db.WithContext(ctx).Where("upload_id = ?", uploadID).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// Confirm 把 pending 记录置为 confirmed。
func (s *Store) Confirm(ctx context.Context, id uint64, at time.Time) error {
	return s.db.WithContext(ctx).Model(&model.UploadTicket{}).
		Where("id = ? AND status = ?", id, "pending").
		Updates(map[string]any{"status": "confirmed", "confirmed_at": at}).Error
}

// ListExpiredPending 返回已超 STS 有效期但仍未 confirm 的记录（孤儿候选）。
func (s *Store) ListExpiredPending(ctx context.Context, now time.Time) ([]model.UploadTicket, error) {
	var tickets []model.UploadTicket
	err := s.db.WithContext(ctx).
		Where("status = ? AND expires_at < ?", "pending", now).
		Order("id").
		Find(&tickets).Error
	return tickets, err
}

// MarkCleaned 标记孤儿已清理；已 confirmed 的记录不覆盖。
func (s *Store) MarkCleaned(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&model.UploadTicket{}).
		Where("id = ? AND status = ?", id, "pending").
		Update("status", "cleaned").Error
}
