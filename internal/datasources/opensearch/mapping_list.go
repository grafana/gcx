package opensearch

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/spf13/cobra"
)

// mappingListSpec parameterizes the two commands that show one half of
// Client.Mapping's result (list-indices, list-fields), which differ only in
// wording, which half of the result they return, and how that half renders
// as a table.
type mappingListSpec struct {
	use, short, long, example string
	tokenCost, llmHint        string
	errNoun                   string // for "failed to list <errNoun>"
	result                    func(indices []opensearch.IndexInfo, fields []opensearch.FieldInfo) any
	formatTable               func(w io.Writer, data any) error
}

type mappingListOpts struct {
	IO         cmdio.Options
	Datasource string
	Index      string
}

func (opts *mappingListOpts) Validate() error {
	return opts.IO.Validate()
}

// newMappingListCmd builds a mapping-listing command from a spec.
func newMappingListCmd(loader *providers.ConfigLoader, spec mappingListSpec) *cobra.Command {
	opts := &mappingListOpts{}

	cmd := &cobra.Command{
		Use:     spec.use,
		Short:   spec.short,
		Long:    spec.long,
		Example: spec.example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if cmd.Flags().Changed("index") && opts.Index == "" {
				return errors.New("--index must not be empty")
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "opensearch")
			if err != nil {
				return err
			}

			client, err := opensearch.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			indices, fields, err := client.Mapping(ctx, datasourceUID, opts.Index)
			if err != nil {
				return fmt.Errorf("failed to list %s: %w", spec.errNoun, err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), spec.result(indices, fields))
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: spec.tokenCost,
		agent.AnnotationLLMHint:   spec.llmHint,
	}

	opts.IO.RegisterCustomCodec("table", &mappingListTableCodec{spec: spec})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.opensearch is configured)")
	cmd.Flags().StringVar(&opts.Index, "index", "", "Restrict to this index or index pattern")

	return cmd
}

type mappingListTableCodec struct {
	spec mappingListSpec
}

func (c *mappingListTableCodec) Format() format.Format { return "table" }

func (c *mappingListTableCodec) Encode(w io.Writer, data any) error {
	return c.spec.formatTable(w, data)
}

func (c *mappingListTableCodec) Decode(io.Reader, any) error {
	return errors.New("mappingListTableCodec does not support decoding")
}
