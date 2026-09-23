package family

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// InviteTTL 是监护人邀请 token 的有效期（api-contract §3.1：72h）。
const InviteTTL = 72 * time.Hour

// Invite 是邀请 token 解出的内容。
type Invite struct {
	ChildID   uint64
	IssuedBy  uint64
	ExpiresAt time.Time
}

type inviteClaims struct {
	ChildID   uint64 `json:"cid"`
	IssuedBy  uint64 `json:"uid"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func (c *inviteClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}
func (c *inviteClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}
func (c *inviteClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c *inviteClaims) GetIssuer() (string, error)              { return "", nil }
func (c *inviteClaims) GetSubject() (string, error)             { return "", nil }
func (c *inviteClaims) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

// InviteSigner 用 HMAC 密钥签发/校验无状态邀请 token（无需建表，72h 过期）。
type InviteSigner struct{ secret []byte }

func NewInviteSigner(secret string) (*InviteSigner, error) {
	if secret == "" {
		return nil, errors.New("family: INVITE_TOKEN_SECRET 未配置")
	}
	return &InviteSigner{secret: []byte(secret)}, nil
}

func (s *InviteSigner) Issue(childID, issuedBy uint64, ttl time.Duration) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	expiresAt = now.Add(ttl)
	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, &inviteClaims{
		ChildID:   childID,
		IssuedBy:  issuedBy,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
	}).SignedString(s.secret)
	return token, expiresAt, err
}

// Verify 校验邀请 token；过期/篡改/错密钥一律报错。
func (s *InviteSigner) Verify(raw string) (*Invite, error) {
	claims := &inviteClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if !tok.Valid || claims.ChildID == 0 {
		return nil, errors.New("family: 邀请 token 无效")
	}
	return &Invite{
		ChildID:   claims.ChildID,
		IssuedBy:  claims.IssuedBy,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0),
	}, nil
}
