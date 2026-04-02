package logs

import (
	"errors"
	"fmt"
	"io"

	internalconfig "github.com/grafana/gcx/internal/config"
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

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-loki-datasource is configured)")
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

func seriesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &seriesOpts{}

	cmd := &cobra.Command{
		Use:   "series",
		Short: "List log streams",
		Long:  "List log streams (series) from a Loki datasource using LogQL stream selectors. At least one --match selector is required.",
		Example: `
	# List series matching a selector (use datasource UID, not name)
	gcx logs series -d <datasource-uid> --match '{job="varlogs"}'

	# Match with regex and multiple labels
	gcx logs series -d <datasource-uid> --match '{container_name=~"prometheus.*", component="server"}'

	# Multiple matchers (OR logic)
	gcx logs series -d <datasource-uid> --match '{job="varlogs"}' --match '{namespace="default"}'

	# Output as JSON
	gcx logs series -d <datasource-uid> --match '{job="varlogs"}' -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := loader.LoadFullConfig(ctx)
				if err != nil {
					return err
				}
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "loki")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-loki-datasource in config")
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
