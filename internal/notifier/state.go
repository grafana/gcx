package notifier

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/gcx/internal/host"
	"gopkg.in/yaml.v3"
)

// State stores per-check timestamps for throttled notifications.
type State struct {
	Checks map[string]CheckState `yaml:"checks,omitempty"`
}

// CheckState stores the last successful run time for one named check.
type CheckState struct {
	LastCheckedAt time.Time `yaml:"last_checked_at,omitempty"`
}

// LoadState reads notifier state from path. Missing files and corrupt YAML
// both yield an empty state — the state is non-critical UX bookkeeping, so
// self-healing avoids permanently silencing the notifier on a partial write.
func LoadState(ctx context.Context, path string) (State, error) {
	data, err := host.ReadFile(ctx, path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read notifier state %q: %w", path, err)
	}

	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		// Self-heal: a corrupt state file is non-critical UX bookkeeping. Returning
		// an error here would propagate up and silently disable the notifier
		// (the call site discards errors), so prefer treating it as missing state.
		return State{}, nil //nolint:nilerr // Self-heal corrupt state — see comment above.
	}
	return state, nil
}

// SaveState writes notifier state to path atomically, creating parent
// directories as needed. The write goes through a sibling .tmp file followed
// by os.Rename so a crash mid-write cannot leave a corrupt state file.
func SaveState(ctx context.Context, path string, state State) error {
	if err := host.MkdirAll(ctx, filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create notifier state dir for %q: %w", path, err)
	}

	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal notifier state %q: %w", path, err)
	}

	tmp := path + ".tmp"
	if err := host.WriteFile(ctx, tmp, data, 0o600); err != nil {
		return fmt.Errorf("write notifier state %q: %w", path, err)
	}
	if err := host.Rename(ctx, tmp, path); err != nil {
		_ = host.Remove(ctx, tmp)
		return fmt.Errorf("rename notifier state %q: %w", path, err)
	}
	return nil
}

// ShouldRun reports whether the named check is due at now for the given interval.
// Missing state, missing keys, zero timestamps, and non-positive intervals are all treated as due.
func ShouldRun(state State, key string, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}

	check, ok := state.Checks[key]
	if !ok || check.LastCheckedAt.IsZero() {
		return true
	}

	return !now.Before(check.LastCheckedAt.Add(interval))
}

// MarkRan records a successful run time for the named check.
func MarkRan(state *State, key string, now time.Time) {
	if state.Checks == nil {
		state.Checks = map[string]CheckState{}
	}
	state.Checks[key] = CheckState{LastCheckedAt: now}
}
