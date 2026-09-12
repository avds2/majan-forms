package ai

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func ValidateEndpoint(ctx context.Context, raw string, allowPrivate bool, resolver Resolver) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 2048 || strings.TrimSpace(raw) != raw {
		return nil, ErrUnsafeEndpoint
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, ErrUnsafeEndpoint
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, ErrUnsafeEndpoint
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.HasSuffix(hostname, ".") || strings.Contains(hostname, "%") {
		return nil, ErrUnsafeEndpoint
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("%w: نام میزبان resolve نشد", ErrUnsafeEndpoint)
	}
	allLoopback := true
	for _, address := range addresses {
		if !address.IsLoopback() {
			allLoopback = false
		}
		if blockedAddress(address) && !allowPrivate {
			return nil, ErrUnsafeEndpoint
		}
	}
	if parsed.Scheme == "http" && (!allowPrivate || !allLoopback) {
		return nil, ErrUnsafeEndpoint
	}
	return parsed, nil
}

func blockedAddress(address netip.Addr) bool {
	if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, prefix := range blockedNetworkPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

var blockedNetworkPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}
