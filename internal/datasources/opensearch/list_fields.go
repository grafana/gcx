package opensearch

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/spf13/cobra"
)

// ListFieldsCmd returns the `list-fields` subcommand for an OpenSearch datasource parent.
func ListFieldsCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newMappingListCmd(loader, mappingListSpec{
		use:   "list-fields",
		short: "List mapped fields from an OpenSearch datasource",
		long: `List the mapped fields and their types, per index. Nested object fields are
flattened with dotted names. Use these names in Lucene queries and --group-by.`,
		example: `
  # All fields across indices
  gcx datasources opensearch list-fields

  # Fields of one index
  gcx datasources opensearch list-fields -d UID --index grafana-logs -o json`,
		tokenCost: "small",
		llmHint:   `gcx datasources opensearch list-fields -d UID --index INDEX`,
		errNoun:   "fields",
		result: func(_ []opensearch.IndexInfo, fields []opensearch.FieldInfo) any {
			return fields
		},
		formatTable: func(w io.Writer, data any) error {
			fields, ok := data.([]opensearch.FieldInfo)
			if !ok {
				return fmt.Errorf("list-fields table codec: unexpected type %T", data)
			}
			return opensearch.FormatFields(w, fields)
		},
	})
}
