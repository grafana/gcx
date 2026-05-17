package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&cloudwatchDSProvider{})
}

type cloudwatchDSProvider struct{}

func (p *cloudwatchDSProvider) Kind() string      { return "cloudwatch" }
func (p *cloudwatchDSProvider) ShortDesc() string { return "Query AWS CloudWatch datasources" }

func (p *cloudwatchDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return cloudwatch.QueryCmd(loader)
}

func (p *cloudwatchDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		cloudwatch.ListNamespacesCmd(loader),
		cloudwatch.ListMetricsCmd(loader),
		cloudwatch.ListDimensionsCmd(loader),
		cloudwatch.ListRegionsCmd(loader),
		cloudwatch.ListAccountsCmd(loader),
	}
}
