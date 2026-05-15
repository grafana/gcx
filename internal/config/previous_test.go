package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/grafana/gcx/internal/config"
)

func setStateHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Cleanup(func() { xdg.Reload() })
	xdg.Reload()
}

func TestPreviousContext_RoundTrip(t *testing.T) {
	setStateHome(t, t.TempDir())

	got, err := config.ReadPreviousContext()
	if err != nil {
		t.Fatalf("ReadPreviousContext on empty state: unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("ReadPreviousContext on empty state: got %q, want empty string", got)
	}

	if err := config.WritePreviousContext("dev-dev"); err != nil {
		t.Fatalf("WritePreviousContext: %v", err)
	}

	got, err = config.ReadPreviousContext()
	if err != nil {
		t.Fatalf("ReadPreviousContext after write: %v", err)
	}
	if got != "dev-dev" {
		t.Fatalf("ReadPreviousContext: got %q, want %q", got, "dev-dev")
	}
}

func TestPreviousContext_Overwrites(t *testing.T) {
	setStateHome(t, t.TempDir())

	for _, name := range []string{"first", "second", "third"} {
		if err := config.WritePreviousContext(name); err != nil {
			t.Fatalf("WritePreviousContext(%q): %v", name, err)
		}
		got, err := config.ReadPreviousContext()
		if err != nil {
			t.Fatalf("ReadPreviousContext: %v", err)
		}
		if got != name {
			t.Fatalf("ReadPreviousContext after writing %q: got %q", name, got)
		}
	}
}

func TestPreviousContext_RejectsEmpty(t *testing.T) {
	setStateHome(t, t.TempDir())

	if err := config.WritePreviousContext(""); err == nil {
		t.Fatal("WritePreviousContext(\"\"): expected error, got nil")
	}
}

func TestPreviousContext_CreatesStateDir(t *testing.T) {
	stateHome := t.TempDir()
	setStateHome(t, stateHome)

	if err := config.WritePreviousContext("foo"); err != nil {
		t.Fatalf("WritePreviousContext: %v", err)
	}

	gcxDir := filepath.Join(stateHome, "gcx")
	if _, err := os.Stat(gcxDir); err != nil {
		t.Fatalf("expected %s to exist: %v", gcxDir, err)
	}
}

func TestPreviousContext_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	setStateHome(t, dir)

	path := filepath.Join(dir, "gcx", "previous-context")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("  prod\n"), 0o600); err != nil {
		t.Fatalf("seeding file: %v", err)
	}

	got, err := config.ReadPreviousContext()
	if err != nil {
		t.Fatalf("ReadPreviousContext: %v", err)
	}
	if got != "prod" {
		t.Fatalf("ReadPreviousContext: got %q, want %q", got, "prod")
	}
}
