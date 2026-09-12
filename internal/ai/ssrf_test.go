package ai

import (
	"context"
	"net/netip"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[host], nil
}

func TestValidateEndpointBlocksSSRF(t *testing.T) {
	resolver := staticResolver{
		"public.example":  {netip.MustParseAddr("1.1.1.1")},
		"private.example": {netip.MustParseAddr("10.0.0.8")},
		"mixed.example":   {netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")},
		"loop.example":    {netip.MustParseAddr("127.0.0.1")},
	}
	tests := []struct {
		name         string
		endpoint     string
		allowPrivate bool
		valid        bool
	}{
		{name: "public HTTPS", endpoint: "https://public.example/v1", valid: true},
		{name: "private rejected", endpoint: "https://private.example/v1"},
		{name: "mixed DNS rejected", endpoint: "https://mixed.example/v1"},
		{name: "loopback HTTPS explicit", endpoint: "https://loop.example/v1", allowPrivate: true, valid: true},
		{name: "loopback HTTP explicit", endpoint: "http://loop.example:8080/v1", allowPrivate: true, valid: true},
		{name: "public HTTP remains rejected", endpoint: "http://public.example/v1", allowPrivate: true},
		{name: "userinfo", endpoint: "https://user:pass@public.example/v1"},
		{name: "query", endpoint: "https://public.example/v1?target=private"},
		{name: "trailing dot", endpoint: "https://public.example./v1"},
		{name: "file scheme", endpoint: "file:///etc/passwd"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateEndpoint(context.Background(), test.endpoint, test.allowPrivate, resolver)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
