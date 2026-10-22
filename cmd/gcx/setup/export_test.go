package setup

import (
	"fmt"

	"github.com/spf13/pflag"
)

// Exported aliases for unexported types and constructors, available to
// external test packages only.

// NewStatusOptsForTest constructs setupStatusOpts with flags bound, for
// output-contract tests. Call agent.SetFlag before this — the agent-mode
// default format is resolved at bind time.
func NewStatusOptsForTest(flags *pflag.FlagSet) *setupStatusOpts {
	opts := &setupStatusOpts{}
	opts.setup(flags)
	return opts
}

// StatusDocForTest builds the setupStatus document with a single
// instrumentation row, mirroring exactly what the status RunE encodes.
// NewQuickstartOptsForTest constructs quickstartOpts with flags bound, for
// output-contract tests. Call agent.SetFlag before this — the agent-mode
// default format is resolved at bind time.
func NewQuickstartOptsForTest(flags *pflag.FlagSet) *quickstartOpts {
	opts := &quickstartOpts{}
	opts.setup(flags)
	return opts
}

// QuickstartStepForTest is the exported alias of quickstartStep for codec tests.
type QuickstartStepForTest = quickstartStep

// QuickstartDocForTest builds a quickstartResult with the given steps.
func QuickstartDocForTest(steps []quickstartStep) quickstartResult {
	return newQuickstartResult(steps)
}

func StatusDocForTest(enabled bool, clusters int) setupStatus {
	return newSetupStatus([]setupProductStatus{
		{
			Product: "instrumentation",
			Enabled: enabled,
			Health:  "healthy",
			Details: fmt.Sprintf("%d clusters", clusters),
		},
	})
}
