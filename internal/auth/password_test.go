package auth

import (
	"errors"
	"testing"
)

func testPasswordParams() PasswordParams {
	return PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

func TestPasswordRoundTrip(t *testing.T) {
	t.Parallel()
	hash, err := HashPassword("a-correct-horse-battery", testPasswordParams())
	if err != nil {
		t.Fatal(err)
	}
	valid, parsed, err := VerifyPassword("a-correct-horse-battery", hash)
	if err != nil || !valid {
		t.Fatalf("VerifyPassword() = %v, %v", valid, err)
	}
	if parsed != testPasswordParams() {
		t.Fatalf("parsed params = %#v", parsed)
	}
	valid, _, err = VerifyPassword("a-wrong-password", hash)
	if err != nil || valid {
		t.Fatalf("wrong password = %v, %v", valid, err)
	}
}

func TestPasswordValidation(t *testing.T) {
	t.Parallel()
	if err := ValidatePassword("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("ValidatePassword() error = %v", err)
	}
	if _, _, err := VerifyPassword("anything-long-enough", "$not-a-hash"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
}
