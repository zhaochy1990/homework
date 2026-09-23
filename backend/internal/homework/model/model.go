// Package model 定义 v1 全部 GORM 实体（见 docs/design/data-model.md）。
//
// 约定：
//   - 主键除 todos（ULID 字符串）外均为 bigint 自增；
//   - 时间戳存 UTC，日期列存 CST 日历日（应用层以 UTC 零点表示，见 data-model）；
//   - subject 取值由代码内全局封闭枚举约束（ADR 0007），不在此包校验。
package model

import "time"

// ---------- 身份与家庭 ----------

type User struct {
	ID           uint64 `gorm:"primaryKey"`
	StrideUserID string `gorm:"column:stride_user_id;uniqueIndex;size:64"` // JWT sub
	WxNickname   string `gorm:"size:64"`
	AvatarURL    string `gorm:"size:512"`
	CreatedAt    time.Time
}

func (User) TableName() string { return "users" }

type Child struct {
	ID        uint64 `gorm:"primaryKey"`
	Name      string `gorm:"size:64"`
	AvatarURL string `gorm:"size:512"`
	CreatedBy uint64
	CreatedAt time.Time
}

func (Child) TableName() string { return "children" }

type Guardianship struct {
	ID      uint64 `gorm:"primaryKey"`
	ChildID uint64 `gorm:"uniqueIndex:uk_child_user"`
	UserID  uint64 `gorm:"uniqueIndex:uk_child_user"`
}

func (Guardianship) TableName() string { return "guardianships" }

// ---------- 班级 ----------

type Class struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string `gorm:"size:64"`
	Visibility   string `gorm:"size:10;default:private"` // public | private
	JoinApproval bool   `gorm:"default:false"`
	InviteCode   string `gorm:"uniqueIndex;size:16"`
	CreatedBy    uint64
	CreatedAt    time.Time
}

func (Class) TableName() string { return "classes" }

// ClassJoinRequest 公开班的加入申请。防重复 pending 的唯一约束由应用层保证
// （data-model 备注的 partial unique index MySQL 无法用 GORM tag 表达）。
type ClassJoinRequest struct {
	ID        uint64 `gorm:"primaryKey"`
	ClassID   uint64 `gorm:"index"`
	UserID    uint64
	Status    string `gorm:"size:10;default:pending"` // pending | approved | rejected
	CreatedAt time.Time
	DecidedBy *uint64
	DecidedAt *time.Time
}

func (ClassJoinRequest) TableName() string { return "class_join_requests" }

type ClassMember struct {
	ID      uint64 `gorm:"primaryKey"`
	ClassID uint64 `gorm:"uniqueIndex:uk_class_user"`
	UserID  uint64 `gorm:"uniqueIndex:uk_class_user"`
	Role    string `gorm:"size:10;default:member"` // admin | member
}

func (ClassMember) TableName() string { return "class_members" }

type ChildEnrollment struct {
	ID      uint64 `gorm:"primaryKey"`
	ChildID uint64 `gorm:"uniqueIndex:uk_child_class"`
	ClassID uint64 `gorm:"uniqueIndex:uk_child_class"`
}

func (ChildEnrollment) TableName() string { return "child_enrollments" }

// ---------- 教材库与资料 ----------

type Textbook struct {
	ID        uint64 `gorm:"primaryKey"`
	Subject   string `gorm:"size:32;index"`
	Name      string `gorm:"size:128"`
	Grade     string `gorm:"size:32"`
	Term      string `gorm:"size:16"`
	CreatedBy uint64
	CreatedAt time.Time
}

func (Textbook) TableName() string { return "textbooks" }

type TextbookUnit struct {
	ID         uint64 `gorm:"primaryKey"`
	TextbookID uint64 `gorm:"index"`
	Name       string `gorm:"size:64"`
	SortOrder  int
}

func (TextbookUnit) TableName() string { return "textbook_units" }

type ClassTextbook struct {
	ID         uint64 `gorm:"primaryKey"`
	ClassID    uint64 `gorm:"uniqueIndex:uk_class_subject"`
	Subject    string `gorm:"uniqueIndex:uk_class_subject;size:32"`
	TextbookID uint64
}

func (ClassTextbook) TableName() string { return "class_textbooks" }

type Material struct {
	ID         uint64 `gorm:"primaryKey"`
	ClassID    uint64 `gorm:"index"`
	UnitID     *uint64
	Type       string `gorm:"size:10"` // text | image | video
	Title      string `gorm:"size:128"`
	Body       string `gorm:"type:text"`
	CosKey     string `gorm:"size:512"`
	SizeBytes  int64
	SecStatus  string `gorm:"size:10;default:pending"` // pending | pass | blocked
	UploadedBy uint64
	CreatedAt  time.Time
}

