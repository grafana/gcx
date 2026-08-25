package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/postgres"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&postgresDSProvider{})
}

type postgresDSProvider struct{}

func (p *postgresDSProvider) Kind() string      { return "postgres" }
func (p *postgresDSProvider) ShortDesc() string { return "Query PostgreSQL datasources" }

func (p *postgresDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return postgres.QueryCmd(loader)
}

func (p *postgresDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		postgres.ListTablesCmd(loader),
		postgres.DescribeTableCmd(loader),
	}
}
