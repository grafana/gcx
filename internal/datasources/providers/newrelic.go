package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/newrelic"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&newrelicDSProvider{})
}

type newrelicDSProvider struct{}

func (p *newrelicDSProvider) Kind() string      { return "newrelic" }
func (p *newrelicDSProvider) ShortDesc() string { return "Query New Relic plugin datasources" }
func (p *newrelicDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newrelic.QueryCmd(loader)
}
func (p *newrelicDSProvider) ExtraCommands(_ *providers.ConfigLoader) []*cobra.Command {
	return nil
}
