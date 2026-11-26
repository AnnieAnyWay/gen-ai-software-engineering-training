package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type ctxKey struct{}

// Require rejects requests without a valid "Authorization: Bearer <token>"
// header and stores the username in the request context.
func (t *Tokens) Require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || raw == "" {
			unauthorized(w, "Missing bearer token")
			return
		}
		c, err := t.Verify(strings.TrimSpace(raw))
		if err != nil {
			msg := "Invalid token"
			if errors.Is(err, ErrExpiredToken) {
				msg = "Token expired"
			}
			unauthorized(w, msg)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, c.Subject)))
	}
}

// UserFrom returns the authenticated username set by Require.
func UserFrom(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="banking-api"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
