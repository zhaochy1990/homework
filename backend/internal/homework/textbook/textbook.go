// Package textbook 维护全局教材库、教材单元与班级按科目选用教材（T06）。
//
// 教材库全局共享、v1 无审核：任何登录用户可添加教材与补充单元。班级选用教材
// 需要管理员权限，由 handler 校验成员角色，store 只管数据。
package textbook

import (
	"context"
	"errors"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrNotFound 表示教材不存在。
	ErrNotFound = errors.New("textbook: 教材不存在")
	// ErrSubjectMismatch 表示教材所属科目与选用科目不一致。
	ErrSubjectMismatch = errors.New("textbook: 教材科目与选用科目不一致")
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// UnitInput 是一条待写入的教材单元；SortOrder 为 0 时按数组位置补齐。
type UnitInput struct {
	Name      string
	SortOrder int
}

// Textbook 是教材连同其单元的聚合。
type Textbook struct {
	model.Textbook
	Units []model.TextbookUnit
}

// ClassTextbook 是班级某科目选用的教材。
type ClassTextbook struct {
	Subject  string
	Textbook Textbook
}

// Create 新建教材并写入单元（同一事务）。
func (s *Store) Create(ctx context.Context, createdBy uint64, t model.Textbook, units []UnitInput) (*Textbook, error) {
	t.CreatedBy = createdBy
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		return insertUnits(tx, t.ID, units)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, t.ID)
}

// Get 取教材及其单元；不存在返回 ErrNotFound。
func (s *Store) Get(ctx context.Context, id uint64) (*Textbook, error) {
	var t model.Textbook
	if err := s.db.WithContext(ctx).First(&t, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	units, err := s.units(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Textbook{Textbook: t, Units: units}, nil
}

// List 按科目/关键词分页检索教材；返回值带各自的单元。total 忽略分页。
func (s *Store) List(ctx context.Context, subject, keyword string, offset, limit int) ([]Textbook, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.Textbook{})
	if subject != "" {
		db = db.Where("subject = ?", subject)
	}
	if keyword != "" {
		db = db.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.Textbook
	if err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]uint64, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	unitsByID, err := s.unitsByTextbookIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Textbook, len(rows))
	for i := range rows {
		out[i] = Textbook{Textbook: rows[i], Units: unitsByID[rows[i].ID]}
	}
	return out, total, nil
}

// AddUnits 向已有教材补充单元，返回更新后的教材；教材不存在返回 ErrNotFound。
func (s *Store) AddUnits(ctx context.Context, textbookID uint64, units []UnitInput) (*Textbook, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t model.Textbook
		if err := tx.First(&t, textbookID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		return insertUnits(tx, textbookID, units)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, textbookID)
}

// SetClassTextbook 按科目选用教材；同科目重复选用即替换。教材不存在返回
// ErrNotFound，教材科目与 subject 不一致返回 ErrSubjectMismatch。
func (s *Store) SetClassTextbook(ctx context.Context, classID uint64, subject string, textbookID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t model.Textbook
		if err := tx.First(&t, textbookID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if t.Subject != subject {
			return ErrSubjectMismatch
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "class_id"}, {Name: "subject"}},
			DoUpdates: clause.AssignmentColumns([]string{"textbook_id"}),
		}).Create(&model.ClassTextbook{ClassID: classID, Subject: subject, TextbookID: textbookID}).Error
	})
}

// ListClassTextbooks 返回班级按科目选用的教材（含单元），按科目排序。
func (s *Store) ListClassTextbooks(ctx context.Context, classID uint64) ([]ClassTextbook, error) {
	var rows []model.ClassTextbook
	if err := s.db.WithContext(ctx).Where("class_id = ?", classID).Order("subject").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ClassTextbook, 0, len(rows))
	for _, row := range rows {
		t, err := s.Get(ctx, row.TextbookID)
		if err != nil {
			return nil, err
		}
		out = append(out, ClassTextbook{Subject: row.Subject, Textbook: *t})
	}
	return out, nil
}

func (s *Store) units(ctx context.Context, textbookID uint64) ([]model.TextbookUnit, error) {
	var units []model.TextbookUnit
	err := s.db.WithContext(ctx).
		Where("textbook_id = ?", textbookID).
		Order("sort_order, id").
		Find(&units).Error
	return units, err
}

func (s *Store) unitsByTextbookIDs(ctx context.Context, ids []uint64) (map[uint64][]model.TextbookUnit, error) {
	out := map[uint64][]model.TextbookUnit{}
	if len(ids) == 0 {
		return out, nil
	}
	var units []model.TextbookUnit
	if err := s.db.WithContext(ctx).
		Where("textbook_id IN ?", ids).
		Order("sort_order, id").
		Find(&units).Error; err != nil {
		return nil, err
	}
	for _, u := range units {
		out[u.TextbookID] = append(out[u.TextbookID], u)
	}
	return out, nil
}

func insertUnits(tx *gorm.DB, textbookID uint64, units []UnitInput) error {
	for i, u := range units {
		order := u.SortOrder
		if order == 0 {
			order = i + 1
		}
		unit := model.TextbookUnit{TextbookID: textbookID, Name: u.Name, SortOrder: order}
		if err := tx.Create(&unit).Error; err != nil {
			return err
		}
	}
	return nil
}
