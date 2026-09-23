// Package wechat 封装小程序服务端调用：stable access_token 缓存与
// getUnlimitedQRCode 小程序码生成。T08 会在此基础上扩展内容安全接口。
package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	tokenPath  = "/cgi-bin/stable_token"
	qrPath     = "/wxa/getwxacodeunlimit"
	qrCacheTTL = 24 * time.Hour
	maxBody    = 4 << 20
)

type Client struct {
	appID      string
	appSecret  string
	apiBase    string
	envVersion string
	http       *http.Client

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
	qrCache     map[string]qrCacheEntry
}

type qrCacheEntry struct {
	png []byte
	at  time.Time
}

func NewClient(appID, appSecret, apiBase, envVersion string) *Client {
	if apiBase == "" {
		apiBase = "https://api.weixin.qq.com"
	}
	if envVersion == "" {
		envVersion = "release"
	}
	return &Client{
		appID:      appID,
		appSecret:  appSecret,
		apiBase:    strings.TrimRight(apiBase, "/"),
		envVersion: envVersion,
		http:       &http.Client{Timeout: 15 * time.Second},
		qrCache:    map[string]qrCacheEntry{},
	}
}

type wxError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

func (e wxError) Error() string {
	return fmt.Sprintf("wechat: errcode=%d errmsg=%s", e.ErrCode, e.ErrMsg)
}

// accessToken 返回缓存的 stable access_token，过期前提前 5 分钟刷新。
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	body, err := json.Marshal(map[string]any{
		"grant_type": "client_credential",
		"appid":      c.appID,
		"secret":     c.appSecret,
	})
	if err != nil {
		return "", err
	}
	data, _, err := c.do(ctx, tokenPath, body, "")
	if err != nil {
		return "", err
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	if r.AccessToken == "" {
		return "", wxError{ErrCode: r.ErrCode, ErrMsg: r.ErrMsg}
	}
	ttl := time.Duration(r.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 7200 * time.Second
	}
	c.token = r.AccessToken
	c.tokenExpiry = time.Now().Add(ttl - 5*time.Minute)
	return c.token, nil
}

// GetUnlimitedQRCode 生成 scene 对应的小程序码 PNG（进程内缓存 24h）。
// ponytail: 进程内缓存，单实例足够；多实例改 COS/Redis。
func (c *Client) GetUnlimitedQRCode(ctx context.Context, scene string) ([]byte, error) {
	c.mu.Lock()
	if e, ok := c.qrCache[scene]; ok && time.Since(e.at) < qrCacheTTL {
		c.mu.Unlock()
		return e.png, nil
	}
	c.mu.Unlock()

	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"scene":       scene,
		"check_path":  false,
		"env_version": c.envVersion,
		"width":       430,
	})
	if err != nil {
		return nil, err
	}
	data, contentType, err := c.do(ctx, qrPath, body, token)
	if err != nil {
		return nil, err
	}
	if strings.Contains(contentType, "application/json") || (len(data) > 0 && data[0] == '{') {
		var e wxError
		_ = json.Unmarshal(data, &e)
		return nil, e
	}

	c.mu.Lock()
	c.qrCache[scene] = qrCacheEntry{png: data, at: time.Now()}
	c.mu.Unlock()
	return data, nil
}

func (c *Client) do(ctx context.Context, path string, body []byte, token string) ([]byte, string, error) {
	endpoint := c.apiBase + path
	if token != "" {
		endpoint += "?access_token=" + url.QueryEscape(token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("wechat: HTTP %d", resp.StatusCode)
	}
	return data, resp.Header.Get("Content-Type"), nil
}
