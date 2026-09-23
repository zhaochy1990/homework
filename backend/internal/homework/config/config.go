// Package config 从仓库根 .env 与环境变量加载后端配置。
//
// 优先级：真实环境变量 > .env 文件 > 内置默认值。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Env      string
	HTTPAddr string
	DB       DB
	Auth     Auth
}

type DB struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

// Auth 是 STRIDE auth-service 的本地验签配置。
type Auth struct {
	Issuer        string // JWT iss，固定 auth-service
	Audience      string // JWT aud，= 本小程序 application 的 client_id
	PublicKeyURL  string // GET {"publickey": PEM}
	PublicKeyFile string // URL 不可达时的本地 PEM fallback
}

// DSN 使用 UTC 连接时区；日期列在应用层以 UTC 零点表示 CST 日历日。
func (d DB) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

// Load 读取 .env 并组装配置。
func Load() (*Config, error) {
	dot := map[string]string{}
	if path := findDotEnv(); path != "" {
		var err error
		dot, err = parseDotEnvFile(path)
		if err != nil {
			return nil, err
		}
	}
	return build(dot, os.LookupEnv)
}

// build 合并 .env 与真实环境变量；lookup 供测试注入。
func build(dot map[string]string, lookup func(string) (string, bool)) (*Config, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && v != "" {
			return v
		}
		if v, ok := dot[key]; ok && v != "" {
			return v
		}
		return def
	}
	return &Config{
		Env:      get("ENV", "dev"),
		HTTPAddr: get("HTTP_ADDR", ":8080"),
		DB: DB{
			Host:     get("DB_HOST", "127.0.0.1"),
			Port:     get("DB_PORT", "3306"),
			User:     get("DB_USER", "homework"),
			Password: get("DB_PASSWORD", "homework"),
			Name:     get("DB_NAME", "homework"),
		},
		Auth: Auth{
			Issuer:        get("AUTH_JWT_ISSUER", "auth-service"),
			Audience:      get("AUTH_JWT_AUDIENCE", ""),
			PublicKeyURL:  get("AUTH_JWT_PUBLIC_KEY_URL", ""),
			PublicKeyFile: get("AUTH_JWT_PUBLIC_KEY_FILE", ""),
		},
	}, nil
}

// findDotEnv 自当前目录逐级向上查找 .env。
func findDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		p := filepath.Join(dir, ".env")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		m[key] = val
	}
	return m, sc.Err()
}
