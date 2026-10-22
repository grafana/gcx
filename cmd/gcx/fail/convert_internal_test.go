package fail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeychainLockedSuggestions checks the remedies on the platforms where gcx
// can classify a locked native keychain, including safe session diagnostics.
func TestKeychainLockedSuggestions(t *testing.T) {
	tests := map[string]struct {
		goos                string
		wantSessionGuidance bool
		wantUnlockCommand   bool
	}{
		"linux":     {goos: "linux", wantSessionGuidance: true},
		"freebsd":   {goos: "freebsd", wantSessionGuidance: true},
		"netbsd":    {goos: "netbsd", wantSessionGuidance: true},
		"openbsd":   {goos: "openbsd", wantSessionGuidance: true},
		"dragonfly": {goos: "dragonfly", wantSessionGuidance: true},
		"darwin":    {goos: "darwin", wantSessionGuidance: true, wantUnlockCommand: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			suggestions := keychainLockedSuggestions(test.goos)
			require.NotEmpty(t, suggestions)

			joined := strings.Join(suggestions, "\n")
			assert.NotContains(t, joined, "gnome-keyring-daemon")
			assert.NotContains(t, joined, "systemd-ask-password")
			assert.Contains(t, joined, "GRAFANA_TOKEN")
			assert.Equal(t, test.wantSessionGuidance, strings.Contains(joined, "session"))
			assert.NotContains(t, joined, "/org/freedesktop/secrets/collection/login")
			assert.Equal(t, test.wantUnlockCommand, strings.Contains(joined, "security unlock-keychain"))
		})
	}
}
