package commands

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// Exported aliases for unexported functions, usable from external test packages.
//
//nolint:gochecknoglobals // Test export pattern.
var (
	WalkCommand            = walkCommand
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

// ExportCompareAgainstLive exposes compareAgainstLive for external tests.
func ExportCompareAgainstLive(catalog []ResourceTypeInfo, liveDescs resources.Descriptors) *ValidationResult {
	return compareAgainstLive(catalog, liveDescs)
}
