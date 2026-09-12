package operations

import (
	"errors"
	"testing"
)

func TestProcessLockSeparatesServiceAndRestore(t *testing.T) {
	directory := t.TempDir()
	serviceLock, err := AcquireProcessLock(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireProcessLock(directory, true); !errors.Is(err, ErrServiceRunning) {
		t.Fatalf("exclusive lock error=%v, want ErrServiceRunning", err)
	}
	if err := serviceLock.Close(); err != nil {
		t.Fatal(err)
	}
	restoreLock, err := AcquireProcessLock(directory, true)
	if err != nil {
		t.Fatalf("acquire exclusive lock after service stopped: %v", err)
	}
	defer restoreLock.Close()
	if _, err := AcquireProcessLock(directory, false); !errors.Is(err, ErrServiceRunning) {
		t.Fatalf("shared lock error=%v, want ErrServiceRunning", err)
	}
}
