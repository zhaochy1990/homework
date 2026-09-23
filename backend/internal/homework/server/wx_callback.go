package server

import (
	"io"
	"net/http"

	"github.com/zhaochy1990/homework/backend/internal/homework/httpx"
	"github.com/zhaochy1990/homework/backend/internal/homework/material"
	"github.com/zhaochy1990/homework/backend/internal/homework/wechat"
)

// wxCallback 是微信小程序消息推送端点（launch-checklist §2，JSON 明文模式）：
// GET 用于配置时的 echostr 校验，POST 接收 wxa_media_check 异步检测结果。
// 按 trace_id 幂等更新资料安全状态；验签失败一律 403（防伪造回调放行违规内容）。
func (s *Server) wxCallback(w http.ResponseWriter, r *http.Request) {
	token := s.cfg.WeChat.MsgPushToken
	if token == "" {
		httpx.Write(w, http.StatusInternalServerError, httpx.CodeInternal, "消息推送未配置")
		return
	}
	q := r.URL.Query()
	if !wechat.VerifyCallbackSignature(token, q.Get("signature"), q.Get("timestamp"), q.Get("nonce")) {
		httpx.Write(w, http.StatusForbidden, httpx.CodeForbidden, "签名校验失败")
		return
	}

	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte(q.Get("echostr")))
		return
	}

	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "读取回调体失败")
		return
	}
	ev, err := wechat.ParseMediaCheckEvent(data)
	if err != nil {
		httpx.Write(w, http.StatusBadRequest, httpx.CodeBadRequest, "回调体不是合法 JSON")
		return
	}
	if ev.Event == "wxa_media_check" && ev.TraceID != "" {
		status := material.StatusBlocked
		if ev.Result.Suggest == "pass" {
			status = material.StatusPass
		}
		if _, err := s.materials.SetSecStatusByTrace(r.Context(), ev.TraceID, status); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("success"))
}
