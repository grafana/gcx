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
