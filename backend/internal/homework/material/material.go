// Package material 管理班级学习资料与内容安全状态（T08，api-contract §3.6）。
package material

import (
	"context"
	"errors"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("material: 资料不存在")

const (
	TypeText  = "text"
	TypeImage = "image"
	TypeVideo = "video"

	StatusPending = "pending"
	StatusPass    = "pass"
	StatusBlocked = "blocked"
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

type CreateInput struct {
	ClassID    uint64
	UnitID     *uint64
	Type       string
	Title      string
	Body       string
	CosKey     string
	SizeBytes  int64
	SecStatus  string
	SecTraceID string
	UploadedBy uint64
}

func (s *Store) Create(ctx context.Context, in CreateInput) (*model.Material, error) {
	m := model.Material{
		ClassID:    in.ClassID,
		UnitID:     in.UnitID,
		Type:       in.Type,
		Title:      in.Title,
		Body:       in.Body,
		CosKey:     in.CosKey,
		SizeBytes:  in.SizeBytes,
		SecStatus:  in.SecStatus,
		SecTraceID: in.SecTraceID,
		UploadedBy: in.UploadedBy,
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) Get(ctx context.Context, id uint64) (*model.Material, error) {
	var m model.Material
	err := s.db.WithContext(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// List 只返回 sec_status=pass 的资料（先审后显）。
func (s *Store) List(ctx context.Context, classID uint64, unitID *uint64, typeFilter string, offset, limit int) ([]model.Material, int64, error) {
	apply := func(q *gorm.DB) *gorm.DB {
		q = q.Where("class_id = ? AND sec_status = ?", classID, StatusPass)
		if unitID != nil {
			q = q.Where("unit_id = ?", *unitID)
		}
		if typeFilter != "" {
			q = q.Where("type = ?", typeFilter)
		}
		return q
	}
	var total int64
	if err := apply(s.db.WithContext(ctx).Model(&model.Material{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.Material
	if err := apply(s.db.WithContext(ctx).Model(&model.Material{})).
		Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) Delete(ctx context.Context, id uint64) error {
	res := s.db.WithContext(ctx).Delete(&model.Material{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSecStatusByTrace 按 trace_id 幂等置状态：仅 pending 可被回调改写。
func (s *Store) SetSecStatusByTrace(ctx context.Context, traceID, status string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&model.Material{}).
		Where("sec_trace_id = ? AND sec_status = ?", traceID, StatusPending).
		Update("sec_status", status)
	return res.RowsAffected > 0, res.Error
}

// UnitExists 校验可选关联的教材单元是否存在。
func (s *Store) UnitExists(ctx context.Context, unitID uint64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.TextbookUnit{}).Where("id = ?", unitID).Count(&count).Error
	return count > 0, err
}
