// Package class 管理班级、成员与加入申请（T04，api-contract §3.2）。
package class

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrClassNotFound     = errors.New("class: 班级不存在")
	ErrMemberNotFound    = errors.New("class: 成员不存在")
	ErrMemberHasChildren = errors.New("class: 成员有孩子在班")
	ErrAdminImmutable    = errors.New("class: 管理员不可被操作")
	ErrClassNotEmpty     = errors.New("class: 班级仍有孩子在班")
	ErrAlreadyExists     = errors.New("class: 已是成员或已有待审批申请")
	ErrAlreadyDecided    = errors.New("class: 申请已处理")
	ErrNotEnrolled       = errors.New("class: 孩子不在班")
)

const (
	RoleAdmin  = "admin"
	RoleMember = "member"

	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// 邀请码字母表：去掉易混淆字符，长度 8（scene ≤ 32）。
const inviteAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// CreateClass 建班，创建者自动成为 admin。
func (s *Store) CreateClass(ctx context.Context, createdBy uint64, name, visibility string, joinApproval bool) (*model.Class, error) {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := generateInviteCode()
		if err != nil {
			return nil, err
		}
		c := model.Class{
			Name:         name,
			Visibility:   visibility,
			JoinApproval: joinApproval,
			InviteCode:   code,
			CreatedBy:    createdBy,
		}
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			return tx.Create(&model.ClassMember{ClassID: c.ID, UserID: createdBy, Role: RoleAdmin}).Error
		})
		if err == nil {
			return &c, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, err
		}
	}
	return nil, errors.New("class: 邀请码生成失败")
}

// Membership 是"我的班级"条目：班级 + 我在其中的角色。
type Membership struct {
	model.Class
	Role string
}

func (s *Store) ListMyClasses(ctx context.Context, userID uint64) ([]Membership, error) {
	var out []Membership
	err := s.db.WithContext(ctx).
		Model(&model.Class{}).
		Select("classes.*, class_members.role").
		Joins("JOIN class_members ON class_members.class_id = classes.id").
		Where("class_members.user_id = ?", userID).
		Order("classes.id").
		Scan(&out).Error
	return out, err
}

func (s *Store) SearchPublicClasses(ctx context.Context, keyword string, offset, limit int) ([]model.Class, int64, error) {
	q := s.db.WithContext(ctx).Model(&model.Class{}).Where("visibility = ?", "public")
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var classes []model.Class
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&classes).Error; err != nil {
		return nil, 0, err
	}
	return classes, total, nil
}

func (s *Store) GetClass(ctx context.Context, classID uint64) (*model.Class, error) {
	var c model.Class
	if err := s.db.WithContext(ctx).First(&c, classID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrClassNotFound
		}
		return nil, err
	}
	return &c, nil
}

// UpdateClass 更新班级（只改非 nil 字段）。
func (s *Store) UpdateClass(ctx context.Context, classID uint64, name, visibility *string, joinApproval *bool) (*model.Class, error) {
	updates := map[string]any{}
	if name != nil {
		updates["name"] = *name
	}
	if visibility != nil {
		updates["visibility"] = *visibility
	}
	if joinApproval != nil {
		updates["join_approval"] = *joinApproval
	}
	db := s.db.WithContext(ctx)
	if len(updates) > 0 {
		res := db.Model(&model.Class{}).Where("id = ?", classID).Updates(updates)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			return nil, ErrClassNotFound
		}
	}
	return s.GetClass(ctx, classID)
}

// MemberRole 返回用户在班内的角色；非成员返回 ""。
func (s *Store) MemberRole(ctx context.Context, classID, userID uint64) (string, error) {
	var m model.ClassMember
	err := s.db.WithContext(ctx).Where("class_id = ? AND user_id = ?", classID, userID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return m.Role, nil
}

// AddMember 建立成员关系；已存在时幂等。
func (s *Store) AddMember(ctx context.Context, classID, userID uint64, role string) error {
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&model.ClassMember{ClassID: classID, UserID: userID, Role: role}).Error
}

