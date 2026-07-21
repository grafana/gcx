package telemetry

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/xdg"
)

const firstRunNoticeFileName = "telemetry-notice-shown"

// firstRunNotice is the one-time message telling interactive users that
// anonymous usage stats are on and how to opt out.
const firstRunNotice = "gcx collects anonymous usage statistics: command paths, flag names, and outcomes - never argument values, resource names, or hosts.\n" +
	"Opt out: set GCX_TELEMETRY=disabled, or diagnostics.telemetry: disabled in the gcx config file.\n" +
	"Details: " + docs.AnonymousUsageStats + "\n"

// FirstRunNoticePath returns the flag file that records the notice was shown.
func FirstRunNoticePath() string {
	return filepath.Join(xdg.StateHome(), "gcx", firstRunNoticeFileName)
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
