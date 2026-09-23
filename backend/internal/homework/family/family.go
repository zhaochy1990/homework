// Package family 管理孩子与监护关系（T03）。
package family

import (
	"context"
	"errors"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrLastGuardian 表示该操作会移除孩子的最后一个监护人。
	ErrLastGuardian = errors.New("family: 最后一个监护人不可移除")
	// ErrGuardianNotFound 表示目标用户不是该孩子的监护人。
	ErrGuardianNotFound = errors.New("family: 监护关系不存在")
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// CreateChild 创建孩子并把创建者登记为监护人（同一事务）。
func (s *Store) CreateChild(ctx context.Context, createdBy uint64, name, avatarURL string) (*model.Child, error) {
	child := model.Child{Name: name, AvatarURL: avatarURL, CreatedBy: createdBy}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&child).Error; err != nil {
			return err
		}
		return tx.Create(&model.Guardianship{ChildID: child.ID, UserID: createdBy}).Error
	})
	if err != nil {
		return nil, err
	}
	return &child, nil
}

// GetChild 取孩子；不存在返回 gorm.ErrRecordNotFound。
func (s *Store) GetChild(ctx context.Context, childID uint64) (*model.Child, error) {
	var child model.Child
	if err := s.db.WithContext(ctx).First(&child, childID).Error; err != nil {
		return nil, err
	}
	return &child, nil
}

// ListChildren 返回该用户担任监护人的全部孩子。
func (s *Store) ListChildren(ctx context.Context, userID uint64) ([]model.Child, error) {
	var children []model.Child
	err := s.db.WithContext(ctx).
		Model(&model.Child{}).
		Select("children.*").
		Joins("JOIN guardianships ON guardianships.child_id = children.id").
		Where("guardianships.user_id = ?", userID).
		Order("children.id").
		Find(&children).Error
	return children, err
}

// IsGuardian 判断该用户是否孩子的监护人。
func (s *Store) IsGuardian(ctx context.Context, childID, userID uint64) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Guardianship{}).
		Where("child_id = ? AND user_id = ?", childID, userID).
		Count(&count).Error
	return count > 0, err
}

// UpdateChild 更新姓名/头像（只更新非 nil 字段）。
func (s *Store) UpdateChild(ctx context.Context, childID uint64, name, avatarURL *string) (*model.Child, error) {
	updates := map[string]any{}
	if name != nil {
		updates["name"] = *name
	}
	if avatarURL != nil {
		updates["avatar_url"] = *avatarURL
	}
	db := s.db.WithContext(ctx)
	if len(updates) > 0 {
		if err := db.Model(&model.Child{}).Where("id = ?", childID).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.GetChild(ctx, childID)
}

// AddGuardian 建立监护关系；已是监护人时幂等成功。
func (s *Store) AddGuardian(ctx context.Context, childID, userID uint64) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.Guardianship{ChildID: childID, UserID: userID}).Error
}

// RemoveGuardian 移除监护关系；目标不是监护人→ErrGuardianNotFound，
// 会移除最后一个监护人→ErrLastGuardian。事务内锁行防并发。
func (s *Store) RemoveGuardian(ctx context.Context, childID, userID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var guardians []model.Guardianship
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("child_id = ?", childID).
			Find(&guardians).Error; err != nil {
			return err
		}
		found := false
		for _, g := range guardians {
			if g.UserID == userID {
				found = true
			}
		}
		if !found {
			return ErrGuardianNotFound
		}
		if len(guardians) <= 1 {
			return ErrLastGuardian
		}
		return tx.Where("child_id = ? AND user_id = ?", childID, userID).
			Delete(&model.Guardianship{}).Error
	})
}
