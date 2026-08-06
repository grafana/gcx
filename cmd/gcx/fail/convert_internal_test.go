package fail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A suggestion must never name an unlock command. The correct command depends
// on the session. A service manager can hold the org.freedesktop.secrets name
// and refuse to yield it, and the daemon accepts the password only on standard
// input without a trailing newline. A command that silently does nothing is
// worse than no command.
func TestKeychainLockedSuggestionsNameNoUnlockCommand(t *testing.T) {
	platforms := []string{"linux", "freebsd", "netbsd", "openbsd", "dragonfly", "darwin", "windows"}
	for _, goos := range platforms {
		t.Run(goos, func(t *testing.T) {
			suggestions := keychainLockedSuggestions(goos)
			require.NotEmpty(t, suggestions)
			for _, suggestion := range suggestions {
				assert.NotContains(t, suggestion, "gnome-keyring-daemon")
				assert.NotContains(t, suggestion, "systemd-ask-password")
			}
		})
	}
}

// Every platform must offer an escape route for a host with no keyring that
// the user can unlock, such as a continuous integration runner.
func TestKeychainLockedSuggestionsOfferAnEnvironmentVariable(t *testing.T) {
	platforms := []string{"linux", "freebsd", "netbsd", "openbsd", "dragonfly", "darwin", "windows"}
	for _, goos := range platforms {
		t.Run(goos, func(t *testing.T) {
			joined := strings.Join(keychainLockedSuggestions(goos), "\n")
			assert.Contains(t, joined, "GRAFANA_TOKEN")
		})
	}
}
