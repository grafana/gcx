package notifier

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

const stateFileName = "notifier.yml"

// StatePath returns the notifier state file path under the platform-appropriate
// XDG state home (or its equivalent on non-XDG platforms).
func StatePath() (string, error) {
	return filepath.Join(xdg.StateHome, "gcx", stateFileName), nil
}

// LoadDefaultState loads notifier state from the default state path.
func LoadDefaultState() (State, error) {
	path, err := StatePath()
	if err != nil {
		return State{}, err
	}
	return LoadState(path)
}

// SaveDefaultState saves notifier state to the default state path.
func SaveDefaultState(state State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	return SaveState(path, state)
}
