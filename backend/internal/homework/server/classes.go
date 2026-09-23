package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zhaochy1990/homework/backend/internal/homework/class"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"gorm.io/gorm"
)

type classResponse struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	Visibility   string `json:"visibility"`
	JoinApproval bool   `json:"joinApproval"`
}

func toClass(c *model.Class) classResponse {
	return classResponse{ID: c.ID, Name: c.Name, Visibility: c.Visibility, JoinApproval: c.JoinApproval}
}

type myClassResponse struct {
	classResponse
	Role string `json:"role"`
}

type classDetailResponse struct {
	classResponse
	MyRole     string          `json:"myRole"`
	MyChildren []childResponse `json:"myChildren"`
}

func (s *Server) createClass(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Name         string `json:"name"`
		Visibility   string `json:"visibility"`
		JoinApproval bool   `json:"joinApproval"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	name, ok := requiredText(w, body.Name, "班级名称", 64)
	if !ok {
		return
	}
	visibility, ok := validVisibility(w, body.Visibility)
	if !ok {
		return
	}
	c, err := s.classes.CreateClass(r.Context(), u.ID, name, visibility, body.JoinApproval)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toClass(c))
}

func (s *Server) listMyClasses(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	mine, err := s.classes.ListMyClasses(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]myClassResponse, 0, len(mine))
	for i := range mine {
		items = append(items, myClassResponse{classResponse: toClass(&mine[i].Class), Role: mine[i].Role})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) searchPublicClasses(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(w, r); !ok {
		return
	}
	page, pageSize, offset := pageParams(r)
	classes, total, err := s.classes.SearchPublicClasses(r.Context(), strings.TrimSpace(r.URL.Query().Get("keyword")), offset, pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]classResponse, 0, len(classes))
	for i := range classes {
		items = append(items, toClass(&classes[i]))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": items, "page": page, "page_size": pageSize, "total": total,
	})
}

func (s *Server) getClass(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	c, role, ok := s.requireMember(w, r, classID, u.ID)
	if !ok {
		return
	}
	children, err := s.classes.MyChildrenInClass(r.Context(), classID, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	myChildren := make([]childResponse, 0, len(children))
	for i := range children {
		myChildren = append(myChildren, toChild(&children[i]))
	}
	httpx.JSON(w, http.StatusOK, classDetailResponse{
		classResponse: toClass(c),
		MyRole:        role,
		MyChildren:    myChildren,
	})
}

func (s *Server) patchClass(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, ok := s.requireAdmin(w, r, classID, u.ID); !ok {
		return
	}
	var body struct {
		Name         *string `json:"name"`
		Visibility   *string `json:"visibility"`
		JoinApproval *bool   `json:"joinApproval"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name != nil {
		name, ok := requiredText(w, *body.Name, "班级名称", 64)
		if !ok {
			return
		}
		body.Name = &name
	}
	if body.Visibility != nil {
		v, ok := validVisibility(w, *body.Visibility)
		if !ok {
			return
		}
		body.Visibility = &v
	}
	c, err := s.classes.UpdateClass(r.Context(), classID, body.Name, body.Visibility, body.JoinApproval)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toClass(c))
}

