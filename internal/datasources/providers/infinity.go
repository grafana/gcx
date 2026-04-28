package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/infinity"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&infinityDSProvider{})
}

type infinityDSProvider struct{}

func (p *infinityDSProvider) Kind() string { return "infinity" }
func (p *infinityDSProvider) ShortDesc() string {
	return "Query Infinity datasources (JSON, CSV, XML, GraphQL from any URL)"
}

func (p *infinityDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return infinity.QueryCmd(loader)
}

func (p *infinityDSProvider) ExtraCommands(_ *providers.ConfigLoader) []*cobra.Command {
	return nil
}
