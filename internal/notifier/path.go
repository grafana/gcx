package notifier

import (
	"context"
	"path/filepath"

	"github.com/grafana/gcx/internal/xdg"
)

const stateFileName = "notifier.yml"

// StatePath returns the notifier state file path under the platform-appropriate
// XDG state home (or its equivalent on non-XDG platforms).
func StatePath(ctx context.Context) string {
	return filepath.Join(xdg.StateHome(ctx), "gcx", stateFileName)
}
