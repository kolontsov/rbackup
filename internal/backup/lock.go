package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lock represents a held file lock.
type Lock struct {
	file *os.File
	path string
}

// AcquireLock acquires a namespace-scoped lock file.
// If lockWait is 0, uses non-blocking lock (fail immediately).
// Otherwise, retries until lockWait expires.
func AcquireLock(lockDir, namespace string, lockWait time.Duration) (*Lock, error) {
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, fmt.Errorf("creating lock dir: %w", err)
	}

	lockPath := filepath.Join(lockDir, fmt.Sprintf("rbackup.%s.lock", namespace))

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	if lockWait == 0 {
		// Non-blocking
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("lock already held: %s", lockPath)
		}
	} else {
		// Blocking with timeout
		deadline := time.Now().Add(lockWait)
		for {
			err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("lock wait timeout after %v: %s", lockWait, lockPath)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	return &Lock{file: f, path: lockPath}, nil
}

// Release releases the lock.
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}
