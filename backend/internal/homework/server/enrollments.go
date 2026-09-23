package server

import (
	"errors"
	"net/http"

	"github.com/zhaochy1990/homework/backend/internal/homework/class"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
)

// createEnrollment 监护人把孩子报入班级（T05，api-contract §3.3）。
// 前提：当前用户是孩子的监护人，且已是该班成员；入班后其全部监护人自动 member。
func (s *Server) createEnrollment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	var body struct {
		ClassID uint64 `json:"classId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ClassID == 0 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "classId 必填")
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}
	c, err := s.classes.GetClass(r.Context(), body.ClassID)
	if errors.Is(err, class.ErrClassNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "班级不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	role, err := s.classes.MemberRole(r.Context(), c.ID, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if role == "" {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "请先加入班级")
		return
	}
	err = s.classes.EnrollChild(r.Context(), c.ID, childID)
	if errors.Is(err, class.ErrAlreadyExists) {
		httpx.Write(w, http.StatusConflict, httpx.CodeAlreadyExists, "孩子已在班")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"childId": childID, "classId": c.ID})
}

func (s *Server) deleteEnrollment(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	childID, ok := pathID(w, r, "childId")
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, ok := s.requireGuardian(w, r, childID, u.ID); !ok {
		return
	}
	err := s.classes.UnenrollChild(r.Context(), classID, childID)
	if errors.Is(err, class.ErrNotEnrolled) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "孩子不在班")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listClassChildren(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, _, ok := s.requireMember(w, r, classID, u.ID); !ok {
		return
	}
	page, pageSize, offset := pageParams(r)
	children, total, err := s.classes.ListChildrenInClass(r.Context(), classID, offset, pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]childResponse, 0, len(children))
	for i := range children {
		items = append(items, toChild(&children[i]))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": items, "page": page, "page_size": pageSize, "total": total,
	})
}
