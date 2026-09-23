// Package server 组装路由与中间件。
package server

import (
	"net/http"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"gorm.io/gorm"
)

type Server struct {
	cfg *config.Config
	db  *gorm.DB
}

func New(cfg *config.Config, db *gorm.DB) http.Handler {
	s := &Server{cfg: cfg, db: db}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)

	// 业务路由统一挂在 /api/v1，全部经 Auth 中间件。
	// 后续 ticket 在此追加，例如：
	//   mux.Handle("POST /api/v1/children", middleware.Auth(http.HandlerFunc(s.createChild)))
	mux.Handle("GET /api/v1/me", middleware.Auth(http.HandlerFunc(s.me)))

	return middleware.Logging(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// me 是 T02 的占位实现，仅用于验证鉴权网关已生效。
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	httpx.Write(w, http.StatusNotImplemented, httpx.CodeNotImplemented, "待 T02 实现")
}
