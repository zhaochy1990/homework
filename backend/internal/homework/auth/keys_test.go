package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
)

func TestLoadVerifierFromURL(t *testing.T) {
	priv, pub, err := GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"publickey": string(pub)})
	}))
	defer srv.Close()

	v, err := LoadVerifier(config.Auth{Issuer: "auth-service", Audience: "app_test", PublicKeyURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignDevToken(priv, validClaims())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(token); err != nil {
		t.Fatalf("URL-fetched key should verify: %v", err)
	}
}

func TestLoadVerifierFallsBackToFile(t *testing.T) {
	priv, pub, err := GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(path, pub, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Auth{
		Issuer:        "auth-service",
		Audience:      "app_test",
		PublicKeyURL:  "http://127.0.0.1:1/public-key", // 不可达
		PublicKeyFile: path,
	}
	v, err := LoadVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token, err := SignDevToken(priv, validClaims())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(token); err != nil {
		t.Fatalf("fallback file key should verify: %v", err)
	}
}

func TestLoadVerifierNeedsKeySource(t *testing.T) {
	if _, err := LoadVerifier(config.Auth{Issuer: "auth-service", Audience: "app_test"}); err == nil {
		t.Fatal("expected error with no key source")
	}
}
