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
	assert.Contains(t, firstRunNotice, "no raw batch or resource counts",
		"the notice must say batch counts are not sent")
	assert.NotContains(t, firstRunNotice, "raw counts of anything",
		"the event does carry other numbers (duration_ms, exit_code), so the promise "+
			"must stay scoped to batch and resource counts")
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

// A state file that cannot be read back must suppress the notice rather than
// re-show it forever. Mode 0200 is the case that bites: the read fails but the
// write succeeds, so a check that only looked at read success would rewrite the
// flag and print the notice on every single interactive run, with nothing the
// user could do about it.
func TestFirstRunNoticeSkippedWhenStateFileUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	path := filepath.Join(t.TempDir(), "gcx", firstRunNoticeFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(noticeRevision+"\n"), 0o200))

	var out strings.Builder
	maybeShowFirstRunNotice(&out, ModeEnabled, true, false, false, path)
	assert.Empty(t, out.String(), "an unreadable flag file must skip the notice, not repeat it")
}

// noticeRevision has to move whenever the notice's substance changes, or a
// revised disclosure ships under the consent people already accepted. Nothing
// mechanical tied the two together: every other assertion in this file is a
// Contains, so a contributor could add a newly collected field to the notice,
// leave the revision at 2, and no test would fail.
//
// Pinning the exact text closes that. Any edit fails here, which forces the
// author to make the revision decision deliberately instead of by omission.
func TestFirstRunNoticeTextIsPinnedToItsRevision(t *testing.T) {
	const pinnedRevision = "2"
	const pinnedNotice = `gcx collects anonymous usage statistics so we can make gcx better. We do not collect arguments, free-form flag values, or resource names, and no raw batch or resource counts. Flags you set are recorded by name only.

For the resource commands that work on batches, we record fixed size categories for the operation's succeeded, failed and skipped portions, rather than numbers. What each portion counts depends on the command: for some it is individual resources, for others whole resource types. Two of those categories, "0" and "1", cover a single value each; the rest are ranges. We also record the output format used, and whether the operation ran in dry-run mode.
You can opt out by setting GCX_TELEMETRY=disabled, or adding to your gcx config file:
  diagnostics:
    telemetry: disabled
Find out more at https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/anonymous-usage-statistics/
`

	require.Equal(t, pinnedRevision, noticeRevision,
		"noticeRevision moved: update pinnedNotice below to the text this revision shows")
	assert.Equal(t, pinnedNotice, firstRunNotice,
		"the notice text changed. If the change alters what is collected or how it is "+
			"described, bump noticeRevision so existing installs see the new disclosure, "+
			"then update this pin. If it is purely cosmetic, update the pin alone and say so "+
			"in the commit message")
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