// createJoinRequest 三分支入班（api-contract §3.2）。
func (s *Server) createJoinRequest(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body struct {
		ClassID    uint64 `json:"classId"`
		InviteCode string `json:"inviteCode"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ClassID == 0 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "classId 必填")
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
	if role != "" {
		httpx.Write(w, http.StatusConflict, httpx.CodeAlreadyExists, "已是班级成员")
		return
	}

	// 私密班：邀请码即授权，绕过审批。
	if c.Visibility == "private" {
		if c.InviteCode == "" || strings.TrimSpace(body.InviteCode) != c.InviteCode {
			httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "邀请码无效")
			return
		}
		s.approveJoin(w, r, c.ID, u.ID)
		return
	}
	// 公开班无审批：直接成为 member。
	if !c.JoinApproval {
		s.approveJoin(w, r, c.ID, u.ID)
		return
	}
	// 公开班有审批：pending（重复申请由应用层校验，并发下可能产生两条）。
	pending, err := s.classes.PendingJoinRequest(r.Context(), c.ID, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if pending != nil {
		httpx.Write(w, http.StatusConflict, httpx.CodeAlreadyExists, "已有待审批申请")
		return
	}
	jr, err := s.classes.CreateJoinRequest(r.Context(), c.ID, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"status": class.StatusPending, "requestId": jr.ID})
}

func (s *Server) approveJoin(w http.ResponseWriter, r *http.Request, classID, userID uint64) {
	if err := s.classes.AddMember(r.Context(), classID, userID, class.RoleMember); err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": class.StatusApproved})
}

func (s *Server) listJoinRequests(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, ok := s.requireAdmin(w, r, classID, u.ID); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = class.StatusPending
	}
	items, err := s.classes.ListJoinRequests(r.Context(), classID, status)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) approveJoinRequest(w http.ResponseWriter, r *http.Request) {
	s.decideJoinRequest(w, r, true)
}

func (s *Server) rejectJoinRequest(w http.ResponseWriter, r *http.Request) {
	s.decideJoinRequest(w, r, false)
}

func (s *Server) decideJoinRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	requestID, ok := pathID(w, r, "requestId")
	if !ok {
		return
	}
	jr, err := s.classes.GetJoinRequest(r.Context(), requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "申请不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, ok := s.requireAdmin(w, r, jr.ClassID, u.ID); !ok {
		return
	}
	updated, err := s.classes.DecideJoinRequest(r.Context(), requestID, u.ID, approve)
	if errors.Is(err, class.ErrAlreadyDecided) {
		httpx.Write(w, http.StatusConflict, httpx.CodeAlreadyExists, "申请已处理")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": updated.Status})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
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
	members, total, err := s.classes.ListMembers(r.Context(), classID, offset, pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": members, "page": page, "page_size": pageSize, "total": total,
	})
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	targetID, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	if _, ok := s.requireAdmin(w, r, classID, u.ID); !ok {
		return
	}
	err := s.classes.RemoveMember(r.Context(), classID, targetID)
	switch {
	case errors.Is(err, class.ErrMemberNotFound):
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "成员不存在")
	case errors.Is(err, class.ErrAdminImmutable):
		httpx.Write(w, http.StatusConflict, httpx.CodeAdminImmutable, "管理员不可被移除")
	case errors.Is(err, class.ErrMemberHasChildren):
		httpx.Write(w, http.StatusConflict, httpx.CodeMemberHasChildren, "该成员有孩子在班")
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) promoteMember(w http.ResponseWriter, r *http.Request) {
	s.setMemberRole(w, r, class.RoleAdmin)
}

func (s *Server) demoteMember(w http.ResponseWriter, r *http.Request) {
	s.setMemberRole(w, r, class.RoleMember)
}

// setMemberRole 仅创建者可任命/撤销 admin（api-contract §3.2）。
func (s *Server) setMemberRole(w http.ResponseWriter, r *http.Request, role string) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	targetID, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	c, ok := s.requireCreator(w, r, classID, u.ID)
	if !ok {
		return
	}
	if role == class.RoleMember && targetID == c.CreatedBy {
		httpx.Write(w, http.StatusConflict, httpx.CodeAdminImmutable, "创建者不可被降级")
		return
	}
	err := s.classes.SetRole(r.Context(), classID, targetID, role)
	switch {
	case errors.Is(err, class.ErrMemberNotFound):
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "成员不存在")
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) quitClass(w http.ResponseWriter, r *http.Request) {
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
	err := s.classes.Quit(r.Context(), classID, u.ID)
	if errors.Is(err, class.ErrMemberHasChildren) {
		httpx.Write(w, http.StatusConflict, httpx.CodeMemberHasChildren, "有孩子在班，不能退出")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dissolveClass(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	if _, ok := s.requireCreator(w, r, classID, u.ID); !ok {
		return
	}
	err := s.classes.Dissolve(r.Context(), classID)
	if errors.Is(err, class.ErrClassNotEmpty) {
		httpx.Write(w, http.StatusConflict, httpx.CodeClassNotEmpty, "班级仍有孩子在班")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) inviteQRCode(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	classID, ok := pathID(w, r, "classId")
	if !ok {
		return
	}
	c, ok := s.requireAdmin(w, r, classID, u.ID)
	if !ok {
		return
	}
	png, err := s.wechat.GetUnlimitedQRCode(r.Context(), c.InviteCode)
	if err != nil {
		middleware.Logger(r.Context()).Error("wechat qrcode", "err", err, "class_id", classID)
		httpx.Write(w, http.StatusBadGateway, httpx.CodeWechatUnavailable, "小程序码生成失败")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// ---------- 权限与工具 ----------

// requireMember 校验班级存在且当前用户是成员；否则写 404（不泄露班级是否存在）。
func (s *Server) requireMember(w http.ResponseWriter, r *http.Request, classID, userID uint64) (*model.Class, string, bool) {
	c, err := s.classes.GetClass(r.Context(), classID)
	if errors.Is(err, class.ErrClassNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "班级不存在")
		return nil, "", false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, "", false
	}
	role, err := s.classes.MemberRole(r.Context(), classID, userID)
	if err != nil {
		s.internalError(w, r, err)
		return nil, "", false
	}
	if role == "" {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "班级不存在")
		return nil, "", false
	}
	return c, role, true
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request, classID, userID uint64) (*model.Class, bool) {
	c, role, ok := s.requireMember(w, r, classID, userID)
	if !ok {
		return nil, false
	}
	if role != class.RoleAdmin {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "需要管理员权限")
		return nil, false
	}
	return c, true
}

func (s *Server) requireCreator(w http.ResponseWriter, r *http.Request, classID, userID uint64) (*model.Class, bool) {
	c, _, ok := s.requireMember(w, r, classID, userID)
	if !ok {
		return nil, false
	}
	if c.CreatedBy != userID {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "仅创建者可以操作")
		return nil, false
	}
	return c, true
}

func validVisibility(w http.ResponseWriter, v string) (string, bool) {
	switch v {
	case "public", "private":
		return v, true
	}
	httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "visibility 必须是 public 或 private")
	return "", false
}

func pageParams(r *http.Request) (page, pageSize, offset int) {
	page = queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}
	pageSize = queryInt(r, "page_size", 20)
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, (page - 1) * pageSize
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
