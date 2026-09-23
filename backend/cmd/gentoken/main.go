// Command gentoken 是本地开发用的 JWT 工具：
//
//	# 生成开发密钥对
//	go run ./cmd/gentoken -gen ./dev-keys
//	# 用私钥签一枚测试 token
//	go run ./cmd/gentoken -key ./dev-keys/private.pem -sub test-user
//
// 生产 token 由 STRIDE auth-service 签发，本工具只用于本地联调。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhaochy1990/homework/backend/internal/homework/auth"
)

func main() {
	genDir := flag.String("gen", "", "生成开发密钥对到该目录后退出")
	keyPath := flag.String("key", os.Getenv("AUTH_JWT_PRIVATE_KEY_FILE"), "私钥 PEM 路径")
	sub := flag.String("sub", "dev-user", "JWT sub")
	aud := flag.String("aud", os.Getenv("AUTH_JWT_AUDIENCE"), "JWT aud")
	iss := flag.String("iss", envOr("AUTH_JWT_ISSUER", "auth-service"), "JWT iss")
	ttl := flag.Duration("ttl", time.Hour, "有效期")
	role := flag.String("role", "user", "role")
	name := flag.String("name", "", "昵称（用户 upsert 时写入）")
	scopes := flag.String("scopes", "", "逗号分隔的 scopes")
	flag.Parse()

	if *genDir != "" {
		if err := generate(*genDir); err != nil {
			log.Fatal(err)
		}
		return
	}

	if *keyPath == "" {
		log.Fatal("缺少私钥：用 -key 指定，或先 -gen 生成并设置 AUTH_JWT_PRIVATE_KEY_FILE")
	}
	if *aud == "" {
		log.Fatal("缺少 aud：用 -aud 指定，或设置 AUTH_JWT_AUDIENCE")
	}
	privPEM, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatal(err)
	}

	now := time.Now()
	claims := auth.Claims{
		Subject:   *sub,
		Audience:  *aud,
		Issuer:    *iss,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(*ttl).Unix(),
		Role:      *role,
		Name:      *name,
	}
	if *scopes != "" {
		claims.Scopes = strings.Split(*scopes, ",")
	}

	token, err := auth.SignDevToken(privPEM, claims)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(token)
}

func generate(dir string) error {
	privPEM, pubPEM, err := auth.GenerateDevKeyPair()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	privPath := filepath.Join(dir, "private.pem")
	pubPath := filepath.Join(dir, "public.pem")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return err
	}
	fmt.Printf("生成开发密钥对：\n  私钥 %s\n  公钥 %s\n", privPath, pubPath)
	fmt.Printf("验签后端请设置 AUTH_JWT_PUBLIC_KEY_FILE=%s（并清空 AUTH_JWT_PUBLIC_KEY_URL）\n", pubPath)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
