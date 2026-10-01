package telemetry

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/grafana/gcx/internal/host"
	"github.com/grafana/gcx/internal/xdg"
)

const deviceIDFileName = "device-id"

// DeviceIDPath returns the full path of the persistent device ID file, or ""
// when no state home is known (HOME and XDG_STATE_HOME both unset), so the ID
// file cannot land relative to the current directory. Deleting the file
// resets the ID.
func DeviceIDPath(ctx context.Context) string {
	stateHome := xdg.StateHome(ctx)
	if stateHome == "" {
		return ""
	}
	return filepath.Join(stateHome, "gcx", deviceIDFileName)
}

// DeviceID returns the anonymous per-install device ID and whether it is
// persisted, creating and storing a random UUIDv4 on first use. When the
// state dir is unwritable (or the ID file is corrupt and cannot be
// rewritten) it returns a fresh ephemeral ID with persisted=false, so
// ephemeral IDs can be excluded from install counts.
func DeviceID(ctx context.Context) (string, bool) {
	path := DeviceIDPath(ctx)
	if data, err := host.ReadFile(ctx, path); err == nil {
		if parsed, err := uuid.Parse(strings.TrimSpace(string(data))); err == nil {
			return parsed.String(), true
		}
	}
	fresh, err := uuid.NewRandom()
	if err != nil {
		return "", false
	}
	id := fresh.String()
	// No known state home: nowhere sensible to persist, so stay ephemeral.
	if path == "" {
		return id, false
	}
	if err := host.MkdirAll(ctx, filepath.Dir(path), 0o700); err != nil {
		return id, false
	}
	if err := host.WriteFile(ctx, path, []byte(id+"\n"), 0o600); err != nil {
		return id, false
	}
	return id, true
}
