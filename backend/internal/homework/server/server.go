// Package server 组装路由与中间件。
package server

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	cal "github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/class"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/family"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/media"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/textbook"
	"github.com/zhaochy1990/homework/backend/internal/homework/user"
	"github.com/zhaochy1990/homework/backend/internal/homework/wechat"
	"gorm.io/gorm"
)

type Server struct {
	cfg       *config.Config
	db        *gorm.DB
	users     *user.Store
	family    *family.Store
	classes   *class.Store
	calendar  *cal.Store
	invites   *family.InviteSigner
	wechat    *wechat.Client
	media     *media.Service
	textbooks *textbook.Store
}

// Option 覆盖装配时的默认依赖（测试注入用）。
type Option func(*Server)

// WithMedia 注入媒体服务（测试或自定义适配器）；不传时由 COS 配置构造。
func WithMedia(svc *media.Service) Option { return func(s *Server) { s.media = svc } }

// New 装配全部依赖；公钥/邀请密钥解析失败即返回错误（拒绝启动）。
func New(cfg *config.Config, db *gorm.DB, opts ...Option) (http.Handler, error) {
	verifier, err := auth.LoadVerifier(cfg.Auth)
	if err != nil {
		return nil, err
	}
	invites, err := family.NewInviteSigner(cfg.InviteTokenSecret)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:       cfg,
		db:        db,
		users:     user.NewStore(db),
		family:    family.NewStore(db),
		classes:   class.NewStore(db),
		calendar:  cal.NewStore(db),
		invites:   invites,
		wechat:    wechat.NewClient(cfg.WeChat.AppID, cfg.WeChat.AppSecret, cfg.WeChat.APIBase, cfg.WeChat.EnvVersion),
		textbooks: textbook.NewStore(db),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.media == nil {
		svc, err := media.NewService(MediaConfig(cfg), db)
		if err != nil {
			return nil, err
		}
		s.media = svc
	}
	authMW := middleware.Auth(verifier.Verify)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)

	// 业务路由统一挂在 /api/v1，全部经 Auth 中间件。
	protect := func(h http.HandlerFunc) http.Handler { return authMW(h) }
	mux.Handle("GET /api/v1/me", protect(s.getMe))
	mux.Handle("PATCH /api/v1/me", protect(s.patchMe))

	// 孩子与监护关系（T03，api-contract §3.1）。
	mux.Handle("POST /api/v1/children", protect(s.createChild))
	mux.Handle("GET /api/v1/children", protect(s.listChildren))
	mux.Handle("PATCH /api/v1/children/{childId}", protect(s.patchChild))
	mux.Handle("POST /api/v1/children/{childId}/guardian-invites", protect(s.createGuardianInvite))
	mux.Handle("POST /api/v1/guardianships/accept", protect(s.acceptGuardianship))
	mux.Handle("DELETE /api/v1/children/{childId}/guardians/{userId}", protect(s.removeGuardian))
	mux.Handle("DELETE /api/v1/children/{childId}/guardians/me", protect(s.quitGuardianship))

	// 班级与成员（T04，api-contract §3.2）。
	mux.Handle("POST /api/v1/classes", protect(s.createClass))
	mux.Handle("GET /api/v1/classes/my", protect(s.listMyClasses))
	mux.Handle("GET /api/v1/classes/public", protect(s.searchPublicClasses))
	mux.Handle("GET /api/v1/classes/{classId}", protect(s.getClass))
	mux.Handle("PATCH /api/v1/classes/{classId}", protect(s.patchClass))
	mux.Handle("POST /api/v1/join-requests", protect(s.createJoinRequest))
	mux.Handle("GET /api/v1/classes/{classId}/join-requests", protect(s.listJoinRequests))
	mux.Handle("POST /api/v1/join-requests/{requestId}/approve", protect(s.approveJoinRequest))
	mux.Handle("POST /api/v1/join-requests/{requestId}/reject", protect(s.rejectJoinRequest))
	mux.Handle("GET /api/v1/classes/{classId}/members", protect(s.listMembers))
	mux.Handle("DELETE /api/v1/classes/{classId}/members/me", protect(s.quitClass))
	mux.Handle("DELETE /api/v1/classes/{classId}/members/{userId}", protect(s.removeMember))
	mux.Handle("POST /api/v1/classes/{classId}/members/{userId}/promote", protect(s.promoteMember))
	mux.Handle("POST /api/v1/classes/{classId}/members/{userId}/demote", protect(s.demoteMember))
	mux.Handle("DELETE /api/v1/classes/{classId}", protect(s.dissolveClass))
	mux.Handle("GET /api/v1/classes/{classId}/invite-qrcode", protect(s.inviteQRCode))

	// 孩子入班与权限联动（T05，api-contract §3.3）。
	mux.Handle("POST /api/v1/children/{childId}/enrollments", protect(s.createEnrollment))
	mux.Handle("DELETE /api/v1/children/{childId}/enrollments/{classId}", protect(s.deleteEnrollment))
	mux.Handle("GET /api/v1/classes/{classId}/children", protect(s.listClassChildren))

	// 上学日历（T09，api-contract §3.9）。
	mux.Handle("GET /api/v1/school-calendar", protect(s.getSchoolCalendar))

	// 媒体直传（T07，api-contract §3.5）。
	mux.Handle("POST /api/v1/media/upload-tickets", protect(s.createUploadTicket))
	mux.Handle("POST /api/v1/media/upload-tickets/{uploadId}/confirm", protect(s.confirmUploadTicket))

	// 教材库（T06，api-contract §3.4）。
	mux.Handle("POST /api/v1/textbooks", protect(s.createTextbook))
	mux.Handle("GET /api/v1/textbooks", protect(s.listTextbooks))
	mux.Handle("POST /api/v1/textbooks/{textbookId}/units", protect(s.addTextbookUnits))
	mux.Handle("PUT /api/v1/classes/{classId}/textbooks", protect(s.setClassTextbook))
	mux.Handle("GET /api/v1/classes/{classId}/textbooks", protect(s.listClassTextbooks))

	return middleware.Logging(mux), nil
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type meResponse struct {
	ID        uint64 `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

func toMe(u *model.User) meResponse {
	return meResponse{ID: u.ID, Nickname: u.WxNickname, AvatarURL: u.AvatarURL}
}

// getMe 返回当前用户；首次请求 upsert users（api-contract §3.1）。
func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, toMe(u))
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Nickname string `json:"nickname"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	nickname := strings.TrimSpace(body.Nickname)
	if nickname == "" || utf8.RuneCountInString(nickname) > 64 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "昵称必填且不超过 64 字")
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	updated, err := s.users.UpdateNickname(r.Context(), u.StrideUserID, nickname)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toMe(updated))
}

// currentUser 取已验签请求对应的本地用户（首见 upsert）；失败时已写好响应。
func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (*model.User, bool) {
	claims, ok := middleware.Claims(r.Context())
	if !ok {
		httpx.Write(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "缺少登录凭证")
		return nil, false
	}
	u, err := s.users.Ensure(r.Context(), claims.Subject, claims.Name)
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return u, true
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	middleware.Logger(r.Context()).Error("internal error", "err", err)
	httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "服务器内部错误")
}
