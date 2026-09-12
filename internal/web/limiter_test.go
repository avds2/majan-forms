package web

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterWindow(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	for attempt := 1; attempt <= 10; attempt++ {
		if !limiter.Allow("192.0.2.1:41000", "Admin") {
			t.Fatalf("attempt %d was rejected before the limit", attempt)
		}
	}
	if limiter.Allow("192.0.2.1:42000", "admin") {
		t.Fatal("eleventh attempt for the same normalized account was allowed")
	}
	now = now.Add(5 * time.Minute)
	if !limiter.Allow("192.0.2.1:43000", "admin") {
		t.Fatal("new rate-limit window did not open")
	}
}

func TestLoginLimiterConcurrent(t *testing.T) {
	limiter := newLoginLimiter()
	limiter.now = func() time.Time { return time.Unix(1_788_500_000, 0) }
	var allowed atomic.Int32
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			if limiter.Allow("198.51.100.10:50000", "same-account") {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed %d concurrent attempts, want 10", got)
	}
}
