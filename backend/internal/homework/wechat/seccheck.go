package wechat

import (
	"context"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// 内容安全场景与媒体类型（research/002 §4，参数以提审前官方文档为准）。
const (
	SceneProfile = 1 // 资料
	SceneForum   = 3 // 论坛（作业/资料正文）

	MediaTypeImage = 2
	MediaTypeVideo = 3
)

const (
	msgSecCheckPath     = "/wxa/msg_sec_check"
	mediaCheckAsyncPath = "/wxa/media_check_async"
)

// MsgSecCheck 同步文本检测，返回 suggest（pass / risky / racy）与 label。
// 调用方应把非 pass 一律按 blocked 处理（fail-closed）。
func (c *Client) MsgSecCheck(ctx context.Context, content string, scene int, openid string) (suggest string, label int, err error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", 0, err
	}
	body, err := json.Marshal(map[string]any{
		"content": content,
		"version": 2,
		"scene":   scene,
		"openid":  openid,
	})
	if err != nil {
		return "", 0, err
	}
	data, _, err := c.do(ctx, msgSecCheckPath, body, token)
	if err != nil {
		return "", 0, err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			Suggest string `json:"suggest"`
			Label   int    `json:"label"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", 0, err
	}
	if r.ErrCode != 0 {
		return "", 0, wxError{ErrCode: r.ErrCode, ErrMsg: r.ErrMsg}
	}
	return r.Result.Suggest, r.Result.Label, nil
}

// MediaCheckAsync 提交异步媒体检测，返回 trace_id（结果经消息推送回调）。
func (c *Client) MediaCheckAsync(ctx context.Context, mediaURL string, mediaType, scene int, openid string) (string, error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(map[string]any{
		"media_url":  mediaURL,
		"media_type": mediaType,
		"version":    2,
		"scene":      scene,
		"openid":     openid,
	})
	if err != nil {
		return "", err
	}
	data, _, err := c.do(ctx, mediaCheckAsyncPath, body, token)
	if err != nil {
		return "", err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	if r.ErrCode != 0 {
		return "", wxError{ErrCode: r.ErrCode, ErrMsg: r.ErrMsg}
	}
	if r.TraceID == "" {
		return "", errors.New("wechat: media_check_async 未返回 trace_id")
	}
	return r.TraceID, nil
}

// VerifyCallbackSignature 校验消息推送签名（明文模式）：
// sha1(sort(token, timestamp, nonce)) == signature。
func VerifyCallbackSignature(token, signature, timestamp, nonce string) bool {
	if token == "" || signature == "" {
		return false
	}
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	want := hex.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(signature))) == 1
}

// MediaCheckEvent 是 wxa_media_check 消息推送事件体（明文 JSON）。
type MediaCheckEvent struct {
	MsgType string `json:"MsgType"`
	Event   string `json:"Event"`
	TraceID string `json:"trace_id"`
	Result  struct {
		Suggest string `json:"suggest"`
		Label   int    `json:"label"`
	} `json:"result"`
}

func ParseMediaCheckEvent(data []byte) (*MediaCheckEvent, error) {
	var ev MediaCheckEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}
