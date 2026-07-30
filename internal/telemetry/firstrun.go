package telemetry

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/xdg"
)

const firstRunNoticeFileName = "telemetry-notice-shown"

// noticeRevision must be bumped whenever firstRunNotice's substance changes,
// in particular whenever a new kind of data starts being collected.
//
// The flag file records the revision it was written for, and a mismatch
// re-shows the notice. Without this, a revised disclosure would only ever reach
// installs that had never run gcx interactively: everyone else already has the
// flag file and would keep the consent text they were originally shown while
// the collection behind it changed.
const noticeRevision = "2"

// firstRunNotice is the one-time message telling interactive users that
// anonymous usage stats are on and how to opt out. The docs link is the
// rendered page (trailing slash), not the raw-markdown .md URL the registry
// serves to agents.
//
// It names the two fields derived from flag settings rather than claiming a
// single exception: output_format carries the value of --output (filtered to a
// fixed list of formats), and dry_run reports whether the run was a rehearsal.
// An enumeration like "the only exception is X" is a promise that has to be
// re-audited against the whole event every time a field is added, and it was
// wrong the first time it was written.
//
//nolint:gochecknoglobals // constant-like; var only because TrimSuffix is not const-able.
var firstRunNotice = `gcx collects anonymous usage statistics so we can make gcx better. We do not collect arguments, free-form flag values, resource names, or raw counts of anything. Flags you set are recorded by name only.

For the resource commands that work on batches, we record how many resources the operation succeeded, failed and skipped, as one of seven fixed size categories rather than a number. Two of those categories, "0" and "1", cover a single value each; the rest are ranges. We also record the output format used, and whether the operation ran in dry-run mode.
You can opt out by setting GCX_TELEMETRY=disabled, or adding to your gcx config file:
  diagnostics:
    telemetry: disabled
Find out more at ` + strings.TrimSuffix(docs.AnonymousUsageStats, ".md") + "/\n"

// FirstRunNoticePath returns the flag file that records the notice was shown,
// or "" when no state home is known (HOME and XDG_STATE_HOME both unset), so
// the flag file cannot land relative to the current directory.
func FirstRunNoticePath() string {
	stateHome := xdg.StateHome()
	if stateHome == "" {
		return ""
	}
	return filepath.Join(stateHome, "gcx", firstRunNoticeFileName)
}

// MaybeShowFirstRunNotice writes the one-time telemetry notice to w. It is
// shown only when telemetry is actually enabled and the run is interactive
// (a TTY, not CI, not an agent), at most once per notice revision, gated by a
// flag file under the XDG state dir. All file I/O is best-effort: when the flag
// file cannot be written the notice is skipped rather than repeated on every
// invocation.
//
// "Once per revision" rather than once per install: bumping noticeRevision
// re-shows a materially changed disclosure to existing installs, which is the
// only way an amended notice reaches anyone who has already run gcx.
func MaybeShowFirstRunNotice(w io.Writer, mode Mode, isTTY, isCI, isAgent bool) {
	maybeShowFirstRunNotice(w, mode, isTTY, isCI, isAgent, FirstRunNoticePath())
}

func maybeShowFirstRunNotice(w io.Writer, mode Mode, isTTY, isCI, isAgent bool, path string) {
	if mode != ModeEnabled || !isTTY || isCI || isAgent {
		return
	}
	// No known state home: without the flag file the notice would repeat on
	// every invocation, so skip it, matching the unwritable-dir behaviour.
	if path == "" {
		return
	}
	// A file written for the current revision means this disclosure has been
	// shown. An older file (including the empty one written before revisions
	// existed) means the notice has changed since, so show it again.
	if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == noticeRevision {
		return
	}
	// Record before showing: when the state dir is unwritable, skipping the
	// notice beats printing it on every invocation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, []byte(noticeRevision+"\n"), 0o600); err != nil {
		return
	}
	fmt.Fprint(w, firstRunNotice)
}
