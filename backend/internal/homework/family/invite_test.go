package family

import (
	"testing"
	"time"
)

func TestInviteRoundTrip(t *testing.T) {
	s, err := NewInviteSigner("secret")
	if err != nil {
		t.Fatal(err)
	}
	token, expiresAt, err := s.Issue(42, 7, InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if inv.ChildID != 42 || inv.IssuedBy != 7 {
		t.Fatalf("unexpected invite: %+v", inv)
	}
	if !expiresAt.After(time.Now().Add(71 * time.Hour)) {
		t.Fatalf("expiry too soon: %v", expiresAt)
	}
}

func TestInviteRejects(t *testing.T) {
	s, err := NewInviteSigner("secret")
	if err != nil {
		t.Fatal(err)
	}
	valid, _, err := s.Issue(1, 1, InviteTTL)
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := s.Issue(1, 1, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewInviteSigner("other-secret")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]func() (*Invite, error){
		"garbage":     func() (*Invite, error) { return s.Verify("not-a-token") },
		"tampered":    func() (*Invite, error) { return s.Verify(valid + "x") },
		"expired":     func() (*Invite, error) { return s.Verify(expired) },
		"wrong_key":   func() (*Invite, error) { return other.Verify(valid) },
		"empty_token": func() (*Invite, error) { return s.Verify("") },
	}
	for name, fn := range cases {
		if _, err := fn(); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestNewInviteSignerRequiresSecret(t *testing.T) {
	if _, err := NewInviteSigner(""); err == nil {
		t.Fatal("empty secret should fail")
	}
}