type Member struct {
	UserID    uint64 `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	Role      string `json:"role"`
}

func (s *Store) ListMembers(ctx context.Context, classID uint64, offset, limit int) ([]Member, int64, error) {
	q := s.db.WithContext(ctx).Table("class_members").Where("class_id = ?", classID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var members []Member
	err := s.db.WithContext(ctx).Table("class_members").
		Select("class_members.user_id, users.wx_nickname AS nickname, users.avatar_url, class_members.role").
		Joins("JOIN users ON users.id = class_members.user_id").
		Where("class_members.class_id = ?", classID).
		Order("class_members.id").
		Offset(offset).Limit(limit).
		Scan(&members).Error
	return members, total, err
}

// SetRole 修改成员角色；目标非成员→ErrMemberNotFound。
func (s *Store) SetRole(ctx context.Context, classID, userID uint64, role string) error {
	res := s.db.WithContext(ctx).Model(&model.ClassMember{}).
		Where("class_id = ? AND user_id = ?", classID, userID).
		Update("role", role)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrMemberNotFound
	}
	return nil
}

// RemoveMember 踢人：admin→ErrAdminImmutable，有孩子在班→ErrMemberHasChildren。
func (s *Store) RemoveMember(ctx context.Context, classID, userID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.ClassMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("class_id = ? AND user_id = ?", classID, userID).First(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMemberNotFound
			}
			return err
		}
		if m.Role == RoleAdmin {
			return ErrAdminImmutable
		}
		has, err := hasChildrenInClass(tx, classID, userID)
		if err != nil {
			return err
		}
		if has {
			return ErrMemberHasChildren
		}
		return tx.Delete(&m).Error
	})
}

// Quit 退出班级：有孩子在班→ErrMemberHasChildren。
func (s *Store) Quit(ctx context.Context, classID, userID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.ClassMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("class_id = ? AND user_id = ?", classID, userID).First(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMemberNotFound
			}
			return err
		}
		has, err := hasChildrenInClass(tx, classID, userID)
		if err != nil {
			return err
		}
		if has {
			return ErrMemberHasChildren
		}
		return tx.Delete(&m).Error
	})
}

// MyChildrenInClass 返回当前用户在班内担任监护人的孩子。
func (s *Store) MyChildrenInClass(ctx context.Context, classID, userID uint64) ([]model.Child, error) {
	var children []model.Child
	err := s.db.WithContext(ctx).
		Model(&model.Child{}).
		Select("children.*").
		Joins("JOIN child_enrollments ON child_enrollments.child_id = children.id").
		Joins("JOIN guardianships ON guardianships.child_id = children.id").
		Where("child_enrollments.class_id = ? AND guardianships.user_id = ?", classID, userID).
		Order("children.id").
		Find(&children).Error
	return children, err
}

// HasChildrenInClass 判断该用户是否有孩子在本班（决定成员能否被踢/退）。
func (s *Store) HasChildrenInClass(ctx context.Context, classID, userID uint64) (bool, error) {
	return hasChildrenInClass(s.db.WithContext(ctx), classID, userID)
}

func hasChildrenInClass(db *gorm.DB, classID, userID uint64) (bool, error) {
	var count int64
	err := db.Table("child_enrollments").
		Joins("JOIN guardianships ON guardianships.child_id = child_enrollments.child_id").
		Where("child_enrollments.class_id = ? AND guardianships.user_id = ?", classID, userID).
		Count(&count).Error
	return count > 0, err
}

// Dissolve 解散班级（软删）；班内仍有孩子→ErrClassNotEmpty。
func (s *Store) Dissolve(ctx context.Context, classID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.ChildEnrollment{}).Where("class_id = ?", classID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrClassNotEmpty
		}
		res := tx.Delete(&model.Class{}, classID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrClassNotFound
		}
		return nil
	})
}

// ---------- 孩子入班与权限联动（T05，api-contract §3.3） ----------

// EnrollChild 孩子入班，同一事务内把孩子全部监护人补为 class member。
// 监护人须已是成员由 handler 校验（否则 403）。重复入班→ErrAlreadyExists。
func (s *Store) EnrollChild(ctx context.Context, classID, childID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.ChildEnrollment
		err := tx.Where("class_id = ? AND child_id = ?", classID, childID).First(&existing).Error
		if err == nil {
			return ErrAlreadyExists
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&model.ChildEnrollment{ChildID: childID, ClassID: classID}).Error; err != nil {
			return err
		}
		return addGuardiansAsMembers(tx, classID, childID)
	})
}

// UnenrollChild 退班：删 enrollment，并回收"因该孩子入班"获得的成员权限
// —— 该监护人无其他孩子在班且非 admin 时移除（data-model 实现备忘）。
// 勾选/打卡历史不删，只是不再展示。
func (s *Store) UnenrollChild(ctx context.Context, classID, childID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("class_id = ? AND child_id = ?", classID, childID).Delete(&model.ChildEnrollment{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotEnrolled
		}

		var guardians []model.Guardianship
		if err := tx.Where("child_id = ?", childID).Find(&guardians).Error; err != nil {
			return err
		}
		for _, g := range guardians {
			stillIn, err := guardianHasOtherChildInClass(tx, classID, g.UserID)
			if err != nil {
				return err
			}
			if stillIn {
				continue
			}
			var m model.ClassMember
			err = tx.Where("class_id = ? AND user_id = ?", classID, g.UserID).First(&m).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if m.Role == RoleAdmin {
				continue
			}
			if err := tx.Delete(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AddGuardianToChildClasses 把 user 加为孩子所在全部班级的 member；
// 监护关系建立（T03 接受邀请）时调用。
func (s *Store) AddGuardianToChildClasses(ctx context.Context, childID, userID uint64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var enrollments []model.ChildEnrollment
		if err := tx.Where("child_id = ?", childID).Find(&enrollments).Error; err != nil {
			return err
		}
		for _, e := range enrollments {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model.ClassMember{ClassID: e.ClassID, UserID: userID, Role: RoleMember}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) ListChildrenInClass(ctx context.Context, classID uint64, offset, limit int) ([]model.Child, int64, error) {
	var total int64
	if err := s.db.WithContext(ctx).Model(&model.ChildEnrollment{}).
		Where("class_id = ?", classID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var children []model.Child
	err := s.db.WithContext(ctx).
		Model(&model.Child{}).
		Select("children.*").
		Joins("JOIN child_enrollments ON child_enrollments.child_id = children.id").
		Where("child_enrollments.class_id = ?", classID).
		Order("children.id").
		Offset(offset).Limit(limit).
		Find(&children).Error
	return children, total, err
}

// addGuardiansAsMembers 把孩子全部监护人补为该班 member（已存在则保留原角色）。
func addGuardiansAsMembers(tx *gorm.DB, classID, childID uint64) error {
	var guardians []model.Guardianship
	if err := tx.Where("child_id = ?", childID).Find(&guardians).Error; err != nil {
		return err
	}
	for _, g := range guardians {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&model.ClassMember{ClassID: classID, UserID: g.UserID, Role: RoleMember}).Error; err != nil {
			return err
		}
	}
	return nil
}

func guardianHasOtherChildInClass(tx *gorm.DB, classID, userID uint64) (bool, error) {
	var count int64
	err := tx.Table("child_enrollments").
		Joins("JOIN guardianships ON guardianships.child_id = child_enrollments.child_id").
		Where("child_enrollments.class_id = ? AND guardianships.user_id = ?", classID, userID).
		Count(&count).Error
	return count > 0, err
}

// ---------- 加入申请 ----------

func (s *Store) PendingJoinRequest(ctx context.Context, classID, userID uint64) (*model.ClassJoinRequest, error) {
	var jr model.ClassJoinRequest
	err := s.db.WithContext(ctx).
		Where("class_id = ? AND user_id = ? AND status = ?", classID, userID, StatusPending).
		First(&jr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &jr, nil
}

func (s *Store) CreateJoinRequest(ctx context.Context, classID, userID uint64) (*model.ClassJoinRequest, error) {
	jr := model.ClassJoinRequest{ClassID: classID, UserID: userID, Status: StatusPending}
	if err := s.db.WithContext(ctx).Create(&jr).Error; err != nil {
		return nil, err
	}
	return &jr, nil
}

type JoinRequest struct {
	ID        uint64    `json:"id"`
	UserID    uint64    `json:"userId"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatarUrl"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) ListJoinRequests(ctx context.Context, classID uint64, status string) ([]JoinRequest, error) {
	q := s.db.WithContext(ctx).Table("class_join_requests").
		Select("class_join_requests.id, class_join_requests.user_id, users.wx_nickname AS nickname, users.avatar_url, class_join_requests.status, class_join_requests.created_at").
		Joins("JOIN users ON users.id = class_join_requests.user_id").
		Where("class_join_requests.class_id = ?", classID)
	if status != "" {
		q = q.Where("class_join_requests.status = ?", status)
	}
	var out []JoinRequest
	err := q.Order("class_join_requests.id").Scan(&out).Error
	return out, err
}

