package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/media"
)

func MediaConfig(cfg *config.Config) media.Config {
	return media.Config{
		Bucket:      cfg.COS.Bucket,
		Region:      cfg.COS.Region,
		AppID:       cfg.COS.AppID,
		SecretID:    cfg.COS.SecretID,
		SecretKey:   cfg.COS.SecretKey,
		STSTTL:      cfg.COS.STSTTL,
		PlaybackTTL: cfg.COS.PlaybackTTL,
	}
}

type stsResponse struct {
	TmpSecretID  string `json:"tmpSecretId"`
	TmpSecretKey string `json:"tmpSecretKey"`
	SessionToken string `json:"sessionToken"`
	ExpiredTime  int64  `json:"expiredTime"` // Unix 秒，供 cos-wx-sdk-v5 getAuthorization
}

type uploadTicketResponse struct {
	UploadID  string      `json:"uploadId"`
	ObjectKey string      `json:"objectKey"`
	Bucket    string      `json:"bucket"`
	Region    string      `json:"region"`
	STS       stsResponse `json:"sts"`
}

// createUploadTicket 签发一次直传授权（api-contract §3.5）。
func (s *Server) createUploadTicket(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "媒体服务未配置")
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Kind        string `json:"kind"`
		ContentType string `json:"contentType"`
		SizeBytes   int64  `json:"sizeBytes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	ticket, err := s.media.CreateTicket(r.Context(), u.ID, body.Kind, body.ContentType, body.SizeBytes)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, uploadTicketResponse{
		UploadID:  ticket.UploadID,
		ObjectKey: ticket.ObjectKey,
		Bucket:    ticket.Bucket,
		Region:    ticket.Region,
		STS: stsResponse{
			TmpSecretID:  ticket.STS.TmpSecretID,
			TmpSecretKey: ticket.STS.TmpSecretKey,
			SessionToken: ticket.STS.SessionToken,
			ExpiredTime:  ticket.STS.ExpiredTime.Unix(),
		},
	})
}

// confirmUploadTicket 校验对象真实存在且大小一致后落 confirmed（api-contract §3.5）。
func (s *Server) confirmUploadTicket(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "媒体服务未配置")
		return
	}
	u, ok := s.currentUser(w, r)
	if !ok {
		return
	}
	uploadID := r.PathValue("uploadId")
	if uploadID == "" {
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "上传凭证不存在")
		return
	}
	var body struct {
		SizeBytes      int64  `json:"sizeBytes"`
		Etag           string `json:"etag"`
		DurationSec    int    `json:"durationSec"`
		ThumbObjectKey string `json:"thumbObjectKey"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "请求体不是合法 JSON")
		return
	}
	confirmed, err := s.media.Confirm(r.Context(), u.ID, uploadID, body.SizeBytes, body.Etag, body.ThumbObjectKey)
	if err != nil {
		s.writeMediaError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"objectKey":   confirmed.ObjectKey,
		"playbackUrl": confirmed.PlaybackURL,
	})
}

func (s *Server) writeMediaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, media.ErrInvalidKind),
		errors.Is(err, media.ErrInvalidContentType),
		errors.Is(err, media.ErrSizeOutOfRange):
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, err.Error())
	case errors.Is(err, media.ErrTicketNotFound):
		httpx.Write(w, http.StatusNotFound, httpx.CodeNotFound, "上传凭证不存在")
	case errors.Is(err, media.ErrUploadMismatch):
		httpx.Write(w, http.StatusUnprocessableEntity, httpx.CodeUploadMismatch, "上传对象与上报不符")
	default:
		s.internalError(w, r, err)
	}
}
