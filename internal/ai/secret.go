package ai

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSecretBytes = 4096

type SecretStore struct{ path string }

func NewSecretStore(dataDir string) *SecretStore {
	return &SecretStore{path: filepath.Join(dataDir, "secrets", "ai-provider.key")}
}

func (s *SecretStore) Save(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > maxSecretBytes || strings.ContainsAny(value, "\r\n\x00") {
		return ErrInvalidSettings
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create AI secret directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect AI secret directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".ai-provider-*")
	if err != nil {
		return fmt.Errorf("stage AI secret: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect staged AI secret: %w", err)
	}
	if _, err := io.WriteString(temporary, value+"\n"); err != nil {
		temporary.Close()
		return fmt.Errorf("write staged AI secret: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync staged AI secret: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close staged AI secret: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("commit AI secret: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open AI secret directory for sync: %w", err)
	}
	if err := directoryHandle.Sync(); err != nil {
		directoryHandle.Close()
		return fmt.Errorf("sync AI secret directory: %w", err)
	}
	if err := directoryHandle.Close(); err != nil {
		return fmt.Errorf("close AI secret directory: %w", err)
	}
	committed = true
	return nil
}

func (s *SecretStore) Read() (string, error) {
	before, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotConfigured
	}
	if err != nil {
		return "", fmt.Errorf("inspect AI secret: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 {
		return "", errors.New("دسترسی فایل secret هوش مصنوعی باید 0600 باشد")
	}
	file, err := os.Open(s.path)
	if err != nil {
		return "", fmt.Errorf("open AI secret: %w", err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect opened AI secret: %w", err)
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&0o077 != 0 {
		return "", errors.New("فایل secret هوش مصنوعی ایمن نیست")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+2))
	if err != nil {
		return "", fmt.Errorf("read AI secret: %w", err)
	}
	value := strings.TrimSpace(string(encoded))
	if len(value) < 8 || len(value) > maxSecretBytes || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("فایل secret هوش مصنوعی معتبر نیست")
	}
	return value, nil
}

func (s *SecretStore) Exists() bool {
	_, err := s.Read()
	return err == nil
}
