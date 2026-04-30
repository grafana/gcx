package notifier

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

const stateFileName = "notifier.yml"

// StatePath returns the notifier state file path under the platform-appropriate
// XDG state home (or its equivalent on non-XDG platforms).
func StatePath() string {
	return filepath.Join(xdg.StateHome, "gcx", stateFileName)
}
