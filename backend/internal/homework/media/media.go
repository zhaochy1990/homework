package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

// 5 种上传 kind（api-contract §3.5）。
const (
	KindMaterialImage = "material_image"
	KindMaterialVideo = "material_video"
	KindCheckinMedia  = "checkin_media"
	KindThumb         = "thumb"
	KindAvatar        = "avatar"
)

// 文件大小上限（图片 10MB / 视频 200MB）。
const (
	MaxImageBytes = 10 << 20
	MaxVideoBytes = 200 << 20
)

var (
	ErrInvalidKind        = errors.New("media: 不支持的 kind")
	ErrInvalidContentType = errors.New("media: 不支持的 contentType")
	ErrSizeOutOfRange     = errors.New("media: 文件大小超限")
	ErrTicketNotFound     = errors.New("media: 上传凭证不存在")
	ErrUploadMismatch     = errors.New("media: 上传对象与上报不符")
	ErrObjectNotFound     = errors.New("media: COS 对象不存在")
)

// extByContentType 是允许上传的 MIME 白名单与其扩展名。
var extByContentType = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/x-m4v":     ".m4v",
	"video/3gpp":      ".3gp",
}

// Credentials 是 STS 临时密钥（api-contract §3.5 的 sts 字段）。
type Credentials struct {
	TmpSecretID  string
	TmpSecretKey string
	SessionToken string
	ExpiredTime  time.Time
}

// Issuer 换取受限 STS 临时密钥；生产实现见 tencent.go。
type Issuer interface {
	Issue(ctx context.Context, policy string, ttl time.Duration) (Credentials, error)
}

