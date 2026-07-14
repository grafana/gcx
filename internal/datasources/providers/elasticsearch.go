package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/elasticsearch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&elasticsearchDSProvider{})
}

type elasticsearchDSProvider struct{}

func (p *elasticsearchDSProvider) Kind() string      { return "elasticsearch" }
func (p *elasticsearchDSProvider) ShortDesc() string { return "Query Elasticsearch datasources" }

func (p *elasticsearchDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return elasticsearch.QueryCmd(loader)
}

func (p *elasticsearchDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		elasticsearch.LogsCmd(loader),
		elasticsearch.MetricsCmd(loader),
		elasticsearch.ListIndicesCmd(loader),
		elasticsearch.FieldsCmd(loader),
	}
}
