// Package auth provides user registration, password hashing and JWT (HS256)
// authentication built on the Go standard library.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

const issuer = "banking-api"

// Claims is the JWT payload.
type Claims struct {
	Subject   string `json:"sub"` // username
	Issuer    string `json:"iss"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Tokens issues and verifies HS256 JWTs.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokens(secret []byte, ttl time.Duration) *Tokens {
	return &Tokens{secret: secret, ttl: ttl, now: time.Now}
}

var b64 = base64.RawURLEncoding

// fixed header: {"alg":"HS256","typ":"JWT"}
var header = b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Issue returns a signed token for username and its expiry time.
func (t *Tokens) Issue(username string) (string, time.Time) {
	now := t.now()
	exp := now.Add(t.ttl)
	payload, _ := json.Marshal(Claims{Subject: username, Issuer: issuer, IssuedAt: now.Unix(), ExpiresAt: exp.Unix()})
	unsigned := header + "." + b64.EncodeToString(payload)
	return unsigned + "." + t.sign(unsigned), exp
}

// Verify checks signature, algorithm, issuer and expiry, and returns the claims.
func (t *Tokens) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	// Only accept HS256: blocks "alg":"none" and algorithm-confusion attacks.
	var h struct {
		Alg string `json:"alg"`
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &h) != nil || h.Alg != "HS256" {
		return Claims{}, ErrInvalidToken
	}
	if !hmac.Equal([]byte(parts[2]), []byte(t.sign(parts[0]+"."+parts[1]))) {
		return Claims{}, ErrInvalidToken
	}
	var c Claims
	pb, err := b64.DecodeString(parts[1])
	if err != nil || json.Unmarshal(pb, &c) != nil || c.Subject == "" || c.Issuer != issuer {
		return Claims{}, ErrInvalidToken
	}
	if t.now().Unix() >= c.ExpiresAt {
		return Claims{}, ErrExpiredToken
	}
	return c, nil
}

func (t *Tokens) sign(unsigned string) string {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte(unsigned))
	return b64.EncodeToString(m.Sum(nil))
}
