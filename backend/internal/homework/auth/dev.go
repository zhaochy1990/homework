package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateDevKeyPair 生成 RS256 开发密钥对（PKCS#1 私钥 + PKIX 公钥）。
// 仅供本地开发与测试，生产密钥由 STRIDE auth-service 持有。
func GenerateDevKeyPair() (privPEM, pubPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	privPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return privPEM, pubPEM, nil
}

// SignDevToken 用私钥签发一枚 RS256 access token，仅供本地开发/测试
// （cmd/gentoken）。
func SignDevToken(privPEM []byte, claims Claims) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privPEM)
	if err != nil {
		return "", err
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, &claims).SignedString(key)
}
