package notifier //nolint:testpackage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadState_MissingFileReturnsEmptyState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.yml")
	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if state.Checks != nil {
		t.Fatalf("LoadState() checks = %#v, want nil", state.Checks)
	}
}

func TestSaveState_RoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "state.yml")
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	original := State{
		Checks: map[string]CheckState{
			"skills":  {LastCheckedAt: now},
			"version": {LastCheckedAt: now.Add(time.Hour)},
		},
	}

	if err := SaveState(path, original); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file perms = %v, want 0600", got)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if len(loaded.Checks) != 2 {
		t.Fatalf("len(loaded.Checks) = %d, want 2", len(loaded.Checks))
	}
	if got := loaded.Checks["skills"].LastCheckedAt; !got.Equal(now) {
		t.Fatalf("loaded skills timestamp = %v, want %v", got, now)
	}
	if got := loaded.Checks["version"].LastCheckedAt; !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("loaded version timestamp = %v, want %v", got, now.Add(time.Hour))
	}
}

func TestShouldRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	interval := 24 * time.Hour

	tests := []struct {
		name     string
		state    State
		key      string
		interval time.Duration
		want     bool
	}{
		{
			name:     "missing key is due",
			state:    State{},
			key:      "skills",
			interval: interval,
			want:     true,
		},
		{
			name: "zero timestamp is due",
			state: State{Checks: map[string]CheckState{
				"skills": {},
			}},
			key:      "skills",
			interval: interval,
			want:     true,
		},
		{
			name: "non-positive interval is always due",
			state: State{Checks: map[string]CheckState{
				"skills": {LastCheckedAt: now},
			}},
			key:      "skills",
			interval: 0,
			want:     true,
		},
		{
			name: "before interval is not due",
			state: State{Checks: map[string]CheckState{
				"skills": {LastCheckedAt: now.Add(-23 * time.Hour)},
			}},
			key:      "skills",
			interval: interval,
			want:     false,
		},
		{
			name: "exactly at interval is due",
			state: State{Checks: map[string]CheckState{
				"skills": {LastCheckedAt: now.Add(-24 * time.Hour)},
			}},
			key:      "skills",
			interval: interval,
			want:     true,
		},
		{
			name: "after interval is due",
			state: State{Checks: map[string]CheckState{
				"skills": {LastCheckedAt: now.Add(-25 * time.Hour)},
			}},
			key:      "skills",
			interval: interval,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ShouldRun(tt.state, tt.key, now, tt.interval); got != tt.want {
				t.Fatalf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMarkRan_InitializesChecksMap(t *testing.T) {
	t.Parallel()

	var state State
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	MarkRan(&state, "skills", now)

	if state.Checks == nil {
		t.Fatal("state.Checks is nil")
	}
	if got := state.Checks["skills"].LastCheckedAt; !got.Equal(now) {
		t.Fatalf("skills timestamp = %v, want %v", got, now)
	}
}
