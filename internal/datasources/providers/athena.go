package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/athena"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&athenaDSProvider{})
}

type athenaDSProvider struct{}

func (p *athenaDSProvider) Kind() string      { return "athena" }
func (p *athenaDSProvider) ShortDesc() string { return "Query Amazon Athena datasources" }

func (p *athenaDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return athena.QueryCmd(loader)
}

func (p *athenaDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		athena.ListCatalogsCmd(loader),
		athena.ListDatabasesCmd(loader),
		athena.ListTablesCmd(loader),
		athena.DescribeTableCmd(loader),
	}
}
