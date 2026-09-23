package server

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/subject"
	"github.com/zhaochy1990/homework/backend/internal/homework/textbook"
)

// ---------- 响应形状（api-contract §3.4） ----------

type unitResponse struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

type textbookResponse struct {
	ID        uint64         `json:"id"`
	Subject   string         `json:"subject"`
	Name      string         `json:"name"`
	Grade     string         `json:"grade"`
	Term      string         `json:"term"`
	CreatedBy uint64         `json:"createdBy"`
	CreatedAt time.Time      `json:"createdAt"`
	Units     []unitResponse `json:"units"`
}

func toTextbook(t *textbook.Textbook) textbookResponse {
	units := make([]unitResponse, 0, len(t.Units))
	for _, u := range t.Units {
		units = append(units, unitResponse{ID: u.ID, Name: u.Name, SortOrder: u.SortOrder})
	}
	return textbookResponse{
		ID: t.ID, Subject: t.Subject, Name: t.Name, Grade: t.Grade, Term: t.Term,
		CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt.UTC(), Units: units,
	}
}

// ---------- 请求解析 ----------

type unitInput struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

// parseUnits 校验单元名并归一为 store 入参（排序号 0 留待 store 按位置补齐）。
func parseUnits(w http.ResponseWriter, units []unitInput) ([]textbook.UnitInput, bool) {
	if len(units) > 500 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "单元数量不能超过 500")
		return nil, false
	}
	out := make([]textbook.UnitInput, 0, len(units))
	for _, u := range units {
		name := strings.TrimSpace(u.Name)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "单元名必填且不超过 64 字")
			return nil, false
		}
		out = append(out, textbook.UnitInput{Name: name, SortOrder: u.SortOrder})
	}
	return out, true
}

// ---------- 全局教材库 ----------

// createTextbook 新增教材（含可选单元）；任何登录用户可添加，v1 无审核。
func (s *Server) createTextbook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Subject string      `json:"subject"`
		Name    string      `json:"name"`
		Grade   string      `json:"grade"`
		Term    string      `json:"term"`
		Units   []unitInput `json:"units"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !subject.Valid(body.Subject) {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "科目不在全局枚举内")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || utf8.RuneCountInString(name) > 128 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "教材名必填且不超过 128 字")
		return
	}
	grade := strings.TrimSpace(body.Grade)
	term := strings.TrimSpace(body.Term)
	if utf8.RuneCountInString(grade) > 32 || utf8.RuneCountInString(term) > 16 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "年级或册别过长")
		return
	}
	units, ok := parseUnits(w, body.Units)
	if !ok {
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	t, err := s.textbooks.Create(r.Context(), u.ID,
		model.Textbook{Subject: body.Subject, Name: name, Grade: grade, Term: term}, units)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toTextbook(t))
}

// listTextbooks 全局库检索：?subject=&keyword=&page=。
func (s *Server) listTextbooks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	subjectFilter := strings.TrimSpace(q.Get("subject"))
	if subjectFilter != "" && !subject.Valid(subjectFilter) {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "科目不在全局枚举内")
		return
	}
	keyword := strings.TrimSpace(q.Get("keyword"))
	page, pageSize, offset := pageParams(r)
	if _, ok := s.currentUser(w, r); !ok {
		return
	}
	items, total, err := s.textbooks.List(r.Context(), subjectFilter, keyword, offset, pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]textbookResponse, len(items))
	for i := range items {
		out[i] = toTextbook(&items[i])
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "page_size": pageSize, "total": total,
	})
}

// addTextbookUnits 向已有教材补充单元。
func (s *Server) addTextbookUnits(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "textbookId")
	if !ok {
		return
	}
	var body struct {
		Units []unitInput `json:"units"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	units, ok := parseUnits(w, body.Units)
	if !ok {
		return
	}
	if len(units) == 0 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "至少提交一个单元")
		return
	}
	if _, ok := s.currentUser(w, r); !ok {
		return
	}
	t, err := s.textbooks.AddUnits(r.Context(), id, units)
	if errors.Is(err, textbook.ErrNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "教材不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toTextbook(t))
}

// ---------- 班级选用教材 ----------

// setClassTextbook 班级按科目选用/更换教材；须为 admin。
func (s *Server) setClassTextbook(w http.ResponseWriter, r *http.Request) {
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	var body struct {
		Subject    string `json:"subject"`
		TextbookID uint64 `json:"textbookId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !subject.Valid(body.Subject) {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "科目不在全局枚举内")
		return
	}
	if body.TextbookID == 0 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "textbookId 必填")
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireAdmin(w, r, classID, u.ID); !ok {
		return
	}
	err := s.textbooks.SetClassTextbook(r.Context(), classID, body.Subject, body.TextbookID)
	switch {
	case errors.Is(err, textbook.ErrNotFound):
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "教材不存在")
		return
	case errors.Is(err, textbook.ErrSubjectMismatch):
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "教材科目与所选科目不一致")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	t, err := s.textbooks.Get(r.Context(), body.TextbookID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"subject": body.Subject, "textbook": toTextbook(t)})
}

// listClassTextbooks 班级已选教材（按科目）；成员可见。
func (s *Server) listClassTextbooks(w http.ResponseWriter, r *http.Request) {
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	if _, _, ok := s.requireMember(w, r, classID, u.ID); !ok {
		return
	}
	chosen, err := s.textbooks.ListClassTextbooks(r.Context(), classID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(chosen))
	for i := range chosen {
		items = append(items, map[string]any{
			"subject":  chosen[i].Subject,
			"textbook": toTextbook(&chosen[i].Textbook),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}
