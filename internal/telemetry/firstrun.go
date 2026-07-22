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

// firstRunNotice is the one-time message telling interactive users that
// anonymous usage stats are on and how to opt out. The docs link is the
// rendered page (trailing slash), not the raw-markdown .md URL the registry
// serves to agents.
//
//nolint:gochecknoglobals // constant-like; var only because TrimSuffix is not const-able.
var firstRunNotice = `gcx collects anonymous usage statistics so we can make gcx better. We do not collect potentially identifiable or sensitive information like argument or flag values or resource names.
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
// (a TTY, not CI, not an agent), at most once per install, gated by a flag
// file under the XDG state dir. All file I/O is best-effort: when the flag
// file cannot be written the notice is skipped rather than repeated on every
// invocation.
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
	if _, err := os.Stat(path); err == nil {
		return
	}
	// Record before showing: when the state dir is unwritable, skipping the
	// notice beats printing it on every invocation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return
	}
	fmt.Fprint(w, firstRunNotice)
}
