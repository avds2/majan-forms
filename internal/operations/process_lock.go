// Package operations contains host-local coordination primitives used by
// service and maintenance commands.
package operations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var ErrServiceRunning = errors.New("سرویس forms در حال اجراست")

type ProcessLock struct{ file *os.File }

func AcquireProcessLock(dataDir string, exclusive bool) (*ProcessLock, error) {
	file, err := os.OpenFile(filepath.Join(dataDir, "process.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open process lock: %w", err)
	}
	mode := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		mode = syscall.LOCK_EX | syscall.LOCK_NB
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrServiceRunning
		}
		return nil, fmt.Errorf("acquire process lock: %w", err)
	}
	return &ProcessLock{file: file}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
