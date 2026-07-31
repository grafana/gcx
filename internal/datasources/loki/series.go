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

type seriesOpts struct {
	IO         cmdio.Options
	Datasource string
	Matchers   []string
}

func (opts *seriesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &lokiSeriesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	flags.StringArrayVarP(&opts.Matchers, "match", "M", nil, "LogQL stream selector (required, e.g., '{job=\"varlogs\"}')")
}

func (opts *seriesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if len(opts.Matchers) == 0 {
		return errors.New("at least one --match selector is required")
	}
	return nil
}

func SeriesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &seriesOpts{}

	cmd := &cobra.Command{
		Use:   "series",
		Short: "List log streams",
		Long:  "List log streams (series) from a Loki datasource using LogQL stream selectors. At least one --match selector is required.",
		Example: `
	# List series matching a selector (use datasource UID, not name)
	gcx datasources loki series -d UID --match '{job="varlogs"}'

	# Match with regex and multiple labels
	gcx datasources loki series -d UID --match '{container_name=~"prometheus.*", component="server"}'

	# Multiple matchers (OR logic)
	gcx datasources loki series -d UID --match '{job="varlogs"}' --match '{namespace="default"}'

	# Output as JSON
	gcx datasources loki series -d UID --match '{job="varlogs"}' -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
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

			resp, err := client.Series(ctx, datasourceUID, opts.Matchers)
			if err != nil {
				return fmt.Errorf("failed to get series: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return loki.FormatSeriesTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources loki series -d UID --match '{job="varlogs"}' -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

type lokiSeriesTableCodec struct{}

func (c *lokiSeriesTableCodec) Format() format.Format {
	return "table"
}

func (c *lokiSeriesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for series table codec")
	}

	return loki.FormatSeriesTable(w, resp)
}

func (c *lokiSeriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("series table codec does not support decoding")
}
