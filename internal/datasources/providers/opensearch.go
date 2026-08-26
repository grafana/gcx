package providers

import (
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/datasources/opensearch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	datasources.RegisterProvider(&opensearchDSProvider{})
}

type opensearchDSProvider struct{}

func (p *opensearchDSProvider) Kind() string      { return "opensearch" }
func (p *opensearchDSProvider) ShortDesc() string { return "Query OpenSearch datasources" }

func (p *opensearchDSProvider) QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return opensearch.QueryCmd(loader)
}

func (p *opensearchDSProvider) ExtraCommands(loader *providers.ConfigLoader) []*cobra.Command {
	return []*cobra.Command{
		opensearch.MetricsCmd(loader),
		opensearch.ListIndicesCmd(loader),
		opensearch.ListFieldsCmd(loader),
	}
}
