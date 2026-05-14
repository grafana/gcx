package instrumentation_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/instrumentation"
)

// TestInstrumentationCommandLongDescriptionMentionsRealVerbs asserts that the
// parent "gcx instrumentation --help" Long description reflects the actual
// subcommand names after PR #597 renamed clusters subcommands:
//
//   - enable  → configure
//   - disable → remove
//
// "reset" was never a registered subcommand in this tree.
//
// This is a regression guard: if the Long text drifts from the real cobra
// subcommand registry again, this test will catch it.
func TestInstrumentationCommandLongDescriptionMentionsRealVerbs(t *testing.T) {
	cmd := instrumentation.Command()
	long := cmd.Long

	// Stale verbs that must NOT appear.
	staleVerbs := []string{"enable", "disable", "reset"}
	for _, verb := range staleVerbs {
		if strings.Contains(long, verb) {
			t.Errorf("Long description contains stale verb %q — update the description to match the current subcommand names", verb)
		}
	}

	// Current verbs that MUST appear.
	currentVerbs := []string{"configure", "remove"}
	for _, verb := range currentVerbs {
		if !strings.Contains(long, verb) {
			t.Errorf("Long description does not contain current verb %q — it may need to be added", verb)
		}
	}
}
