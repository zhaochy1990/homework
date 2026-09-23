package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestVerifier(t *testing.T) (*Verifier, []byte) {
	t.Helper()
	priv, pub, err := GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(pub, "auth-service", "app_test")
	if err != nil {
		t.Fatal(err)
	}
	return v, priv
}

func validClaimsPtr() *Claims {
	c := validClaims()
	return &c
}

func validClaims() Claims {
	now := time.Now()
	return Claims{
		Subject:   "user-1",
		Audience:  "app_test",
		Issuer:    "auth-service",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Hour).Unix(),
		Name:      "小明",
		Scopes:    []string{"openid"},
		Role:      "user",
	}
}

func TestVerifyValidToken(t *testing.T) {
	v, priv := newTestVerifier(t)
	token, err := SignDevToken(priv, validClaims())
	if err != nil {
		t.Fatal(err)
	}
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-1" || claims.Name != "小明" || claims.Audience != "app_test" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestVerifyRejects(t *testing.T) {
	v, priv := newTestVerifier(t)

	expired := validClaims()
	expired.ExpiresAt = time.Now().Add(-time.Minute).Unix()

	wrongAud := validClaims()
	wrongAud.Audience = "other_app"

	wrongIss := validClaims()
	wrongIss.Issuer = "evil"

	noExp := validClaims()
	noExp.ExpiresAt = 0

	noIat := validClaims()
	noIat.IssuedAt = 0

	otherPriv, _, err := GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	hs256, err := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaimsPtr()).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{}
	if tok, err := SignDevToken(priv, expired); err == nil {
		cases["expired"] = tok
	} else {
		t.Fatal(err)
	}
	for name, claims := range map[string]Claims{"wrong_aud": wrongAud, "wrong_iss": wrongIss, "no_exp": noExp, "no_iat": noIat} {
		tok, err := SignDevToken(priv, claims)
		if err != nil {
			t.Fatal(err)
		}
		cases[name] = tok
	}
	if tok, err := SignDevToken(otherPriv, validClaims()); err == nil {
		cases["wrong_key"] = tok
	} else {
		t.Fatal(err)
	}
	cases["hs256_alg"] = hs256
	cases["garbage"] = "not-a-jwt"

	for name, token := range cases {
		if _, err := v.Verify(token); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestNewVerifierRequiresIssuerAndAudience(t *testing.T) {
	_, pub, err := GenerateDevKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier(pub, "", "app_test"); err == nil {
		t.Error("empty issuer should fail")
	}
	if _, err := NewVerifier(pub, "auth-service", ""); err == nil {
		t.Error("empty audience should fail")
	}
	if _, err := NewVerifier([]byte("not pem"), "auth-service", "app_test"); err == nil {
		t.Error("bad PEM should fail")
	}
}
