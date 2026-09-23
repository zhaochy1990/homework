package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("TEST_MYSQL") == "" {
		t.Skip("set TEST_MYSQL=1 (and DB_*) to run against a real MySQL")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(cfg.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

// fakeIssuer 记录最后一次策略，返回固定临时密钥。
type fakeIssuer struct{ lastPolicy string }

func (f *fakeIssuer) Issue(_ context.Context, policy string, ttl time.Duration) (Credentials, error) {
	f.lastPolicy = policy
	return Credentials{
		TmpSecretID:  "tmp-id",
		TmpSecretKey: "tmp-key",
		SessionToken: "tmp-token",
		ExpiredTime:  time.Now().Add(ttl),
	}, nil
}

// fakeObjects 用内存 map 模拟私有桶：Put 模拟直传，未 Put 即视为不存在。
type fakeObjects struct {
	objects map[string]int64
	deleted []string
}

func newFakeObjects() *fakeObjects { return &fakeObjects{objects: map[string]int64{}} }

func (f *fakeObjects) Put(key string, size int64) { f.objects[key] = size }

func (f *fakeObjects) Head(_ context.Context, key string) (int64, error) {
	size, ok := f.objects[key]
	if !ok {
		return 0, ErrObjectNotFound
	}
	return size, nil
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	if _, ok := f.objects[key]; !ok {
		return ErrObjectNotFound
	}
	delete(f.objects, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeObjects) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://cos.example.com/" + key + "?sign=sig", nil
}

func newTestService(t *testing.T) (*Service, *Store, *fakeIssuer, *fakeObjects, *gorm.DB) {
	t.Helper()
	db := testDB(t)
	store := NewStore(db)
	issuer := &fakeIssuer{}
	objects := newFakeObjects()
	cfg := Config{Bucket: "homework-1250000000", Region: "ap-shanghai", AppID: "1250000000",
		STSTTL: 45 * time.Minute, PlaybackTTL: 2 * time.Hour}
	svc := NewWithDeps(cfg, store, issuer, objects)
	t.Cleanup(func() {
		db.Where("user_id = ?", testUserID).Delete(&model.UploadTicket{})
	})
	return svc, store, issuer, objects, db
}

const testUserID = uint64(990001)

func TestCreateTicketRestrictsPolicyAndPersists(t *testing.T) {
	svc, store, issuer, _, _ := newTestService(t)
	ctx := context.Background()

	ticket, err := svc.CreateTicket(ctx, testUserID, KindCheckinMedia, "video/mp4", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ticket.ObjectKey, UploadPrefix+fmt.Sprintf("%d/", testUserID)) {
		t.Fatalf("objectKey %q not under uploads/{uid}/", ticket.ObjectKey)
	}
	if ticket.STS.TmpSecretID == "" || ticket.STS.SessionToken == "" {
		t.Fatalf("sts credentials missing: %+v", ticket.STS)
	}
	// 会话策略必须存在、且只覆盖 uploads/ 前缀、不含读权限。
	if !strings.Contains(issuer.lastPolicy, "uploads/") || !strings.Contains(issuer.lastPolicy, ticket.ObjectKey) {
		t.Fatalf("policy not restricted to object key: %s", issuer.lastPolicy)
	}
	for _, forbidden := range []string{"cos:GetObject", "cos:DeleteObject", "uploads/*"} {
		if strings.Contains(issuer.lastPolicy, forbidden) {
			t.Fatalf("policy must not contain %q: %s", forbidden, issuer.lastPolicy)
		}
	}

	rec, err := store.Get(ctx, ticket.UploadID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != "pending" || rec.UserID != testUserID || rec.ObjectKey != ticket.ObjectKey {
		t.Fatalf("unexpected record: %+v", rec)
	}
}

func TestConfirmSuccessThenIdempotent(t *testing.T) {
	svc, store, _, objects, _ := newTestService(t)
	ctx := context.Background()

	ticket, err := svc.CreateTicket(ctx, testUserID, KindMaterialImage, "image/jpeg", 2048)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟客户端直传。
	objects.Put(ticket.ObjectKey, 2048)

	got, err := svc.Confirm(ctx, testUserID, ticket.UploadID, 2048, "etag-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ObjectKey != ticket.ObjectKey || got.PlaybackURL == "" {
		t.Fatalf("bad confirm result: %+v", got)
	}
	rec, _ := store.Get(ctx, ticket.UploadID)
	if rec.Status != "confirmed" || rec.ConfirmedAt == nil {
		t.Fatalf("record not confirmed: %+v", rec)
	}

	// 幂等：重复 confirm 仍成功。
	if _, err := svc.Confirm(ctx, testUserID, ticket.UploadID, 2048, "etag-1", ""); err != nil {
		t.Fatalf("idempotent confirm failed: %v", err)
	}
}

func TestConfirmMismatchKeepsPending(t *testing.T) {
	svc, store, _, objects, _ := newTestService(t)
	ctx := context.Background()

	// 分支一：对象根本不存在（客户端谎报）。
	missing, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/png", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, testUserID, missing.UploadID, 1024, "", ""); !errors.Is(err, ErrUploadMismatch) {
		t.Fatalf("missing object: err = %v, want ErrUploadMismatch", err)
	}
	if rec, _ := store.Get(ctx, missing.UploadID); rec.Status != "pending" {
		t.Fatalf("missing object should stay pending, got %s", rec.Status)
	}

	// 分支二：对象存在但大小不符（上传半途截断/谎报）。
	short, err := svc.CreateTicket(ctx, testUserID, KindThumb, "image/webp", 4096)
	if err != nil {
		t.Fatal(err)
	}
	objects.Put(short.ObjectKey, 1000)
	if _, err := svc.Confirm(ctx, testUserID, short.UploadID, 4096, "", ""); !errors.Is(err, ErrUploadMismatch) {
		t.Fatalf("size mismatch: err = %v, want ErrUploadMismatch", err)
	}
}

func TestConfirmIsolatesUsersAndUnknown(t *testing.T) {
	svc, _, _, _, _ := newTestService(t)
	ctx := context.Background()

	ticket, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/jpeg", 512)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, testUserID+1, ticket.UploadID, 512, "", ""); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("other user: err = %v, want ErrTicketNotFound", err)
	}
	if _, err := svc.Confirm(ctx, testUserID, "does-not-exist", 512, "", ""); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("unknown ticket: err = %v, want ErrTicketNotFound", err)
	}
}

