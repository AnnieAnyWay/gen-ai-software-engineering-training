package ratelimit

import (
	"testing"
	"time"
)

func TestWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := New(2, time.Minute)
	l.now = func() time.Time { return now }

	for i, want := range []bool{true, true, false} {
		if ok, _, _ := l.Allow("1.2.3.4"); ok != want {
			t.Fatalf("request %d: allowed=%v want %v", i, ok, want)
		}
	}
	if ok, _, _ := l.Allow("5.6.7.8"); !ok {
		t.Fatal("other IPs must have their own limit")
	}
	now = now.Add(time.Minute)
	if ok, rem, _ := l.Allow("1.2.3.4"); !ok || rem != 1 {
		t.Fatalf("after window reset: ok=%v remaining=%d", ok, rem)
	}
	now = now.Add(5 * time.Minute)
	l.Allow("9.9.9.9")
	if len(l.clients) != 1 {
		t.Fatalf("expired buckets not cleaned up: %d", len(l.clients))
	}
}
