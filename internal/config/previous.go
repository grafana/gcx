package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/gcx/internal/xdg"
)

const previousContextFileName = "previous-context"

// PreviousContextPath returns the file path where the previous context name
// is persisted. The file lives under the platform-appropriate XDG state
// directory and is created on demand by WritePreviousContext.
func PreviousContextPath() string {
	return filepath.Join(xdg.StateHome(), "gcx", previousContextFileName)
}

// ReadPreviousContext returns the previous context name as last persisted by
// WritePreviousContext. A missing file yields an empty string and a nil error
// — callers treat the absence of history as a normal first-run state.
//
// Reads are unlocked: WritePreviousContext swaps the file in via an atomic
// rename, so a concurrent read always observes either the complete old or the
// complete new file, never a partial write.
func ReadPreviousContext() (string, error) {
	data, err := os.ReadFile(PreviousContextPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read previous context: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// WritePreviousContext persists name as the previous context.
//
// Concurrent writers are guarded on two levels. An advisory flock (matching the
// config token-persistence lock in rest.go) serializes writers on the same
// host, so only one is in the critical section at a time. The payload then
// lands in a per-writer unique temp file that is atomically renamed into place;
// this protects against a writer that cannot honor the advisory lock (e.g. an
// older gcx during a rollout) clobbering an in-flight temp file. The worst case
// under contention is benign last-writer-wins on a single-value bookmark.
//
// The lock acquisition is best-effort with a short timeout: callers treat a
// failure here as a non-fatal warning, so a context switch is never blocked.
func WritePreviousContext(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("previous context name cannot be empty")
	}

	path := PreviousContextPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create previous-context dir: %w", err)
	}

	lock := flock.New(path + ".lock")
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if ok, err := lock.TryLockContext(lockCtx, 50*time.Millisecond); err != nil || !ok {
		if err != nil {
			return fmt.Errorf("lock previous-context: %w", err)
		}
		return errors.New("lock previous-context: timed out")
	}
	defer func() { _ = lock.Unlock() }()

	// os.CreateTemp creates the file with 0o600 perms, matching the previous
	// fixed-name write.
	tmp, err := os.CreateTemp(dir, previousContextFileName+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create previous-context temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.WriteString(name + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write previous-context: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close previous-context temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename previous-context: %w", err)
	}
	return nil
}
