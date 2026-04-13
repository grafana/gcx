package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&tempoDSProvider{})
}

type tempoDSProvider struct{}

func (p *tempoDSProvider) Kind() string      { return "tempo" }
func (p *tempoDSProvider) ShortDesc() string { return "Query Tempo datasources" }

func (p *tempoDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return tempo.QueryCmd(loader)
}

func (p *tempoDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		tempo.GetCmd(loader),
		tempo.LabelsCmd(loader),
		tempo.MetricsCmd(loader),
	}
}
