package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&pyroscopeDSProvider{})
}

type pyroscopeDSProvider struct{}

func (p *pyroscopeDSProvider) Kind() string      { return "pyroscope" }
func (p *pyroscopeDSProvider) ShortDesc() string { return "Query Pyroscope datasources" }

func (p *pyroscopeDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return pyroscope.QueryCmd(loader)
}

func (p *pyroscopeDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		pyroscope.LabelsCmd(loader),
		pyroscope.ProfileTypesCmd(loader),
		pyroscope.MetricsCmd(loader),
	}
}
