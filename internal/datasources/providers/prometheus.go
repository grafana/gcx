package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&prometheusDSProvider{})
}

type prometheusDSProvider struct{}

func (p *prometheusDSProvider) Kind() string      { return "prometheus" }
func (p *prometheusDSProvider) ShortDesc() string { return "Query Prometheus datasources" }

func (p *prometheusDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return prometheus.QueryCmd(loader)
}

func (p *prometheusDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		prometheus.LabelsCmd(loader),
		prometheus.MetadataCmd(loader),
	}
}
