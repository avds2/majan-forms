package web

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestPublicLimiterSeparatesOperationsAndCapsSlug(t *testing.T) {
	t.Parallel()
	limiter := newPublicRequestLimiter()
	limiter.now = func() time.Time { return time.Unix(1_788_500_000, 0) }
	if !limiter.Allow("192.0.2.1", "sample", "start", 1, 2) {
		t.Fatal("first start was rejected")
	}
	if limiter.Allow("192.0.2.1", "sample", "start", 1, 2) {
		t.Fatal("client operation limit was bypassed")
	}
	if !limiter.Allow("192.0.2.1", "sample", "submit", 1, 2) {
		t.Fatal("start requests incorrectly consumed submit quota")
	}
	if !limiter.Allow("192.0.2.2", "sample", "start", 1, 2) {
		t.Fatal("second client was rejected before slug limit")
	}
	if limiter.Allow("192.0.2.3", "sample", "start", 1, 2) {
		t.Fatal("aggregate slug limit was bypassed")
	}
}

func TestRequestRemoteIPTrustsOnlyConfiguredProxyChain(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "http://example.test/f/sample", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.8, 10.0.0.2")
	untrusted := &Server{}
	if got := untrusted.requestRemoteIP(request); got != "127.0.0.1" {
		t.Fatalf("untrusted forwarded address = %q", got)
	}
	trusted := &Server{trustedProxies: []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("10.0.0.0/8"),
	}}
	if got := trusted.requestRemoteIP(request); got != "198.51.100.8" {
		t.Fatalf("trusted proxy chain address = %q", got)
	}
}
