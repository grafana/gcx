package notifier //nolint:testpackage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
)

func TestStatePath_UsesXDGStateHomeWhenSet(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	t.Cleanup(func() { xdg.Reload() })
	xdg.Reload()

	path := StatePath()
	want := filepath.Join("/tmp/xdg-state", "gcx", "notifier.yml")
	if path != want {
		t.Fatalf("StatePath() = %q, want %q", path, want)
	}
}

func TestStatePath_RoundTripsStateViaLoadAndSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Cleanup(func() { xdg.Reload() })
	xdg.Reload()

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	state := State{Checks: map[string]CheckState{
		"skills": {LastCheckedAt: now},
	}}

	path := StatePath()
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}

	got := loaded.Checks["skills"].LastCheckedAt
	if !got.Equal(now) {
		t.Fatalf("loaded timestamp = %v, want %v", got, now)
	}
}
