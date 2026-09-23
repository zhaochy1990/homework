// Package server 组装路由与中间件。
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/family"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/user"
	"gorm.io/gorm"
)

type Server struct {
	cfg     *config.Config
	db      *gorm.DB
	users   *user.Store
	family  *family.Store
	invites *family.InviteSigner
}

// New 装配全部依赖；公钥/邀请密钥解析失败即返回错误（拒绝启动）。
func New(cfg *config.Config, db *gorm.DB) (http.Handler, error) {
	verifier, err := auth.LoadVerifier(cfg.Auth)
	if err != nil {
		return nil, err
	}
	invites, err := family.NewInviteSigner(cfg.InviteTokenSecret)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, db: db, users: user.NewStore(db), family: family.NewStore(db), invites: invites}
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
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
