package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/family"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

type childResponse struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func toChild(c *model.Child) childResponse {
	return childResponse{ID: c.ID, Name: c.Name, AvatarURL: c.AvatarURL}
}

// createChild 创建孩子，创建者自动成为监护人。avatarUploadId 待 T07 媒体链路接入。
func (s *Server) createChild(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	name, ok := validName(w, body.Name)
	if !ok {
		return
	}
	child, err := s.family.CreateChild(r.Context(), u.ID, name, "")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toChild(child))
}

func (s *Server) listChildren(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	children, err := s.family.ListChildren(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]childResponse, 0, len(children))
	for i := range children {
		items = append(items, toChild(&children[i]))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) patchChild(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}

	var body struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	if body.Name != nil {
		name, ok := validName(w, *body.Name)
		if !ok {
			return
		}
		body.Name = &name
	}
	child, err := s.family.UpdateChild(r.Context(), childID, body.Name, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toChild(child))
}

// createGuardianInvite 由任一监护人签发 72h 邀请 token（分享卡片）。
func (s *Server) createGuardianInvite(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}
	token, expiresAt, err := s.invites.Issue(childID, u.ID, family.InviteTTL)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"inviteToken": token,
		"expiresAt":   expiresAt.UTC(),
	})
}

func (s *Server) acceptGuardianship(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body struct {
		InviteToken string `json:"inviteToken"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	invite, err := s.invites.Verify(strings.TrimSpace(body.InviteToken))
	if err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "邀请无效或已过期")
		return
	}
	child, err := s.family.GetChild(r.Context(), invite.ChildID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "孩子不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.family.AddGuardian(r.Context(), invite.ChildID, u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toChild(child))
}

func (s *Server) removeGuardian(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	targetID, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}
	s.writeRemoveGuardian(w, r, childID, targetID)
}

func (s *Server) quitGuardianship(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}
	s.writeRemoveGuardian(w, r, childID, u.ID)
}

func (s *Server) writeRemoveGuardian(w http.ResponseWriter, r *http.Request, childID, targetID uint64) {
	err := s.family.RemoveGuardian(r.Context(), childID, targetID)
	switch {
	case errors.Is(err, family.ErrGuardianNotFound):
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "监护关系不存在")
	case errors.Is(err, family.ErrLastGuardian):
		httpx.Write(w, http.StatusConflict, httpx.CodeLastGuardian, "不能移除孩子的最后一个监护人")
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// requireGuardian 校验孩子存在且当前用户是其监护人；否则写 404 并返回 false。
// 非监护人一律 404（不泄露孩子是否存在，api-contract §1）。
func (s *Server) requireGuardian(w http.ResponseWriter, r *http.Request, childID, userID uint64) (*model.Child, bool) {
	child, err := s.family.GetChild(r.Context(), childID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "孩子不存在")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	isGuardian, err := s.family.IsGuardian(r.Context(), childID, userID)
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	if !isGuardian {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "孩子不存在")
		return nil, false
	}
	return child, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uint64, bool) {
	n, err := strconv.ParseUint(r.PathValue(name), 10, 64)
	if err != nil || n == 0 {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "资源不存在")
		return 0, false
	}
	return n, true
}

func validName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "姓名必填且不超过 64 字")
		return "", false
	}
	return name, true
}