func (s *Store) GetJoinRequest(ctx context.Context, id uint64) (*model.ClassJoinRequest, error) {
	var jr model.ClassJoinRequest
	if err := s.db.WithContext(ctx).First(&jr, id).Error; err != nil {
		return nil, err
	}
	return &jr, nil
}

// DecideJoinRequest 审批：approve 时在同一事务内建立成员关系。
func (s *Store) DecideJoinRequest(ctx context.Context, id, decidedBy uint64, approve bool) (*model.ClassJoinRequest, error) {
	var jr model.ClassJoinRequest
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&jr, id).Error; err != nil {
			return err
		}
		if jr.Status != StatusPending {
			return ErrAlreadyDecided
		}
		status := StatusRejected
		if approve {
			status = StatusApproved
		}
		now := time.Now()
		if err := tx.Model(&jr).Updates(map[string]any{
			"status": status, "decided_by": decidedBy, "decided_at": now,
		}).Error; err != nil {
			return err
		}
		jr.Status = status
		jr.DecidedBy = &decidedBy
		jr.DecidedAt = &now
		if approve {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&model.ClassMember{ClassID: jr.ClassID, UserID: jr.UserID, Role: RoleMember}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &jr, nil
}

func generateInviteCode() (string, error) {
	b := make([]byte, 8)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b), nil
}
