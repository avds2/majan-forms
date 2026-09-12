package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
	"sync"
	"time"

	"forms/internal/normalize"
)

type publicLimitBucket struct {
	started time.Time
	count   int
}

type publicRequestLimiter struct {
	mu      sync.Mutex
	buckets map[string]publicLimitBucket
	now     func() time.Time
}

func newPublicRequestLimiter() *publicRequestLimiter {
	return &publicRequestLimiter{buckets: make(map[string]publicLimitBucket), now: time.Now}
}

func (l *publicRequestLimiter) Allow(remoteAddress, slug, operation string, clientLimit, slugLimit int) bool {
	if clientLimit < 1 || slugLimit < 1 {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	normalizedSlug := normalize.Slug(slug)
	clientDigest := sha256.Sum256([]byte("client|" + strings.TrimSpace(host) + "|" + normalizedSlug + "|" + operation))
	slugDigest := sha256.Sum256([]byte("slug|" + normalizedSlug + "|" + operation))
	clientKey := hex.EncodeToString(clientDigest[:])
	slugKey := hex.EncodeToString(slugDigest[:])
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) >= 20_000 {
		for candidate, bucket := range l.buckets {
			if now.Sub(bucket.started) >= time.Minute {
				delete(l.buckets, candidate)
			}
		}
		if len(l.buckets) >= 20_000 {
			_, clientExists := l.buckets[clientKey]
			_, slugExists := l.buckets[slugKey]
			if !clientExists || !slugExists {
				return false
			}
		}
	}
	if !l.take(clientKey, clientLimit, now) {
		return false
	}
	return l.take(slugKey, slugLimit, now)
}

func (l *publicRequestLimiter) take(key string, limit int, now time.Time) bool {
	bucket := l.buckets[key]
	if bucket.started.IsZero() || now.Sub(bucket.started) >= time.Minute {
		bucket = publicLimitBucket{started: now}
	}
	bucket.count++
	l.buckets[key] = bucket
	return bucket.count <= limit
}
