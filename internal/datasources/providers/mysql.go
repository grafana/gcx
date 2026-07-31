package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/mysql"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&mysqlDSProvider{})
}

type mysqlDSProvider struct{}

func (p *mysqlDSProvider) Kind() string      { return "mysql" }
func (p *mysqlDSProvider) ShortDesc() string { return "Query MySQL datasources" }

func (p *mysqlDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return mysql.QueryCmd(loader)
}

func (p *mysqlDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		mysql.ListTablesCmd(loader),
		mysql.DescribeTableCmd(loader),
	}
}
