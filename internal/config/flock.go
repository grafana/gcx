package config

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

// fileLock is a simple file-based lock using flock(2).
type fileLock struct {
	path string
	f    *os.File
}

// tryLockContext tries to acquire an exclusive lock on the file,
// retrying every interval until the context expires.
func (fl *fileLock) tryLockContext(ctx context.Context, interval time.Duration) error {
	f, err := os.OpenFile(fl.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	fl.f = f

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			fl.f = nil
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// unlock releases the lock and closes the file.
func (fl *fileLock) unlock() error {
	if fl.f == nil {
		return nil
	}
	err := syscall.Flock(int(fl.f.Fd()), syscall.LOCK_UN)
	closeErr := fl.f.Close()
	fl.f = nil
	if err != nil {
		return err
	}
	return closeErr
}
