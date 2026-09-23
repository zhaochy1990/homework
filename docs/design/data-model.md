# 数据模型与状态机 v1

> 产出自 wayfinder ticket「Grilling: 数据模型与状态机」。上游依据：`CONTEXT.md`（术语）、`docs/adr/0002`（班级共享/孩子私有）、`docs/adr/0003`（todo id 不可变）、`docs/adr/0004`（streak 履约模型）。

## 总览

MySQL 8，GORM。所有"日期"均为 Asia/Shanghai 日历日期（`DATE` 类型）；时间戳一律 UTC（`DATETIME(3)` 或 `TIMESTAMP`）。时区硬编码 CST，无海外需求。

所有 `subject` 字段取同一份**全局封闭枚举**（ADR 0007）：语文/数学/英语/物理/化学/生物/历史/地理/道法/科学/体育/艺术/其他。以字符串存储、由代码内唯一常量表约束，不存在"班级自维护学科集合"这回事。

## 表定义（GORM 形态）

### 身份与家庭

```go
// 用户——由 STRIDE JWT 驱动，首次请求 upsert，昵称头像随登录刷新
type User struct {
    ID           uint64  `gorm:"primaryKey"`
    StrideUserID string  `gorm:"column:stride_user_id;uniqueIndex;size:64"` // JWT sub
    WxNickname   string  `gorm:"size:64"`
    AvatarURL    string  `gorm:"size:512"`
    CreatedAt    time.Time
}

// 孩子——打卡与进度的主体
type Child struct {
    ID        uint64  `gorm:"primaryKey"`
    Name      string  `gorm:"size:64"`
    AvatarURL string  `gorm:"size:512"` // 可空
    CreatedBy uint64  // users.id
    CreatedAt time.Time
}

// 监护关系——多监护人同权
type Guardianship struct {
    ID      uint64 `gorm:"primaryKey"`
    ChildID uint64 `gorm:"uniqueIndex:uk_child_user"`
    UserID  uint64 `gorm:"uniqueIndex:uk_child_user"`
}
```

### 班级

```go
type Class struct {
    ID           uint64 `gorm:"primaryKey"`
    Name         string `gorm:"size:64"`
    Visibility   string `gorm:"size:10;default:private"` // public | private
    JoinApproval bool   `gorm:"default:false"`           // 公开班加入是否需 admin 审批（私密班凭邀请码绕过）
    InviteCode   string `gorm:"uniqueIndex;size:16"`     // 私密班扫码/链接加入
    CreatedBy    uint64
    CreatedAt    time.Time
}

// 公开班的加入申请（join_approval=true 时）；无审批的加入自动 approved 并直接建 member
type ClassJoinRequest struct {
    ID        uint64 `gorm:"primaryKey"`
    ClassID   uint64 `gorm:"index"`
    UserID    uint64
    Status    string `gorm:"size:10;default:pending"` // pending | approved | rejected
    CreatedAt time.Time
    DecidedBy *uint64
    DecidedAt *time.Time
}
// unique index uk_class_user_pending (class_id, user_id) WHERE status='pending'
// —— GORM 用应用层校验防重复 pending 申请

type ClassMember struct {
    ID      uint64 `gorm:"primaryKey"`
    ClassID uint64 `gorm:"uniqueIndex:uk_class_user"`
    UserID  uint64 `gorm:"uniqueIndex:uk_class_user"`
    Role    string `gorm:"size:10;default:member"` // admin | member
}

// 孩子入班；孩子入班 => 其全部监护人自动获得该班 member 权限（应用层保证）
type ChildEnrollment struct {
    ID      uint64 `gorm:"primaryKey"`
    ChildID uint64 `gorm:"uniqueIndex:uk_child_class"`
    ClassID uint64 `gorm:"uniqueIndex:uk_child_class"`
}
```

### 教材库（全局）与资料

```go
// 全局教材库：任何用户可添加，v1 无审核
type Textbook struct {
    ID        uint64 `gorm:"primaryKey"`
    Subject   string `gorm:"size:32;index"`
    Name      string `gorm:"size:128"`
    Grade     string `gorm:"size:32"` // 可空，如"三年级"
    Term      string `gorm:"size:16"` // 上/下册，可空
    CreatedBy uint64
    CreatedAt time.Time
}

type TextbookUnit struct {
    ID         uint64 `gorm:"primaryKey"`
    TextbookID uint64 `gorm:"index"`
    Name       string `gorm:"size:64"` // "Unit 2"
    SortOrder  int
}

// 班级按科目选用教材
type ClassTextbook struct {
    ID         uint64 `gorm:"primaryKey"`
    ClassID    uint64 `gorm:"uniqueIndex:uk_class_subject"`
    Subject    string `gorm:"uniqueIndex:uk_class_subject;size:32"`
    TextbookID uint64
}

// 学习资料——班级共享；先审后显
type Material struct {
    ID         uint64 `gorm:"primaryKey"`
    ClassID    uint64 `gorm:"index"`
    UnitID     *uint64 // 可空：未关联单元
    Type       string `gorm:"size:10"` // text | image | video
    Title      string `gorm:"size:128"`
    Body       string `gorm:"type:text"` // type=text 时必填
    CosKey     string `gorm:"size:512"`  // image/video 的对象键
    SizeBytes  int64
    SecStatus  string `gorm:"size:10;default:pending"` // pending | pass | blocked
    UploadedBy uint64
    CreatedAt  time.Time
}
```

