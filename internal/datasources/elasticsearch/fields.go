package elasticsearch

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type fieldsOpts struct {
	IO         cmdio.Options
	Datasource string
	Index      string
}

func (opts *fieldsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &fieldsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.elasticsearch is configured)")
	flags.StringVar(&opts.Index, "index", "", "Restrict to this index or index pattern")
}

func (opts *fieldsOpts) Validate() error {
	return opts.IO.Validate()
}

// FieldsCmd returns the `fields` subcommand for an Elasticsearch datasource parent.
func FieldsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fieldsOpts{}

	cmd := &cobra.Command{
		Use:   "fields",
		Short: "List mapped fields from an Elasticsearch datasource",
		Long: `List the mapped fields and their types, per index. Nested object fields are
flattened with dotted names. Use these names in Lucene queries and --group-by.`,
		Example: `
  # All fields across indices
  gcx datasources elasticsearch fields

  # Fields of one index
  gcx datasources elasticsearch fields -d UID --index grafana-logs -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "elasticsearch")
			if err != nil {
				return err
			}

			client, err := elasticsearch.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			_, fields, err := client.Mapping(ctx, datasourceUID, opts.Index)
			if err != nil {
				return fmt.Errorf("failed to list fields: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), fields)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources elasticsearch fields -d UID --index INDEX`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

type fieldsTableCodec struct{}

func (c *fieldsTableCodec) Format() format.Format { return "table" }

func (c *fieldsTableCodec) Encode(w io.Writer, data any) error {
	fields, ok := data.([]elasticsearch.FieldInfo)
	if !ok {
		return fmt.Errorf("fieldsTableCodec: unexpected type %T", data)
	}
	return elasticsearch.FormatFields(w, fields)
}

func (c *fieldsTableCodec) Decode(io.Reader, any) error {
	return errors.New("fieldsTableCodec does not support decoding")
}
