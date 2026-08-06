package fail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeychainLockedSuggestions checks the invariants of the locked-keychain
// remedies on every platform. A suggestion must never name an unlock command,
// because the correct command depends on the session; the documentation holds
// the procedure. Every platform must also offer an escape route for a host
// that the user cannot unlock, such as a continuous integration runner. Only
// the freedesktop Secret Service exposes a lock-state command.
func TestKeychainLockedSuggestions(t *testing.T) {
	tests := map[string]struct {
		goos          string
		wantLockState bool
	}{
		"linux":     {goos: "linux", wantLockState: true},
		"freebsd":   {goos: "freebsd", wantLockState: true},
		"netbsd":    {goos: "netbsd", wantLockState: true},
		"openbsd":   {goos: "openbsd", wantLockState: true},
		"dragonfly": {goos: "dragonfly", wantLockState: true},
		"darwin":    {goos: "darwin", wantLockState: false},
		"windows":   {goos: "windows", wantLockState: false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			suggestions := keychainLockedSuggestions(test.goos)
			require.NotEmpty(t, suggestions)

			joined := strings.Join(suggestions, "\n")
			assert.NotContains(t, joined, "gnome-keyring-daemon")
			assert.NotContains(t, joined, "systemd-ask-password")
			assert.Contains(t, joined, "GRAFANA_TOKEN")
			assert.Equal(t, test.wantLockState, strings.Contains(joined, "busctl"))
		})
	}
}
