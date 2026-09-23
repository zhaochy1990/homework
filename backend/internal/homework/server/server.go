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
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/user"
	"gorm.io/gorm"
)

type Server struct {
	cfg   *config.Config
	db    *gorm.DB
	users *user.Store
}

// New 装配全部依赖；公钥解析失败即返回错误（拒绝启动）。
func New(cfg *config.Config, db *gorm.DB) (http.Handler, error) {
	verifier, err := auth.LoadVerifier(cfg.Auth)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, db: db, users: user.NewStore(db)}
	authMW := middleware.Auth(verifier.Verify)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)

	// 业务路由统一挂在 /api/v1，全部经 Auth 中间件。
	// 后续 ticket 在此追加，例如：
	//   mux.Handle("POST /api/v1/children", authMW(http.HandlerFunc(s.createChild)))
	mux.Handle("GET /api/v1/me", authMW(http.HandlerFunc(s.getMe)))
	mux.Handle("PATCH /api/v1/me", authMW(http.HandlerFunc(s.patchMe)))

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
	claims, ok := middleware.Claims(r.Context())
	if !ok {
		httpx.Write(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "缺少登录凭证")
		return
	}
	u, err := s.users.Ensure(r.Context(), claims.Subject, claims.Name)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toMe(u))
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.Claims(r.Context())
	if !ok {
		httpx.Write(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "缺少登录凭证")
		return
	}
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
	if _, err := s.users.Ensure(r.Context(), claims.Subject, claims.Name); err != nil {
		s.internalError(w, r, err)
		return
	}
	u, err := s.users.UpdateNickname(r.Context(), claims.Subject, nickname)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toMe(u))
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	middleware.Logger(r.Context()).Error("internal error", "err", err)
	httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "服务器内部错误")
}
