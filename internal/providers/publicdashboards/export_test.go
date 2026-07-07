package publicdashboards

import (
	"io"

	"github.com/spf13/cobra"
)

// ReadPublicDashboardSpecForTest exposes readPublicDashboardSpec for external tests.
func ReadPublicDashboardSpecForTest(path string, stdin io.Reader) (*PublicDashboard, error) {
	return readPublicDashboardSpec(path, stdin)
}

// NewCreateCommandForTest exposes newCreateCommand for external tests.
func NewCreateCommandForTest(loader GrafanaConfigLoader) *cobra.Command {
	return newCreateCommand(loader)
}
