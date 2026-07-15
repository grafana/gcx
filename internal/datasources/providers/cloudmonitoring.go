package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/cloudmonitoring"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&cloudmonitoringDSProvider{})
}

type cloudmonitoringDSProvider struct{}

func (p *cloudmonitoringDSProvider) Kind() string { return "cloudmonitoring" }
func (p *cloudmonitoringDSProvider) ShortDesc() string {
	return "Query Google Cloud Monitoring datasources"
}

func (p *cloudmonitoringDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return cloudmonitoring.QueryCmd(loader)
}

func (p *cloudmonitoringDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		cloudmonitoring.ListProjectsCmd(loader),
		cloudmonitoring.ListMetricsCmd(loader),
	}
}
