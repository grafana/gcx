package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&lokiDSProvider{})
}

type lokiDSProvider struct{}

func (p *lokiDSProvider) Kind() string      { return "loki" }
func (p *lokiDSProvider) ShortDesc() string { return "Query Loki datasources" }

func (p *lokiDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return loki.QueryCmd(loader)
}

func (p *lokiDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		loki.MetricsCmd(loader),
		loki.LabelsCmd(loader),
		loki.SeriesCmd(loader),
	}
}
