package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDotEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\nDB_HOST=127.0.0.1\n\nexport DB_NAME=\"homework\"\nEMPTY=\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := parseDotEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if m["DB_HOST"] != "127.0.0.1" || m["DB_NAME"] != "homework" {
		t.Fatalf("unexpected map: %#v", m)
	}
	if _, ok := m["# comment"]; ok {
		t.Fatal("comment should be skipped")
	}
}

func TestBuildPrecedence(t *testing.T) {
	dot := map[string]string{"DB_NAME": "from_dot", "DB_USER": "dotuser", "AUTH_JWT_AUDIENCE": "app_dot"}
	lookup := func(k string) (string, bool) {
		if k == "DB_NAME" {
			return "from_env", true
		}
		return "", false
	}
	cfg, err := build(dot, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Name != "from_env" {
		t.Fatalf("env should win, got %q", cfg.DB.Name)
	}
	if cfg.DB.User != "dotuser" {
		t.Fatalf(".env fallback failed, got %q", cfg.DB.User)
	}
	if cfg.HTTPAddr != ":8080" || cfg.Env != "dev" {
		t.Fatalf("defaults missing: %+v", cfg)
	}
	if cfg.Auth.Issuer != "auth-service" || cfg.Auth.Audience != "app_dot" {
		t.Fatalf("auth config wrong: %+v", cfg.Auth)
	}
}

func TestDSN(t *testing.T) {
	d := DB{Host: "db", Port: "3306", User: "u", Password: "p", Name: "n"}
	want := "u:p@tcp(db:3306)/n?charset=utf8mb4&parseTime=True&loc=UTC"
	if got := d.DSN(); got != want {
		t.Fatalf("DSN = %q, want %q", got, want)
	}
}

func TestDeepSeekDefaultsAndOverride(t *testing.T) {
	cfg, err := build(nil, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.DeepSeek
	if d.BaseURL != "https://api.deepseek.com" || d.Model != "deepseek-flash" || d.HardModel != "deepseek-v4-pro" {
		t.Fatalf("defaults wrong: %+v", d)
	}
	if d.Timeout != 30*time.Second || d.APIKey != "" {
		t.Fatalf("defaults wrong: %+v", d)
	}

	dot := map[string]string{
		"DEEPSEEK_API_KEY":    "sk-dot",
		"DEEPSEEK_MODEL":      "m-dot",
		"DEEPSEEK_MODEL_HARD": "hard-dot",
		"DEEPSEEK_TIMEOUT":    "45s",
	}
	lookup := func(k string) (string, bool) {
		if k == "DEEPSEEK_BASE_URL" {
			return "http://stub", true
		}
		return "", false
	}
	cfg, err = build(dot, lookup)
	if err != nil {
		t.Fatal(err)
	}
	d = cfg.DeepSeek
	if d.BaseURL != "http://stub" || d.APIKey != "sk-dot" || d.Model != "m-dot" ||
		d.HardModel != "hard-dot" || d.Timeout != 45*time.Second {
		t.Fatalf("override wrong: %+v", d)
	}
}
