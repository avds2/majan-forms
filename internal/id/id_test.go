package id

import (
	"regexp"
	"testing"
)

func TestNewUUID(t *testing.T) {
	t.Parallel()
	got, err := New()
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(got) {
		t.Fatalf("New() = %q, want a v4 UUID", got)
	}
}

func TestTokenMinimumEntropy(t *testing.T) {
	t.Parallel()
	if _, err := Token(8); err == nil {
		t.Fatal("Token(8) unexpectedly succeeded")
	}
	a, err := Token(32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Token(32)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) < 40 {
		t.Fatalf("tokens are not suitably random: %q %q", a, b)
	}
}
