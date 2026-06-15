package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// WritePreviousContext persists name as the previous context. The write is
// atomic — content lands in a sibling .tmp file and is then renamed into
// place, so a crash mid-write cannot corrupt the file.
func WritePreviousContext(name string) error {
	if name == "" {
		return errors.New("previous context name cannot be empty")
	}

	path := PreviousContextPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create previous-context dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("write previous-context: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename previous-context: %w", err)
	}
	return nil
}
