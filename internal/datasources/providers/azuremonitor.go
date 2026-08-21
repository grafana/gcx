package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/azuremonitor"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&azuremonitorDSProvider{})
}

type azuremonitorDSProvider struct{}

func (p *azuremonitorDSProvider) Kind() string      { return "azuremonitor" }
func (p *azuremonitorDSProvider) ShortDesc() string { return "Query Azure Monitor datasources" }

func (p *azuremonitorDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return azuremonitor.QueryCmd(loader)
}

func (p *azuremonitorDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		azuremonitor.LogsCmd(loader),
		azuremonitor.ResourceGraphCmd(loader),
		azuremonitor.ListSubscriptionsCmd(loader),
		azuremonitor.ListResourceGroupsCmd(loader),
		azuremonitor.ListResourcesCmd(loader),
		azuremonitor.ListMetricsCmd(loader),
	}
}
