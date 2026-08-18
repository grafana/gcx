package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/bigquery"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&bigqueryDSProvider{})
}

type bigqueryDSProvider struct{}

func (p *bigqueryDSProvider) Kind() string      { return "bigquery" }
func (p *bigqueryDSProvider) ShortDesc() string { return "Query BigQuery datasources" }

func (p *bigqueryDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return bigquery.QueryCmd(loader)
}

func (p *bigqueryDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		bigquery.ListDatasetsCmd(loader),
		bigquery.ListTablesCmd(loader),
		bigquery.DescribeTableCmd(loader),
	}
}
