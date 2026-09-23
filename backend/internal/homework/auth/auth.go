// Package auth 负责 STRIDE auth-service 签发的 RS256 JWT 的本地验签。
//
// 校验集复刻 auth-service 的 VerifyAccessToken 并补上它不做的 aud 自校验
// （research/001 §2、§6）：RS256 白名单、iss 白名单、exp 必填、aud 自校验、
// sub/iat 必填。纯本地验签，不查库。
package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 是用户 access token 的 claims（research/001 §2.1，公开 JWT 契约）。
type Claims struct {
	Subject    string   `json:"sub"`
	Audience   string   `json:"aud"`
	Issuer     string   `json:"iss"`
	ExpiresAt  int64    `json:"exp"`
	IssuedAt   int64    `json:"iat"`
	Scopes     []string `json:"scopes,omitempty"`
	Role       string   `json:"role,omitempty"`
	Membership string   `json:"membership,omitempty"`
	UserType   string   `json:"user_type,omitempty"`
	Name       string   `json:"name,omitempty"`
}

func (c *Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	if c.ExpiresAt == 0 {
		return nil, nil
	}
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

func (c *Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	if c.IssuedAt == 0 {
		return nil, nil
	}
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

func (c *Claims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

func (c *Claims) GetIssuer() (string, error) { return c.Issuer, nil }

func (c *Claims) GetSubject() (string, error) { return c.Subject, nil }

func (c *Claims) GetAudience() (jwt.ClaimStrings, error) {
	return jwt.ClaimStrings{c.Audience}, nil
}

type Verifier struct {
	key      *rsa.PublicKey
	issuer   string
	audience string
}

// NewVerifier 解析 PEM 公钥并构建验签器。issuer/audience 必填（fail-closed）。
func NewVerifier(pubPEM []byte, issuer, audience string) (*Verifier, error) {
	if issuer == "" {
		return nil, errors.New("auth: AUTH_JWT_ISSUER 未配置")
	}
	if audience == "" {
		return nil, errors.New("auth: AUTH_JWT_AUDIENCE 未配置")
	}
	key, err := jwt.ParseRSAPublicKeyFromPEM(pubPEM)
	if err != nil {
		return nil, fmt.Errorf("auth: 解析公钥: %w", err)
	}
	return &Verifier{key: key, issuer: issuer, audience: audience}, nil
}

// Verify 校验 token 并返回 claims；任何失败都应映射为 401 invalid_token。
func (v *Verifier) Verify(raw string) (*Claims, error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return v.key, nil },
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("auth: token 无效")
	}
	if claims.Subject == "" || claims.IssuedAt == 0 {
		return nil, errors.New("auth: 缺少 sub/iat")
	}
	return claims, nil
}
