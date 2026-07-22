package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstRunNoticeShownOnceThenSuppressed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gcx", firstRunNoticeFileName)

	var first strings.Builder
	maybeShowFirstRunNotice(&first, ModeEnabled, true, false, false, path)
	assert.Equal(t, firstRunNotice, first.String())
	assert.Contains(t, first.String(), "GCX_TELEMETRY=disabled")
	// The config opt-out must be paste-ready YAML, not an inline key that the
	// strict config parser would reject verbatim.
	assert.Contains(t, first.String(), "  diagnostics:\n    telemetry: disabled")
	assert.Contains(t, first.String(), "https://grafana.com/docs/")
	assert.NotContains(t, first.String(), ".md", "notice must link the rendered page, not raw markdown")
	assert.NotContains(t, first.String(), "—", "notice must not contain em-dashes")

	_, err := os.Stat(path)
	require.NoError(t, err, "showing the notice must write the flag file")

	var second strings.Builder
	maybeShowFirstRunNotice(&second, ModeEnabled, true, false, false, path)
	assert.Empty(t, second.String(), "flag file must suppress the notice")
}

func TestFirstRunNoticeSuppressedWhenNotInteractive(t *testing.T) {
	tests := []struct {
		name                 string
		isTTY, isCI, isAgent bool
	}{
		{name: "non-terminal stderr", isTTY: false},
		{name: "CI environment", isTTY: true, isCI: true},
		{name: "agent mode", isTTY: true, isAgent: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gcx", firstRunNoticeFileName)

			var out strings.Builder
			maybeShowFirstRunNotice(&out, ModeEnabled, tc.isTTY, tc.isCI, tc.isAgent, path)
			assert.Empty(t, out.String())

			_, err := os.Stat(path)
			assert.True(t, os.IsNotExist(err), "suppressed runs must not consume the one-time flag")
		})
	}
}

func TestFirstRunNoticeSuppressedWhenModeNotEnabled(t *testing.T) {
	for _, mode := range []Mode{ModeDisabled, ModeLog} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gcx", firstRunNoticeFileName)

			var out strings.Builder
			maybeShowFirstRunNotice(&out, mode, true, false, false, path)
			assert.Empty(t, out.String())

			_, err := os.Stat(path)
			assert.True(t, os.IsNotExist(err))
		})
	}
}

func TestFirstRunNoticeSkippedWhenStateHomeUnknown(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	assert.Empty(t, FirstRunNoticePath(), "unknown state home must not yield a relative path")

	var out strings.Builder
	maybeShowFirstRunNotice(&out, ModeEnabled, true, false, false, FirstRunNoticePath())
	assert.Empty(t, out.String(), "unknown state home must skip the notice, not repeat it")
}

func TestFirstRunNoticeSkippedWhenStateDirUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	readonly := filepath.Join(t.TempDir(), "readonly")
	require.NoError(t, os.MkdirAll(readonly, 0o500))
	path := filepath.Join(readonly, "gcx", firstRunNoticeFileName)

	var out strings.Builder
	maybeShowFirstRunNotice(&out, ModeEnabled, true, false, false, path)
	assert.Empty(t, out.String(), "unwritable flag file must skip the notice, not repeat it")
}
