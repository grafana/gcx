package errortracking_test

import (
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
)

func TestHeatmapsBreakdown(t *testing.T) {
	f := newFakeET(t)

	f.runCases(t, []cliCase{
		{name: "and so is an empty one", args: []string{"errors", "timeline", "42", "--project", "314", "--from", "now", "--to", "now", "-o", "json"},
			exit: gcxerrors.ExitUsageError, err: `--from "now" is not before --to "now"`},
	})
}

func TestHelpMarksRequiredFlags(t *testing.T) {
	f := newFakeET(t)
	f.runCases(t, []cliCase{
		{name: "--status help says what new and regressed send", args: []string{"errors", "list", "--help"},
			has: []string{"regressed (resolved errors that came back)", "new (unresolved, first seen since the window start and not reviewed)"}},
		{name: "--project names no auto-pick", args: []string{"errors", "list", "--help"}, has: []string{"then the stack's default (projects use)\n"}, hasNot: []string{"only project"}},
	})
}
