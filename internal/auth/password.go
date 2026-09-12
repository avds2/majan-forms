package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var (
	ErrPasswordTooShort = errors.New("رمز عبور باید دست‌کم ۱۲ نویسه باشد")
	ErrPasswordTooLong  = errors.New("رمز عبور بیش از حد بلند است")
	ErrInvalidHash      = errors.New("قالب hash رمز عبور معتبر نیست")
)

type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func DefaultPasswordParams() PasswordParams {
	return PasswordParams{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

func HashPassword(password string, params PasswordParams) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.Memory,
		params.Iterations,
		params.Parallelism,
		b64.EncodeToString(salt),
		b64.EncodeToString(key),
	), nil
}

func VerifyPassword(password, encodedHash string) (bool, PasswordParams, error) {
	params, salt, expected, err := parsePasswordHash(encodedHash)
	if err != nil {
		return false, PasswordParams{}, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, params, nil
}

func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < 12 {
		return ErrPasswordTooShort
	}
	if len(password) > 256 {
		return ErrPasswordTooLong
	}
	return nil
}

func NeedsRehash(current, preferred PasswordParams) bool {
	return current != preferred
}

func BenchmarkPasswordHash(params PasswordParams) time.Duration {
	salt := make([]byte, params.SaltLength)
	started := time.Now()
	argon2.IDKey([]byte("forms-startup-benchmark-only"), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return time.Since(started)
}

func parsePasswordHash(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, ErrInvalidHash
	}
	params := PasswordParams{}
	for _, part := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return PasswordParams{}, nil, nil, ErrInvalidHash
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return PasswordParams{}, nil, nil, ErrInvalidHash
		}
		switch key {
		case "m":
			params.Memory = uint32(parsed)
		case "t":
			params.Iterations = uint32(parsed)
		case "p":
			if parsed > 255 {
				return PasswordParams{}, nil, nil, ErrInvalidHash
			}
			params.Parallelism = uint8(parsed)
		default:
			return PasswordParams{}, nil, nil, ErrInvalidHash
		}
	}
	if params.Memory < 8*1024 || params.Memory > 1024*1024 || params.Iterations < 1 || params.Iterations > 20 || params.Parallelism < 1 || params.Parallelism > 16 {
		return PasswordParams{}, nil, nil, ErrInvalidHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return PasswordParams{}, nil, nil, ErrInvalidHash
	}
	expected, err := b64.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 128 {
		return PasswordParams{}, nil, nil, ErrInvalidHash
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(expected))
	return params, salt, expected, nil
}
