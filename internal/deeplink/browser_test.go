package deeplink_test

import (
	"testing"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOpenURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
	}{
		{
			name:   "rejects hostless https URL",
			rawURL: "https://?x=1",
		},
		{
			name:   "rejects unsupported scheme with http prefix in path",
			rawURL: "file://http://example.com",
		},
		{
			name:   "rejects schemeless URL",
			rawURL: "example.com/d/abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := deeplink.Open(tt.rawURL)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "refusing to open non-http URL")
		})
	}
}

func TestBrowserCommandWindowsDoesNotUseShell(t *testing.T) {
	rawURL := "https://example.com/?x=1&calc"

	name, args := deeplink.BrowserCommand("windows", rawURL)

	assert.NotEqual(t, "cmd", name)
	assert.NotContains(t, args, "/c")
	assert.Contains(t, args, rawURL)
}