func (Material) TableName() string { return "materials" }

// ---------- 作业 ----------

type HomeworkSession struct {
	ID          uint64 `gorm:"primaryKey"`
	ClassID     uint64 `gorm:"index"`
	Subject     string `gorm:"size:32"`
	Kind        string `gorm:"size:10"` // day | holiday
	Date        *time.Time
	StartDate   *time.Time
	EndDate     *time.Time
	HolidayName string `gorm:"size:32"`
	RawText     string `gorm:"type:text"`
	Notes       string `gorm:"type:text"`
	CreatedBy   uint64
	CreatedAt   time.Time
}

func (HomeworkSession) TableName() string { return "homework_sessions" }

// Todo 主键为公开 ULID，id 永不变更（ADR 0003）。
type Todo struct {
	ID               string `gorm:"primaryKey;size:26"`
	SessionID        uint64 `gorm:"index"`
	Content          string `gorm:"size:512"`
	EstimatedMinutes *int
	SortOrder        int
	CreatedAt        time.Time
}

func (Todo) TableName() string { return "todos" }

type TodoTick struct {
	ID       uint64 `gorm:"primaryKey"`
	TodoID   string `gorm:"uniqueIndex:uk_todo_child;size:26"`
	ChildID  uint64 `gorm:"uniqueIndex:uk_todo_child"`
	TickedAt time.Time
}

func (TodoTick) TableName() string { return "todo_ticks" }

// ---------- 打卡 ----------

type Checkin struct {
	ID          uint64    `gorm:"primaryKey"`
	SessionID   uint64    `gorm:"uniqueIndex:uk_session_child"`
	ChildID     uint64    `gorm:"uniqueIndex:uk_session_child"`
	CheckinDate time.Time `gorm:"type:date"`
	CheckedInAt time.Time
	Note        string `gorm:"size:512"`
}

func (Checkin) TableName() string { return "checkins" }

type CheckinMedia struct {
	ID          uint64 `gorm:"primaryKey"`
	CheckinID   uint64 `gorm:"index"`
	Type        string `gorm:"size:10"` // image | video
	CosKey      string `gorm:"size:512"`
	ThumbKey    string `gorm:"size:512"`
	SizeBytes   int64
	DurationSec int
	SecStatus   string `gorm:"size:10;default:pending"` // pending | pass | blocked
}

func (CheckinMedia) TableName() string { return "checkin_media" }

type CheckinCard struct {
	ID          uint64 `gorm:"primaryKey"`
	CheckinID   uint64 `gorm:"uniqueIndex"`
	ImageCosKey string `gorm:"size:512"`
	GeneratedAt time.Time
}

func (CheckinCard) TableName() string { return "checkin_cards" }

// ---------- 日历与无作业日 ----------

type SchoolCalendar struct {
	Date     time.Time `gorm:"primaryKey;type:date"`
	Type     string    `gorm:"size:20"` // holiday | adjusted_workday
	Name     string    `gorm:"size:32"`
	Verified bool      `gorm:"default:false"`
}

func (SchoolCalendar) TableName() string { return "school_calendar" }

type NoHomeworkDay struct {
	ID        uint64    `gorm:"primaryKey"`
	ClassID   uint64    `gorm:"uniqueIndex:uk_class_date"`
	Date      time.Time `gorm:"uniqueIndex:uk_class_date;type:date"`
	CreatedBy uint64
	CreatedAt time.Time
}

func (NoHomeworkDay) TableName() string { return "no_homework_days" }

// ---------- Streak ----------

type Streak struct {
	ID              uint64 `gorm:"primaryKey"`
	ChildID         uint64 `gorm:"uniqueIndex:uk_child_subject"`
	Subject         string `gorm:"uniqueIndex:uk_child_subject;size:32"`
	CurrentCount    int
	LastFulfillDate *time.Time `gorm:"type:date"`
	UpdatedAt       time.Time
}

func (Streak) TableName() string { return "streaks" }

// All 返回全部待建表实体，供 AutoMigrate 使用。
func All() []any {
	return []any{
		&User{}, &Child{}, &Guardianship{},
		&Class{}, &ClassJoinRequest{}, &ClassMember{}, &ChildEnrollment{},
		&Textbook{}, &TextbookUnit{}, &ClassTextbook{}, &Material{},
		&HomeworkSession{}, &Todo{}, &TodoTick{},
		&Checkin{}, &CheckinMedia{}, &CheckinCard{},
		&SchoolCalendar{}, &NoHomeworkDay{},
		&Streak{},
	}
}
