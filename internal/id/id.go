// Package id creates opaque random identifiers without relying on a database
// extension or a third-party UUID implementation.
package id

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

// New returns a random RFC 4122 version 4 UUID.
func New() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded[:]), nil
}

// Token returns a URL-safe random token with the requested entropy in bytes.
func Token(bytes int) (string, error) {
	if bytes < 16 {
		return "", errors.New("token entropy must be at least 16 bytes")
	}
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