func TestConfirmRejectsExpiredTicket(t *testing.T) {
	svc, _, _, objects, db := newTestService(t)
	ctx := context.Background()

	ticket, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/jpeg", 100)
	if err != nil {
		t.Fatal(err)
	}
	objects.Put(ticket.ObjectKey, 100)
	// 让凭证过期（模拟长时间后才 confirm）。
	if err := db.Model(&model.UploadTicket{}).Where("upload_id = ?", ticket.UploadID).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, testUserID, ticket.UploadID, 100, "", ""); !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("expired confirm: err = %v, want ErrTicketNotFound", err)
	}
}

func TestCleanupOrphansDeletesOnlyExpiredPending(t *testing.T) {
	svc, store, _, objects, db := newTestService(t)
	ctx := context.Background()

	// 过期 pending：应被清理。
	orphan, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/jpeg", 100)
	if err != nil {
		t.Fatal(err)
	}
	objects.Put(orphan.ObjectKey, 100)
	// 过期但已 confirmed：不应被清理。
	done, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/jpeg", 200)
	if err != nil {
		t.Fatal(err)
	}
	objects.Put(done.ObjectKey, 200)
	if _, err := svc.Confirm(ctx, testUserID, done.UploadID, 200, "", ""); err != nil {
		t.Fatal(err)
	}
	// 未过期的 pending：不应被清理。
	fresh, err := svc.CreateTicket(ctx, testUserID, KindAvatar, "image/jpeg", 300)
	if err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-time.Hour)
	db.Model(&model.UploadTicket{}).Where("upload_id IN ?", []string{orphan.UploadID, done.UploadID}).
		Update("expires_at", past)

	n, err := svc.CleanupOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cleaned = %d, want 1", n)
	}
	if _, ok := objects.objects[orphan.ObjectKey]; ok {
		t.Fatal("orphan object should be deleted from COS")
	}
	if _, ok := objects.objects[done.ObjectKey]; !ok {
		t.Fatal("confirmed object must survive cleanup")
	}
	if rec, _ := store.Get(ctx, orphan.UploadID); rec.Status != "cleaned" {
		t.Fatalf("orphan status = %s, want cleaned", rec.Status)
	}
	if rec, _ := store.Get(ctx, done.UploadID); rec.Status != "confirmed" {
		t.Fatalf("confirmed status = %s, want confirmed", rec.Status)
	}
	if rec, _ := store.Get(ctx, fresh.UploadID); rec.Status != "pending" {
		t.Fatalf("fresh status = %s, want pending", rec.Status)
	}
}

func TestValidateTicket(t *testing.T) {
	ok := []struct {
		kind, ct string
		size     int64
	}{
		{KindMaterialImage, "image/png", 1},
		{KindMaterialVideo, "video/mp4", 1 << 20},
		{KindCheckinMedia, "image/jpeg", 1},
		{KindCheckinMedia, "video/mp4", 1},
		{KindThumb, "image/webp", 1},
		{KindAvatar, "image/gif", MaxImageBytes},
	}
	for _, c := range ok {
		if _, err := validateTicket(c.kind, c.ct, c.size); err != nil {
			t.Errorf("validateTicket(%s,%s,%d) = %v, want nil", c.kind, c.ct, c.size, err)
		}
	}
	bad := []struct {
		kind, ct string
		size     int64
	}{
		{"unknown_kind", "image/png", 1},
		{KindMaterialImage, "video/mp4", 1},                 // 图片 kind 不能传视频
		{KindMaterialVideo, "image/png", 1},                 // 视频 kind 不能传图
		{KindAvatar, "application/pdf", 1},                  // 非白名单类型
		{KindAvatar, "image/png", 0},                        // 大小必须为正
		{KindAvatar, "image/png", -1},                       // 负数
		{KindMaterialImage, "image/png", MaxImageBytes + 1}, // 图片超限
		{KindMaterialVideo, "video/mp4", MaxVideoBytes + 1}, // 视频超限
	}
	for _, c := range bad {
		if _, err := validateTicket(c.kind, c.ct, c.size); err == nil {
			t.Errorf("validateTicket(%s,%s,%d) = nil, want error", c.kind, c.ct, c.size)
		}
	}
}
