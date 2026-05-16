package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/clickhouse"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&clickhouseDSProvider{})
}

type clickhouseDSProvider struct{}

func (p *clickhouseDSProvider) Kind() string      { return "clickhouse" }
func (p *clickhouseDSProvider) ShortDesc() string { return "Query ClickHouse datasources" }

func (p *clickhouseDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return clickhouse.QueryCmd(loader)
}

func (p *clickhouseDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		clickhouse.ListTablesCmd(loader),
		clickhouse.DescribeTableCmd(loader),
	}
}
