// Package versions — test-only exports.
// This file is compiled only during tests (it lives in the main package, which the
// testpackage linter exempts for files named export_test.go).
package versions

import (
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
)

// NewTestCommandDeps constructs a commandDeps with a pre-built client and descriptor
// for use in tests that bypass real K8s discovery.
func NewTestCommandDeps(client DashboardVersionsClient, desc resources.Descriptor) *commandDeps {
	return &commandDeps{client: client, desc: desc}
}

// NewTestListCommand exposes newListCommand for external test packages.
func NewTestListCommand(deps *commandDeps) *cobra.Command {
	return newListCommand(deps)
}

// NewTestRestoreCommand exposes newRestoreCommand for external test packages.
func NewTestRestoreCommand(deps *commandDeps) *cobra.Command {
	return newRestoreCommand(deps)
}
