package notifier

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

	path, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath() error = %v", err)
	}

	want := filepath.Join("/tmp/xdg-state", "gcx", "notifier.yml")
	if path != want {
		t.Fatalf("StatePath() = %q, want %q", path, want)
	}
}

func TestLoadAndSaveDefaultState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Cleanup(func() { xdg.Reload() })
	xdg.Reload()

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	state := State{Checks: map[string]CheckState{
		"skills": {LastCheckedAt: now},
	}}

	if err := SaveDefaultState(state); err != nil {
		t.Fatalf("SaveDefaultState() error = %v", err)
	}

	loaded, err := LoadDefaultState()
	if err != nil {
		t.Fatalf("LoadDefaultState() error = %v", err)
	}

	got := loaded.Checks["skills"].LastCheckedAt
	if !got.Equal(now) {
		t.Fatalf("loaded timestamp = %v, want %v", got, now)
	}
}
