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

// An install that already saw an earlier notice must see a revised one. This is
// the upgrade path: before revisions existed the flag file was written empty, so
// every existing install holds one, and without this the amended disclosure
// would only ever reach brand-new installs while the collection behind it had
// already changed for everyone.
func TestFirstRunNoticeReshownAfterRevisionBump(t *testing.T) {
	for _, stale := range []struct {
		name    string
		content []byte
	}{
		{"pre-revision empty file", nil},
		{"older revision", []byte("1\n")},
	} {
		t.Run(stale.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gcx", firstRunNoticeFileName)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, stale.content, 0o600))

			var out strings.Builder
			maybeShowFirstRunNotice(&out, ModeEnabled, true, false, false, path)
			assert.Equal(t, firstRunNotice, out.String(),
				"a changed disclosure must reach installs that saw an earlier one")

			recorded, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, noticeRevision, strings.TrimSpace(string(recorded)),
				"the flag file must record the revision just shown")

			var again strings.Builder
			maybeShowFirstRunNotice(&again, ModeEnabled, true, false, false, path)
			assert.Empty(t, again.String(), "the revised notice must then be suppressed")
		})
	}
}

// The notice must disclose what this revision actually added.
//
// Revision 2 exists because batch size categories started being collected, so a
// notice that mentions only the output format and dry-run state discloses the
// trimmings and omits the headline. That was the shape of the first draft.
func TestFirstRunNoticeDisclosesBatchSizeCategories(t *testing.T) {
	assert.Contains(t, firstRunNotice, "size categories",
		"the batch size categories are the new collection in this revision")
	assert.Contains(t, firstRunNotice, "succeeded, failed and skipped",
		"say which counts are categorised, not just that sizes are collected")
	assert.Contains(t, firstRunNotice, `"0" and "1"`,
		"the singleton categories are exact, so the notice must say so")
	assert.Contains(t, firstRunNotice, "dry-run mode",
		"dry_run must be disclosed alongside the sizes")
	assert.Contains(t, firstRunNotice, "output format",
		"output_format is derived from --output and must be disclosed")
}

// The notice states what is not collected without enumerating exceptions. An
// enumeration has to be re-audited against the whole event every time a field is
// added, and the first one shipped was already wrong: it claimed --dry-run was
// the only flag-derived value while output_format sat beside it.
func TestFirstRunNoticeStatesExclusionsWithoutEnumeratingExceptions(t *testing.T) {
	assert.NotContains(t, firstRunNotice, "one exception",
		"do not enumerate exceptions; name what is collected")
	assert.Contains(t, firstRunNotice, "by name only",
		"the notice must still state that flags are recorded by name")
	assert.Contains(t, firstRunNotice, "raw counts",
		"no raw numeric count field is sent, and the notice must say so")
	for _, excluded := range []string{"arguments", "free-form flag values", "resource names"} {
		assert.Contains(t, firstRunNotice, excluded,
			"the notice must name what is not collected")
	}
}

// "rehearsal" overstated what dry_run means: pull is read-only and always
// reports false, so false does not imply a real change. The notice must not
// reintroduce that framing.
func TestFirstRunNoticeDoesNotEquateDryRunWithRehearsalVersusChange(t *testing.T) {
	assert.NotContains(t, firstRunNotice, "rather than a real change",
		"dry_run=false does not imply mutation; pull is read-only and always false")
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
