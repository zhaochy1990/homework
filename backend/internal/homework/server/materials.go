package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/material"
	"github.com/zhaochy1990/homework/backend/internal/homework/media"
	"github.com/zhaochy1990/homework/backend/internal/homework/middleware"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
	"github.com/zhaochy1990/homework/backend/internal/homework/wechat"
)

// maxMaterialText 是单次 msgSecCheck 的文本上限（research/002 §5.2）。
const maxMaterialText = 2500

type materialResponse struct {
	ID          uint64    `json:"id"`
	ClassID     uint64    `json:"classId"`
	UnitID      *uint64   `json:"unitId,omitempty"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	SizeBytes   int64     `json:"sizeBytes,omitempty"`
	SecStatus   string    `json:"secStatus"`
	PlaybackURL string    `json:"playbackUrl,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toMaterial(m *model.Material) materialResponse {
	return materialResponse{
		ID: m.ID, ClassID: m.ClassID, UnitID: m.UnitID, Type: m.Type, Title: m.Title,
		Body: m.Body, SizeBytes: m.SizeBytes, SecStatus: m.SecStatus,
		CreatedAt: m.CreatedAt.UTC(),
	}
}

// createMaterial 新增资料（admin）。文本同步检测通过才入库；
// 媒体走 mediaCheckAsync，落 pending（对成员不可见）。
func (s *Server) createMaterial(w http.ResponseWriter, r *http.Request) {
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
		Type           string  `json:"type"`
		Title          string  `json:"title"`
		Body           string  `json:"body"`
		UnitID         *uint64 `json:"unitId"`
		UploadID       string  `json:"uploadId"`
		ThumbObjectKey string  `json:"thumbObjectKey"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	title := strings.TrimSpace(body.Title)
	if utf8.RuneCountInString(title) > 128 {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "标题不超过 128 字")
		return
	}
	if body.UnitID != nil {
		exists, err := s.materials.UnitExists(r.Context(), *body.UnitID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !exists {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "单元不存在")
			return
		}
	}

	switch body.Type {
	case material.TypeText:
		s.createTextMaterial(w, r, u.ID, classID, title, body.Body, body.UnitID)
	case material.TypeImage, material.TypeVideo:
		s.createMediaMaterial(w, r, u.ID, classID, title, body.Type, body.UploadID, body.UnitID)
	default:
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "type 必须是 text / image / video")
	}
}

func (s *Server) createTextMaterial(w http.ResponseWriter, r *http.Request, userID, classID uint64, title, rawBody string, unitID *uint64) {
	text := strings.TrimSpace(rawBody)
	if text == "" || utf8.RuneCountInString(text) > maxMaterialText {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "正文必填且不超过 2500 字")
		return
	}
	suggest, _, err := s.wechat.MsgSecCheck(r.Context(), text, wechat.SceneForum, "")
	if err != nil {
		s.writeWechatError(w, r, err)
		return
	}
	if suggest != "pass" {
		httpx.Write(w, http.StatusUnprocessableEntity, httpx.CodeContentBlocked, "内容未通过安全检测")
		return
	}
	m, err := s.materials.Create(r.Context(), material.CreateInput{
		ClassID: classID, UnitID: unitID, Type: material.TypeText,
		Title: title, Body: text, SecStatus: material.StatusPass, UploadedBy: userID,
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toMaterial(m))
}

func (s *Server) createMediaMaterial(w http.ResponseWriter, r *http.Request, userID, classID uint64, title, mtype, uploadID string, unitID *uint64) {
	if s.media == nil {
		httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "媒体服务未配置")
		return
	}
	if uploadID == "" {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "uploadId 必填")
		return
	}
	rec, err := s.media.Resolve(r.Context(), userID, uploadID)
	if errors.Is(err, media.ErrTicketNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "上传凭证不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	wantKind := media.KindMaterialImage
	if mtype == material.TypeVideo {
		wantKind = media.KindMaterialVideo
	}
	if rec.Kind != wantKind {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "上传类型与资料类型不符")
		return
	}

	mediaURL, err := s.media.PlaybackURL(r.Context(), rec.ObjectKey)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	mediaType := wechat.MediaTypeImage
	if mtype == material.TypeVideo {
		mediaType = wechat.MediaTypeVideo
	}
	traceID, err := s.wechat.MediaCheckAsync(r.Context(), mediaURL, mediaType, wechat.SceneForum, "")
	if err != nil {
		s.writeWechatError(w, r, err)
		return
	}

	m, err := s.materials.Create(r.Context(), material.CreateInput{
		ClassID: classID, UnitID: unitID, Type: mtype, Title: title,
		CosKey: rec.ObjectKey, SizeBytes: rec.SizeBytes,
		SecStatus: material.StatusPending, SecTraceID: traceID, UploadedBy: userID,
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toMaterial(m))
}

func (s *Server) listMaterials(w http.ResponseWriter, r *http.Request) {
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
	q := r.URL.Query()
	var unitID *uint64
	if v := strings.TrimSpace(q.Get("unitId")); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "unitId 非法")
			return
		}
		unitID = &n
	}
	typeFilter := strings.TrimSpace(q.Get("type"))
	if typeFilter != "" && typeFilter != material.TypeText && typeFilter != material.TypeImage && typeFilter != material.TypeVideo {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "type 非法")
		return
	}
	page, pageSize, offset := pageParams(r)
	items, total, err := s.materials.List(r.Context(), classID, unitID, typeFilter, offset, pageSize)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]materialResponse, len(items))
	for i := range items {
		out[i] = toMaterial(&items[i])
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": out, "page": page, "page_size": pageSize, "total": total,
	})
}

func (s *Server) getMaterial(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "materialId")
	if !ok {
		return
	}
	m, err := s.materials.Get(r.Context(), id)
	if errors.Is(err, material.ErrNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "资料不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// 先审后显：pending/blocked 对外一律 404。
	if m.SecStatus != material.StatusPass {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "资料不存在")
		return
	}
	if _, _, ok := s.requireMember(w, r, m.ClassID, u.ID); !ok {
		return
	}
	resp := toMaterial(m)
	if m.Type != material.TypeText && m.CosKey != "" && s.media != nil {
		url, err := s.media.PlaybackURL(r.Context(), m.CosKey)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		resp.PlaybackURL = url
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (s *Server) deleteMaterial(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r, "materialId")
	if !ok {
		return
	}
	m, err := s.materials.Get(r.Context(), id)
	if errors.Is(err, material.ErrNotFound) {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "资料不存在")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if _, ok := s.requireAdmin(w, r, m.ClassID, u.ID); !ok {
		return
	}
	if m.CosKey != "" && s.media != nil {
		// 对象删除失败不阻塞记录删除（避免卡住用户），残留对象由运维清理。
		if err := s.media.DeleteObject(r.Context(), m.CosKey); err != nil {
			middleware.Logger(r.Context()).Error("material object delete failed", "err", err, "material_id", id)
		}
	}
	if err := s.materials.Delete(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeWechatError(w http.ResponseWriter, r *http.Request, err error) {
	middleware.Logger(r.Context()).Error("wechat call failed", "err", err)
	httpx.Write(w, http.StatusBadGateway, httpx.CodeWechatUnavailable, "微信接口暂不可用")
}
