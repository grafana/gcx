package fail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeychainLockedSuggestions checks the remedies on the platforms where gcx
// can classify a locked native keychain. Secret Service commands vary by
// session, while macOS has a stable command with security-session scope.
func TestKeychainLockedSuggestions(t *testing.T) {
	tests := map[string]struct {
		goos          string
		wantLockState bool
		wantSecurity  bool
	}{
		"linux":     {goos: "linux", wantLockState: true},
		"freebsd":   {goos: "freebsd", wantLockState: true},
		"netbsd":    {goos: "netbsd", wantLockState: true},
		"openbsd":   {goos: "openbsd", wantLockState: true},
		"dragonfly": {goos: "dragonfly", wantLockState: true},
		"darwin":    {goos: "darwin", wantSecurity: true},
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
			assert.Equal(t, test.wantSecurity, strings.Contains(joined, "security unlock-keychain"))
		})
	}
}
