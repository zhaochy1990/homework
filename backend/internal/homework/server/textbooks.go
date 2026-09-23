package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

func decodeBody(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, max)).Decode(dst); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return false
	}
	return true
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

// pagination 解析 ?page=&page_size=（默认 20，上限 100）。
func pagination(w http.ResponseWriter, q url.Values) (page, pageSize int, ok bool) {
	page = 1
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "page 必须为正整数")
			return 0, 0, false
		}
		page = n
	}
	pageSize = 20
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "page_size 取值范围 1-100")
			return 0, 0, false
		}
		pageSize = n
	}
	return page, pageSize, true
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
	if !decodeBody(w, r, 64*1024, &body) {
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
	page, pageSize, ok := pagination(w, q)
	if !ok {
		return
	}
	if _, ok := s.currentUser(w, r); !ok {
		return
	}
	items, total, err := s.textbooks.List(r.Context(), subjectFilter, keyword, (page-1)*pageSize, pageSize)
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
	if !decodeBody(w, r, 64*1024, &body) {
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
	if !decodeBody(w, r, 4096, &body) {
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
	if !s.requireClassAdmin(w, r, classID, u.ID) {
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
	if _, isMember, err := s.textbooks.MemberRole(r.Context(), classID, u.ID); err != nil {
		s.internalError(w, r, err)
		return
	} else if !isMember {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "非班级成员")
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

// requireClassAdmin 校验当前用户是班级 admin；否则写 403 并返回 false。
func (s *Server) requireClassAdmin(w http.ResponseWriter, r *http.Request, classID, userID uint64) bool {
	role, isMember, err := s.textbooks.MemberRole(r.Context(), classID, userID)
	if err != nil {
		s.internalError(w, r, err)
		return false
	}
	if !isMember {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "非班级成员")
		return false
	}
	if role != "admin" {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "需要管理员权限")
		return false
	}
	return true
}
