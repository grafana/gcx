package errortracking_test

import (
	"testing"
)

func TestHelpMarksRequiredFlags(t *testing.T) {
	f := newFakeET(t)
	f.runCases(t, []cliCase{
		{name: "--status help says what new and regressed send", args: []string{"errors", "list", "--help"},
			has: []string{"regressed (resolved errors that came back)", "new (unresolved, first seen since the window start and not reviewed)"}},
		{name: "--project names no auto-pick", args: []string{"errors", "list", "--help"}, has: []string{"then the stack's default (projects use)\n"}, hasNot: []string{"only project"}},
	})
}
