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

type listIndicesOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *listIndicesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listIndicesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.elasticsearch is configured)")
}

func (opts *listIndicesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListIndicesCmd returns the `list-indices` subcommand for an Elasticsearch datasource parent.
func ListIndicesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listIndicesOpts{}

	cmd := &cobra.Command{
		Use:   "list-indices",
		Short: "List indices from an Elasticsearch datasource",
		Long:  "List the indices visible to an Elasticsearch datasource, with their mapped field counts.",
		Example: `
  gcx datasources elasticsearch list-indices
  gcx datasources elasticsearch list-indices -d UID -o json`,
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

			indices, _, err := client.Mapping(ctx, datasourceUID, "")
			if err != nil {
				return fmt.Errorf("failed to list indices: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), indices)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources elasticsearch list-indices -d UID`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

type listIndicesTableCodec struct{}

func (c *listIndicesTableCodec) Format() format.Format { return "table" }

func (c *listIndicesTableCodec) Encode(w io.Writer, data any) error {
	indices, ok := data.([]elasticsearch.IndexInfo)
	if !ok {
		return fmt.Errorf("listIndicesTableCodec: unexpected type %T", data)
	}
	return elasticsearch.FormatIndices(w, indices)
}

func (c *listIndicesTableCodec) Decode(io.Reader, any) error {
	return errors.New("listIndicesTableCodec does not support decoding")
}
