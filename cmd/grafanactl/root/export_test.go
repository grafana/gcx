package root

import (
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/spf13/cobra"
)

// NewCommandForTest exposes the internal newCommand constructor for use in
// external (_test) packages. It is only compiled during `go test`.
func NewCommandForTest(version string, pp []providers.Provider) *cobra.Command {
	return newCommand(version, pp)
}
