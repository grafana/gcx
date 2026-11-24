package root

import (
	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// NewCommandForTest exposes the internal newCommand constructor for use in
// external (_test) packages. It is only compiled during `go test`.
func NewCommandForTest(version string, pp []providers.Provider) *cobra.Command {
	return newCommand(version, pp)
}

// ExplicitHumanOutputForTest exposes the shared explicit output helper
// for use in external (_test) packages. It is only compiled during `go test`.
func ExplicitHumanOutputForTest(cmd *cobra.Command) bool {
	return output.IsHumanOutputFlagActive(cmd)
}
