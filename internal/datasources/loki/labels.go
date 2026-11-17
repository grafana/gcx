package loki

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type lokiLabelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
	Query      string
}

func (opts *lokiLabelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &lokiLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
	flags.StringVarP(&opts.Query, "query", "q", "", "LogQL stream selector to scope labels, e.g. '{app=\"foo\"}' (pipeline stages are not supported)")
}

func (opts *lokiLabelsOpts) Validate() error {
	return opts.IO.Validate()
}

func LabelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &lokiLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Args:  cobra.NoArgs,
		Long:  "List all labels or get values for a specific label from a Loki datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources loki labels -d UID

	# Get values for a specific label
	gcx datasources loki labels -d UID --label job

	# Filter labels with a query
	gcx datasources loki labels -d UID --query '{app="foo"}'

	# Filter label values with a query
	gcx datasources loki labels -d UID --label job --query '{app="foo"}'

	# Output as JSON
	gcx datasources loki labels -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// Reject an explicitly empty --query/--label (unset shell var)
			// instead of silently dropping scoping.
			for _, name := range []string{"query", "label"} {
				if cmd.Flags().Changed(name) && cmd.Flags().Lookup(name).Value.String() == "" {
					return fmt.Errorf("invalid --%s: value is empty (unset shell variable?)", name)
				}
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, opts.Label, opts.Query)
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.Labels(ctx, datasourceUID, opts.Query)
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources loki labels -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}

type lokiLabelsTableCodec struct{}

func (c *lokiLabelsTableCodec) Format() format.Format {
	return "table"
}

func (c *lokiLabelsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.LabelsResponse)
	if !ok {
		return errors.New("invalid data type for loki labels table codec")
	}

	return loki.FormatLabelsTable(w, resp)
}

func (c *lokiLabelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("loki labels table codec does not support decoding")
}
