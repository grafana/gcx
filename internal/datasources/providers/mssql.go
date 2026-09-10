package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/mssql"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&mssqlDSProvider{})
}

type mssqlDSProvider struct{}

func (p *mssqlDSProvider) Kind() string      { return "mssql" }
func (p *mssqlDSProvider) ShortDesc() string { return "Query Microsoft SQL Server datasources" }

func (p *mssqlDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return mssql.QueryCmd(loader)
}

func (p *mssqlDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		mssql.ListTablesCmd(loader),
		mssql.DescribeTableCmd(loader),
	}
}
