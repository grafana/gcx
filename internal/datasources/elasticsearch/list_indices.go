package elasticsearch

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
)

// ListIndicesCmd returns the `list-indices` subcommand for an Elasticsearch datasource parent.
func ListIndicesCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newMappingListCmd(loader, mappingListSpec{
		use:   "list-indices",
		short: "List indices from an Elasticsearch datasource",
		long: `List the indices visible to an Elasticsearch datasource, with their mapped
field counts. Pass --index to restrict to one index or pattern; fetching the
mapping for every index can hit the response size cap on a large cluster.`,
		example: `
  gcx datasources elasticsearch list-indices
  gcx datasources elasticsearch list-indices -d UID -o json

  # Restrict to one index or pattern
  gcx datasources elasticsearch list-indices -d UID --index grafana-logs`,
		tokenCost: "small",
		llmHint:   `gcx datasources elasticsearch list-indices -d UID`,
		errNoun:   "indices",
		result: func(indices []elasticsearch.IndexInfo, _ []elasticsearch.FieldInfo) any {
			return indices
		},
		formatTable: func(w io.Writer, data any) error {
			indices, ok := data.([]elasticsearch.IndexInfo)
			if !ok {
				return fmt.Errorf("list-indices table codec: unexpected type %T", data)
			}
			return elasticsearch.FormatIndices(w, indices)
		},
	})
}
