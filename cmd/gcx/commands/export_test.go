package commands

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Exported aliases for unexported functions, usable from external test packages.
//
//nolint:gochecknoglobals // Test export pattern.
var (
	WalkCommand = func(cmd *cobra.Command, parentPath string) CommandInfo {
		return walkCommandWithOptions(cmd, parentPath, false)
	}
	WalkCommandWithOptions = walkCommandWithOptions
	FlattenCommands        = flattenCommands
	ExtractArgs            = extractArgs
)

// NewTestCommand builds a Command for testing with the given root.
func NewTestCommand(root *cobra.Command) *cobra.Command {
	return Command(root)
}

// ExportCollectResourceTypes exposes collectResourceTypes for external tests.
func ExportCollectResourceTypes(wk []agent.KnownResource, regs []adapter.Registration) []ResourceTypeInfo {
	return collectResourceTypes(wk, regs)
}

// NewCommandsOptsForTest constructs commandsOpts with flags bound, for
// output-contract tests. Call agent.SetFlag before this — the agent-mode
// default format is resolved at bind time.
func NewCommandsOptsForTest(flags *pflag.FlagSet) *commandsOpts {
	opts := &commandsOpts{}
	opts.setup(flags)
	return opts
}

// EmitValidationResultForTest exposes emitValidationResult (the --validate
// output tail: encode + EmittedError on uncovered types).
//
//nolint:gochecknoglobals // Test export pattern.
var EmitValidationResultForTest = emitValidationResult
