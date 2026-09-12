package ai

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretStoreUsesPrivateAtomicFile(t *testing.T) {
	dataDir := t.TempDir()
	store := NewSecretStore(dataDir)
	if err := store.Save("sk-private-test-value"); err != nil {
		t.Fatalf("save secret: %v", err)
	}
	info, err := os.Stat(filepath.Join(dataDir, "secrets", "ai-provider.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode=%o want=600", info.Mode().Perm())
	}
	value, err := store.Read()
	if err != nil || value != "sk-private-test-value" {
		t.Fatalf("read secret=%q error=%v", value, err)
	}
	if err := os.Chmod(filepath.Join(dataDir, "secrets", "ai-provider.key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(); err == nil || errors.Is(err, ErrNotConfigured) {
		t.Fatalf("insecure permissions were not rejected: %v", err)
	}
}

func TestSecretStoreRejectsMalformedValue(t *testing.T) {
	store := NewSecretStore(t.TempDir())
	for _, value := range []string{"short", "valid-key\nsecond-line", string(make([]byte, maxSecretBytes+1))} {
		if err := store.Save(value); !errors.Is(err, ErrInvalidSettings) {
			t.Fatalf("malformed secret length=%d error=%v", len(value), err)
		}
	}
}
