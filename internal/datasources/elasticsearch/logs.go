package elasticsearch

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
)

// LogsCmd returns the `logs` subcommand for an Elasticsearch datasource parent.
func LogsCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newSearchCmd(loader, searchCmdSpec{
		use:   "logs [EXPR]",
		short: "Query logs from an Elasticsearch datasource",
		long: `Query log documents from an Elasticsearch datasource with a Lucene query,
newest first. Plugin-internal fields (_source, sort, highlight) are omitted.

EXPR is a Lucene query string; omit it to match all documents in the time range.
Datasource is resolved from -d flag or datasources.elasticsearch in your context.
Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		example: `
  # Latest logs from the last hour
  gcx datasources elasticsearch logs --since 1h

  # Filter by field
  gcx datasources elasticsearch logs -d UID 'level:error' --since 6h --limit 50

  # Output as JSON
  gcx datasources elasticsearch logs -d UID 'app:frontend' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources elasticsearch logs 'level:error' --since 1h --share-link

  # Continue the same log search in Grafana Explore
  gcx datasources elasticsearch logs 'level:error' --since 1h --open`,
		sizeFlag:       "limit",
		sizeUsage:      fmt.Sprintf("Max log lines to return (capped at %d)", maxSize),
		tokenCost:      "large",
		llmHint:        `gcx datasources elasticsearch logs -d UID 'level:error' --since 1h --limit 50`,
		exploreSubject: "logs query",
		search: func(ctx context.Context, c *elasticsearch.Client, uid string, req elasticsearch.SearchRequest) (*querysql.QueryResponse, error) {
			return c.Logs(ctx, uid, req)
		},
		explore: LogsExploreURL,
	})
}