### 作业

```go
// 作业 Session：kind=day 唯一键 (class,subject,date)；kind=holiday 唯一键 (class,subject,start_date)
type HomeworkSession struct {
    ID          uint64 `gorm:"primaryKey"`
    ClassID     uint64 `gorm:"index"`
    Subject     string `gorm:"size:32"`
    Kind        string `gorm:"size:10"` // day | holiday
    Date        *time.Time     // kind=day
    StartDate   *time.Time     // kind=holiday：连续休息日首日
    EndDate     *time.Time     // kind=holiday：末日（履约截止）
    HolidayName string `gorm:"size:32"` // "国庆"
    RawText     string `gorm:"type:text"` // 管理员粘贴的老师原文，原样保留
    Notes       string `gorm:"type:text"` // "老师还提到"：解析出的 unparsed 拍平后可编辑
    CreatedBy   uint64
    CreatedAt   time.Time
}
// 履约截止：kind=day => Date 当日 23:59:59 CST；kind=holiday => EndDate 当日 23:59:59 CST

// 待办——id 永不变更（ADR 0003），公开暴露 ULID
type Todo struct {
    ID               string `gorm:"primaryKey;size:26"` // ULID
    SessionID        uint64 `gorm:"index"`
    Content          string `gorm:"size:512"` // 编辑只改这里
    EstimatedMinutes *int   // 大模型估算的完成时长；NULL = 未估（手填路径一律 NULL），0/负数非法
    SortOrder        int
    CreatedAt        time.Time
}

// 勾选——孩子私有（ADR 0002）；打卡后冻结
type TodoTick struct {
    ID       uint64 `gorm:"primaryKey"`
    TodoID   string `gorm:"uniqueIndex:uk_todo_child;size:26"`
    ChildID  uint64 `gorm:"uniqueIndex:uk_todo_child"`
    TickedAt time.Time
}
```

### 打卡

```go
// 打卡——session 级、孩子维度、终态不可撤销
type Checkin struct {
    ID          uint64 `gorm:"primaryKey"`
    SessionID   uint64 `gorm:"uniqueIndex:uk_session_child"`
    ChildID     uint64 `gorm:"uniqueIndex:uk_session_child"`
    CheckinDate time.Time `gorm:"type:date"` // 实际打卡的 CST 日期（补卡即本日）
    CheckedInAt time.Time            // UTC 时间戳
    Note        string `gorm:"size:512"`  // 家长一句话（打卡动态文字，可空）
}

// 打卡附件——照片/视频混存，合计 ≤9，随打卡一起发布（无独立入口）
type CheckinMedia struct {
    ID          uint64 `gorm:"primaryKey"`
    CheckinID   uint64 `gorm:"index"`
    Type        string `gorm:"size:10"`  // image | video
    CosKey      string `gorm:"size:512"`
    ThumbKey    string `gorm:"size:512"` // 视频封面（chooseMedia thumbTempFilePath）
    SizeBytes   int64
    DurationSec int    // video 专用
    SecStatus   string `gorm:"size:10;default:pending"` // pending | pass | blocked（先审后显）
}

// 打卡卡片——异步生成
type CheckinCard struct {
    ID          uint64 `gorm:"primaryKey"`
    CheckinID   uint64 `gorm:"uniqueIndex"`
    ImageCosKey string `gorm:"size:512"`
    GeneratedAt time.Time
}
```

### 日历与无作业日

```go
// 上学日历种子表——research/005 产出，verified=false 时不得参与上线逻辑
type SchoolCalendar struct {
    Date     time.Time `gorm:"primaryKey;type:date"`
    Type     string    `gorm:"size:20"` // holiday | adjusted_workday
    Name     string    `gorm:"size:32"`
    Verified bool      `gorm:"default:false"`
}

type NoHomeworkDay struct {
    ID        uint64 `gorm:"primaryKey"`
    ClassID   uint64 `gorm:"uniqueIndex:uk_class_date"`
    Date      time.Time `gorm:"uniqueIndex:uk_class_date;type:date"`
    CreatedBy uint64
    CreatedAt time.Time
}
```

