// Package ratelimit implements a per-client fixed-window rate limiter.
package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter allows `limit` requests per `window` for each client IP.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]*bucket
	now     func() time.Time
	lastGC  time.Time
}

type bucket struct {
	count int
	reset time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, clients: map[string]*bucket{}, now: time.Now}
}

// Allow records one request from key and reports whether it is allowed,
// how many requests remain, and when the window resets.
func (l *Limiter) Allow(key string) (ok bool, remaining int, reset time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	b := l.clients[key]
	if b == nil || !now.Before(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.clients[key] = b
	}
	if b.count >= l.limit {
		return false, 0, b.reset
	}
	b.count++
	return true, l.limit - b.count, b.reset
}

// gc drops expired buckets at most once per window so memory stays bounded.
func (l *Limiter) gc(now time.Time) {
	if now.Sub(l.lastGC) < l.window {
		return
	}
	l.lastGC = now
	for k, b := range l.clients {
		if !now.Before(b.reset) {
			delete(l.clients, k)
		}
	}
}

// Middleware applies the limit per client IP and sets X-RateLimit-* headers.
// The IP comes from the TCP connection, not X-Forwarded-For, which clients can fake.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, remaining, reset := l.Allow(clientIP(r))
		h := w.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(l.limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		if !ok {
			secs := int(time.Until(reset).Seconds()) + 1
			h.Set("Retry-After", strconv.Itoa(secs))
			h.Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "Too many requests",
				"retryAfterSeconds": secs,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
