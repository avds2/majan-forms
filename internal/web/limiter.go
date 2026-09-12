package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"forms/internal/normalize"
)

const (
	loginWindow         = 5 * time.Minute
	accountAttemptLimit = 10
	networkAttemptLimit = 1_000
	maximumLoginBuckets = 20_000
)

type loginBucket struct {
	started  time.Time
	attempts int
}

type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]loginBucket
	now     func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{buckets: make(map[string]loginBucket), now: time.Now}
}

func (l *loginLimiter) Allow(remoteAddress, identifier string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	identifierHash := sha256.Sum256([]byte(normalize.LoginIdentifier(identifier)))
	accountKey := "account:" + hex.EncodeToString(identifierHash[:])
	networkKey := "network:" + host
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) >= maximumLoginBuckets {
		l.pruneExpired(now)
	}
	networkAllowed := l.take(networkKey, networkAttemptLimit, now)
	if !networkAllowed {
		return false
	}
	accountAllowed := l.take(accountKey, accountAttemptLimit, now)
	return accountAllowed
}

func (l *loginLimiter) take(key string, limit int, now time.Time) bool {
	bucket, exists := l.buckets[key]
	if !exists && len(l.buckets) >= maximumLoginBuckets {
		return false
	}
	if !exists || now.Sub(bucket.started) >= loginWindow {
		bucket = loginBucket{started: now}
	}
	bucket.attempts++
	l.buckets[key] = bucket
	return bucket.attempts <= limit
}

func (l *loginLimiter) pruneExpired(now time.Time) {
	for key, candidate := range l.buckets {
		if now.Sub(candidate.started) >= loginWindow {
			delete(l.buckets, key)
		}
	}
}

func tooManyRequests(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "300")
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": apiError{Code: "rate_limited", Message: "تلاش‌های ورود بیش از حد بود؛ چند دقیقه بعد دوباره امتحان کنید"}})
}
