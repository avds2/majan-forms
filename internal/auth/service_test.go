package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8PreservesValidText(t *testing.T) {
	input := strings.Repeat("الف", 100)
	got := truncateUTF8(input, 255)
	if len(got) > 255 || !utf8.ValidString(got) {
		t.Fatalf("truncateUTF8 returned %d invalid bytes", len(got))
	}
	if !strings.HasPrefix(input, got) {
		t.Fatal("truncateUTF8 did not preserve the input prefix")
	}
}

func TestTruncateUTF8ReplacesInvalidInput(t *testing.T) {
	got := truncateUTF8("agent\xffname", 255)
	if !utf8.ValidString(got) || got != "agent�name" {
		t.Fatalf("truncateUTF8() = %q", got)
	}
}