### Streak

```go
// 事件驱动计数器（Q3=A）：打卡/无作业日事务内 +1，同日同科目只加一次
type Streak struct {
    ID             uint64 `gorm:"primaryKey"`
    ChildID        uint64 `gorm:"uniqueIndex:uk_child_subject"`
    Subject        string `gorm:"uniqueIndex:uk_child_subject;size:32"`
    CurrentCount   int
    LastFulfillDate *time.Time `gorm:"type:date"`
    UpdatedAt      time.Time
}
// 中断检测：惰性（读写时检查过期未履约义务）+ 每日校准任务对账
// 中断 => CurrentCount=0，从下一次履约重新计数
```

## 履约与 streak 计算口径（实现规范）

**履约义务（Obligation）**：一条 Session 对孩子 c、科目 s 构成义务，当且仅当：c 在该班（enrollment）∧ 截止时间未过 ∧ **session.created_at 早于截止时间**（迟建不追溯断签）。

**+1 事件**（事务内执行，同 `(child, subject, date)` 幂等）：
1. 打卡成功 → +1，落在 `checkin.checkin_date`（补卡计实际打卡日）；
2. 无作业日（班级级）→ 该班 enrolled 孩子的**每个该班科目** +1，落在标记日。

**中断**：任一义务过期（截止 < now）且该孩子未打卡 → 该 `(child, subject)` 计数归零。同日同科目多个义务/事件只 +1 一次（跨班同科目合并计一天）。

**冻结**：无义务的休息日不加不断。无作业日撤销不回溯已发放的 +1。

## 操作影响矩阵

| # | 操作 | 勾选 | 打卡 | streak | 卡片 |
|---|---|---|---|---|---|
| 1 | admin 编辑 todo 内容/预估时长 | 不变（id 稳定） | 不变 | 不变 | 不变（历史卡片不重绘） |
| 2 | admin 新增 todo | 新 todo 全员未勾 | 已打卡者不受影响 | 不变 | 不变 |
| 3 | admin 删除未勾选的 todo | 直接删 | — | 不变 | — |
| 4 | admin 删除已勾选的 todo（二次确认） | 勾选级联清除 | 不变 | 不变 | 不变 |
| 5 | admin 删除 Session | 有任何打卡→禁止；无打卡→级联清 todo+勾选 | — | 不变 | 不存在 |
| 6 | 补录过去日期的 Session | 正常勾 | 正常打卡，+1 计实际打卡日 | 不断签，新 streak 从 1 起 | 正常生成 |
| 7 | 标记无作业日（当日无 Session 才可标） | — | 禁止发作业 | 当日各科目 +1 | — |
| 8 | 撤销无作业日 | — | — | 已 +1 不回溯 | — |
| 9 | 孩子退班 | 勾选/打卡历史保留不再展示 | streak 冻结保留 | 该班科目不再产生义务 | 历史卡片保留 |
| 10 | 移除监护人 | 数据保留 | 失去该孩子全部访问权 | — | — |

补卡遵守"同日同科目只 +1 一次"：补打历史 session 与当日 session 同科同日，只 +1。

## 状态机

**媒体审核**（`materials.sec_status` / `checkin_media.sec_status`，先审后显）：

```
文本:  创建时 msgSecCheck 同步检测 → pass | blocked
媒体:  created → pending ──(wxa_media_check 消息推送回调, trace_id 幂等)──→ pass | blocked
       pending / blocked 对班级成员不可见；blocked 为终态；racy 内容 v1 从严按 blocked
       打卡附件逐个附件独立审核（仅监护人可见，审核不通过时对该用户隐藏该附件）
```

**打卡**：`created` 即终态（唯一约束 `session_id+child_id` 兜底不可重打）。

**Session**：`created` →（todo 可随时编辑，见矩阵 #1–4）→ 仅无任何打卡时可删除。

## 实现细节备忘

- 打卡后该 session 的勾选记录冻结：已打卡孩子的 `todo_ticks` 不再接受变更；admin 后加的 todo 对其显示"新增未完成"，不影响打卡与 streak。
- 卡片在打卡事务提交后异步生成（Go image 库绘制 → 上传 COS → 写 `checkin_cards`），失败重试。
- todo 主键 ULID：时间有序、可安全公开；其余表 bigint 自增。
- `school_calendar.verified=false` 时仅记录、不驱动 Session 合并与 streak 冻结判定（上线前须人工核验，见 research/005）。
- 孩子入班 → 监护人自动 member；退班 → 权限随之消失（应用层在 enrollment 变更时重建 class_members）。
