package root

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// NewCommandForTest exposes the internal newCommand constructor for use in
// external (_test) packages. It is only compiled during `go test`.
func NewCommandForTest(version string, pp []providers.Provider) *cobra.Command {
	return newCommand(version, pp)
}

// ContextPolicyRoutesForTest exposes the command routes named by the strict
// context policy so a test can check them against the real command tree.
func ContextPolicyRoutesForTest() []string {
	return append(append([]string{}, contextExemptRoutes...), contextRequiredRoutes...)
}