// Objects 是 confirm 校验、播放签发与孤儿清理所需的 COS 对象操作。
type Objects interface {
	// Head 返回对象大小；对象不存在时返回 ErrObjectNotFound。
	Head(ctx context.Context, key string) (int64, error)
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Config 是媒体链路的 COS/STS 配置。
type Config struct {
	Bucket      string
	Region      string
	AppID       string
	SecretID    string
	SecretKey   string
	STSTTL      time.Duration
	PlaybackTTL time.Duration
}

// Ticket 是 upload-tickets 响应内容。
type Ticket struct {
	UploadID  string
	ObjectKey string
	Bucket    string
	Region    string
	STS       Credentials
}

// Confirmed 是 confirm 响应内容。
type Confirmed struct {
	ObjectKey   string
	PlaybackURL string
	ContentType string
	SizeBytes   int64
}

// Service 编排签发 → confirm → 清理。
type Service struct {
	cfg     Config
	store   *Store
	issuer  Issuer
	objects Objects
	now     func() time.Time
}

// NewService 用真实腾讯云适配器构造服务；COS 未配置时返回 (nil, nil)，媒体端点降级不可用。
func NewService(cfg Config, db *gorm.DB) (*Service, error) {
	if cfg.Bucket == "" || cfg.Region == "" || cfg.SecretID == "" || cfg.SecretKey == "" {
		return nil, nil
	}
	issuer, err := newTencentIssuer(cfg)
	if err != nil {
		return nil, err
	}
	objects, err := newCOSObjects(cfg)
	if err != nil {
		return nil, err
	}
	return NewWithDeps(cfg, NewStore(db), issuer, objects), nil
}

// NewWithDeps 用注入的依赖构造服务（测试/自定义适配器）。
func NewWithDeps(cfg Config, store *Store, issuer Issuer, objects Objects) *Service {
	if cfg.STSTTL <= 0 {
		cfg.STSTTL = 45 * time.Minute
	}
	if cfg.PlaybackTTL <= 0 {
		cfg.PlaybackTTL = 2 * time.Hour
	}
	return &Service{cfg: cfg, store: store, issuer: issuer, objects: objects, now: time.Now}
}

// CreateTicket 校验入参、生成对象键与受限策略、换取 STS 凭证并落一条 pending 记录。
func (s *Service) CreateTicket(ctx context.Context, userID uint64, kind, contentType string, sizeBytes int64) (*Ticket, error) {
	ext, err := validateTicket(kind, contentType, sizeBytes)
	if err != nil {
		return nil, err
	}
	uploadID, err := randomID()
	if err != nil {
		return nil, err
	}
	objectKey := fmt.Sprintf("%s%d/%s/%s%s", UploadPrefix, userID, kind, uploadID, ext)
	policy, err := BuildWritePolicy(s.cfg.Region, s.cfg.AppID, s.cfg.Bucket, objectKey)
	if err != nil {
		return nil, err
	}
	creds, err := s.issuer.Issue(ctx, policy, s.cfg.STSTTL)
	if err != nil {
		return nil, err
	}
	rec := &model.UploadTicket{
		UploadID:    uploadID,
		UserID:      userID,
		Kind:        kind,
		ObjectKey:   objectKey,
		ContentType: contentType,
		SizeBytes:   sizeBytes,
		Bucket:      s.cfg.Bucket,
		Region:      s.cfg.Region,
		Status:      "pending",
		ExpiresAt:   s.now().Add(s.cfg.STSTTL),
	}
	if err := s.store.Create(ctx, rec); err != nil {
		return nil, err
	}
	return &Ticket{UploadID: uploadID, ObjectKey: objectKey, Bucket: rec.Bucket, Region: rec.Region, STS: creds}, nil
}

// Confirm 对已上传对象做 HEAD 校验（存在 + 大小一致），落 confirmed 并签发播放 URL。
// 幂等：重复 confirm 同一 ticket 只重签播放 URL。
func (s *Service) Confirm(ctx context.Context, userID uint64, uploadID string, sizeBytes int64, etag, thumbObjectKey string) (*Confirmed, error) {
	rec, err := s.store.Get(ctx, uploadID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTicketNotFound
	}
	if err != nil {
		return nil, err
	}
	if rec.UserID != userID || rec.Status == "cleaned" {
		return nil, ErrTicketNotFound
	}
	// 凭证已过期：拒绝 confirm。同时收窄与孤儿清理并发删除同一对象的窗口。
	if s.now().After(rec.ExpiresAt) {
		return nil, ErrTicketNotFound
	}
	size, err := s.objects.Head(ctx, rec.ObjectKey)
	if errors.Is(err, ErrObjectNotFound) {
		return nil, ErrUploadMismatch
	}
	if err != nil {
		return nil, err
	}
	if size != sizeBytes {
		return nil, ErrUploadMismatch
	}
	if rec.Status != "confirmed" {
		if err := s.store.Confirm(ctx, rec.ID, s.now()); err != nil {
			return nil, err
		}
	}
	playback, err := s.objects.PresignGet(ctx, rec.ObjectKey, s.cfg.PlaybackTTL)
	if err != nil {
		return nil, err
	}
	return &Confirmed{ObjectKey: rec.ObjectKey, PlaybackURL: playback, ContentType: rec.ContentType, SizeBytes: size}, nil
}

// CleanupOrphans 删除超时未 confirm 的 pending 对象并置 cleaned，返回清理条数。
//
// ponytail: 与 Confirm 共享对象时靠 ExpiresAt 先决条件裁剪窗口，极小概率下仍可能在 Head 后
// 被本任务删除；v1 接受（需真正消除则应改为先原子 claim status 再删对象）。
func (s *Service) CleanupOrphans(ctx context.Context) (int, error) {
	expired, err := s.store.ListExpiredPending(ctx, s.now())
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for i := range expired {
		rec := &expired[i]
		if err := s.objects.Delete(ctx, rec.ObjectKey); err != nil && !errors.Is(err, ErrObjectNotFound) {
			return cleaned, err
		}
		if err := s.store.MarkCleaned(ctx, rec.ID); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

// RunCleanup 先立即清理一次（覆盖重启前的孤儿），之后每 interval 运行，直到 ctx 取消。
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration) {
	run := func() {
		if n, err := s.CleanupOrphans(ctx); err != nil {
			slog.Error("media cleanup failed", "err", err)
		} else if n > 0 {
			slog.Info("media cleanup done", "cleaned", n)
		}
	}
	run()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		}
	}
}

// validateTicket 返回扩展名；kind/contentType/size 任一不合法即报错。
func validateTicket(kind, contentType string, sizeBytes int64) (string, error) {
	if _, ok := familyOf(kind); !ok {
		return "", ErrInvalidKind
	}
	ext, ok := extByContentType[contentType]
	if !ok {
		return "", ErrInvalidContentType
	}
	isImage := strings.HasPrefix(contentType, "image/")
	kindImage, _ := familyOf(kind)
	if kind != KindCheckinMedia && kindImage != isImage {
		return "", ErrInvalidContentType
	}
	if sizeBytes <= 0 {
		return "", ErrSizeOutOfRange
	}
	limit := int64(MaxVideoBytes)
	if isImage {
		limit = MaxImageBytes
	}
	if sizeBytes > limit {
		return "", ErrSizeOutOfRange
	}
	return ext, nil
}

// familyOf 返回 kind 是否属于图片族；ok=false 表示未知 kind。
func familyOf(kind string) (image, ok bool) {
	switch kind {
	case KindMaterialImage, KindThumb, KindAvatar:
		return true, true
	case KindMaterialVideo:
		return false, true
	case KindCheckinMedia:
		return false, true // 图片/视频皆可，由 contentType 判定
	default:
		return false, false
	}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
