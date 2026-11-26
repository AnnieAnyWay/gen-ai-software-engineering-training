package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	tk := NewTokens([]byte("secret-secret-secret-secret-secret"), time.Hour)
	tok, exp := tk.Issue("anna")
	c, err := tk.Verify(tok)
	if err != nil || c.Subject != "anna" || c.ExpiresAt != exp.Unix() {
		t.Fatalf("verify: %v %+v", err, c)
	}
}

func TestTokenRejected(t *testing.T) {
	tk := NewTokens([]byte("secret-secret-secret-secret-secret"), time.Hour)
	tok, _ := tk.Issue("anna")
	parts := strings.Split(tok, ".")

	other := NewTokens([]byte("another-secret-another-secret-xx"), time.Hour)
	forgedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"admin","iss":"banking-api","exp":9999999999}`))
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))

	cases := map[string]string{
		"garbage":         "abc",
		"tampered":        parts[0] + "." + forgedPayload + "." + parts[2],
		"alg none":        none + "." + parts[1] + ".",
		"wrong signature": parts[0] + "." + parts[1] + ".AAAA",
	}
	for name, bad := range cases {
		if _, err := tk.Verify(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := other.Verify(tok); err == nil {
		t.Error("token signed with a different secret was accepted")
	}

	expired := NewTokens([]byte("secret-secret-secret-secret-secret"), time.Hour)
	expired.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	old, _ := expired.Issue("anna")
	if _, err := tk.Verify(old); err != ErrExpiredToken {
		t.Errorf("expired token: got %v", err)
	}
}

func TestPasswords(t *testing.T) {
	u := NewUsers(1000)
	if _, err := u.Register("Anna", "password123"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Authenticate("anna", "password123"); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if _, err := u.Authenticate("anna", "password124"); err != ErrInvalidCredentials {
		t.Fatalf("wrong password: %v", err)
	}
	if u.users["anna"].hash == "password123" || !strings.HasPrefix(u.users["anna"].hash, "pbkdf2-sha256$1000$") {
		t.Fatalf("password not hashed: %q", u.users["anna"].hash)
	}
}
