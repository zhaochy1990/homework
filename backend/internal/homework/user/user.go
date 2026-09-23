// Package user 维护 stride_user_id -> users 行的本地映射（T02）。
package user

import (
	"context"
	"errors"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// Ensure 按 JWT sub upsert 用户：首见创建并写入 JWT name 作为初始昵称；
// 已存在则原样返回（后续昵称由 PATCH /me 维护）。并发首见撞唯一键时重查一次。
func (s *Store) Ensure(ctx context.Context, strideUserID, nickname string) (*model.User, error) {
	db := s.db.WithContext(ctx)

	var u model.User
	err := db.Where("stride_user_id = ?", strideUserID).First(&u).Error
	if err == nil {
		return &u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	u = model.User{StrideUserID: strideUserID, WxNickname: nickname}
	if err := db.Create(&u).Error; err != nil {
		var existing model.User
		if e := db.Where("stride_user_id = ?", strideUserID).First(&existing).Error; e == nil {
			return &existing, nil
		}
		return nil, err
	}
	return &u, nil
}

// UpdateNickname 覆盖昵称；用户必须已存在（先 Ensure）。
func (s *Store) UpdateNickname(ctx context.Context, strideUserID, nickname string) (*model.User, error) {
	db := s.db.WithContext(ctx)
	if err := db.Model(&model.User{}).
		Where("stride_user_id = ?", strideUserID).
		Update("wx_nickname", nickname).Error; err != nil {
		return nil, err
	}
	var u model.User
	if err := db.Where("stride_user_id = ?", strideUserID).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
