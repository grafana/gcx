package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/pinot"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&pinotDSProvider{})
}

type pinotDSProvider struct{}

func (p *pinotDSProvider) Kind() string      { return "pinot" }
func (p *pinotDSProvider) ShortDesc() string { return "Query StarTree Pinot datasources" }

func (p *pinotDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return pinot.QueryCmd(loader)
}

func (p *pinotDSProvider) ExtraCommands(_ *providers.ConfigLoader) []*cobra.Command {
	return nil
}
