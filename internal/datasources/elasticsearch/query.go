package elasticsearch

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
)

const (
	defaultSize = 100
	maxSize     = 1000
)

// QueryCmd returns the `query` subcommand for an Elasticsearch datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newSearchCmd(loader, searchCmdSpec{
		use:   "query [EXPR]",
		short: "Search documents in an Elasticsearch datasource",
		long: `Search documents in an Elasticsearch datasource with a Lucene query.

EXPR is a Lucene query string (e.g. 'app:frontend AND level:error'); omit it to
match all documents in the time range. The index pattern comes from the
datasource configuration.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.`,
		example: `
  # Match all documents in the last hour
  gcx datasources elasticsearch query --since 1h

  # Lucene query with explicit datasource
  gcx datasources elasticsearch query -d UID 'app:frontend AND level:error' --since 1h

  # Output as JSON, limit results
  gcx datasources elasticsearch query -d UID 'datacenter:us-east' --size 20 -o json`,
		sizeFlag:  "size",
		sizeUsage: fmt.Sprintf("Max documents to return (capped at %d)", maxSize),
		tokenCost: "large",
		llmHint:   `gcx datasources elasticsearch query -d UID 'app:frontend AND level:error' --since 1h --size 20`,
		search: func(ctx context.Context, c *elasticsearch.Client, uid string, req elasticsearch.SearchRequest) (*querysql.QueryResponse, error) {
			return c.Search(ctx, uid, req)
		},
	})
}
