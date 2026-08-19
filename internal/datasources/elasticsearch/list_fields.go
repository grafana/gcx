package elasticsearch

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
)

// ListFieldsCmd returns the `list-fields` subcommand for an Elasticsearch datasource parent.
func ListFieldsCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newMappingListCmd(loader, mappingListSpec{
		use:   "list-fields",
		short: "List mapped fields from an Elasticsearch datasource",
		long: `List the mapped fields and their types, per index. Nested object fields are
flattened with dotted names. Use these names in Lucene queries and --group-by.`,
		example: `
  # All fields across indices
  gcx datasources elasticsearch list-fields

  # Fields of one index
  gcx datasources elasticsearch list-fields -d UID --index grafana-logs -o json`,
		tokenCost: "small",
		llmHint:   `gcx datasources elasticsearch list-fields -d UID --index INDEX`,
		errNoun:   "fields",
		result: func(_ []elasticsearch.IndexInfo, fields []elasticsearch.FieldInfo) any {
			return fields
		},
		formatTable: func(w io.Writer, data any) error {
			fields, ok := data.([]elasticsearch.FieldInfo)
			if !ok {
				return fmt.Errorf("list-fields table codec: unexpected type %T", data)
			}
			return elasticsearch.FormatFields(w, fields)
		},
	})
}
