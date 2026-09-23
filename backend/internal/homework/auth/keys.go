package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
)

const publicKeyFetchTimeout = 5 * time.Second

// LoadVerifier 按配置解析公钥并构建验签器：优先 HTTP 端点，失败回退本地文件。
func LoadVerifier(cfg config.Auth) (*Verifier, error) {
	pemBytes, err := loadPublicKeyPEM(cfg)
	if err != nil {
		return nil, err
	}
	return NewVerifier(pemBytes, cfg.Issuer, cfg.Audience)
}

func loadPublicKeyPEM(cfg config.Auth) ([]byte, error) {
	var urlErr error
	if cfg.PublicKeyURL != "" {
		pemBytes, err := fetchPublicKey(cfg.PublicKeyURL)
		if err == nil {
			return pemBytes, nil
		}
		urlErr = err
	}
	if cfg.PublicKeyFile != "" {
		pemBytes, err := os.ReadFile(cfg.PublicKeyFile)
		if err == nil {
			return pemBytes, nil
		}
		return nil, fmt.Errorf("auth: 读取公钥文件 %s: %w（URL: %v）", cfg.PublicKeyFile, err, urlErr)
	}
	if urlErr != nil {
		return nil, fmt.Errorf("auth: 拉取公钥: %w", urlErr)
	}
	return nil, errors.New("auth: 未配置 AUTH_JWT_PUBLIC_KEY_URL / AUTH_JWT_PUBLIC_KEY_FILE")
}

// publicKeyResponse 是 GET /api/system/public-key 的形状（research/001 §2.3）。
type publicKeyResponse struct {
	PublicKey string `json:"publickey"`
}

func fetchPublicKey(url string) ([]byte, error) {
	client := &http.Client{Timeout: publicKeyFetchTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth: 公钥端点返回 %d", resp.StatusCode)
	}
	var body publicKeyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	if body.PublicKey == "" {
		return nil, errors.New("auth: 公钥响应缺少 publickey")
	}
	return []byte(body.PublicKey), nil
}
