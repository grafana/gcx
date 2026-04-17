package metrics

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type seriesOpts struct {
	IO         cmdio.Options
	Datasource string
	TimeRange  dsquery.TimeRangeOpts
	Match      []string
}

func (opts *seriesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &seriesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-prometheus-datasource is configured)")
	flags.StringSliceVar(&opts.Match, "match", nil, "Additional series selector(s); repeatable")
	opts.TimeRange.SetupTimeFlags(flags)
}

func (opts *seriesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.TimeRange.ValidateTimeRange()
}

// runSeries executes a Prometheus /api/v1/series lookup against datasourceUID.
// It is extracted so the billing subcommands can reuse the same flow with a
// pre-selected datasource.
func runSeries(cmd *cobra.Command, loader *providers.ConfigLoader, opts *seriesOpts, positional []string, datasourceDefault string) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	selectors := append([]string{}, opts.Match...)
	selectors = append(selectors, positional...)
	if len(selectors) == 0 {
		return errors.New("at least one selector is required (positional arg or --match)")
	}

	ctx := cmd.Context()

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return err
	}

	var cfgCtx *internalconfig.Context
	fullCfg, err := loader.LoadFullConfig(ctx)
	if err != nil {
		logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
	} else {
		cfgCtx = fullCfg.GetCurrentContext()
	}

	datasource := opts.Datasource
	if datasource == "" {
		datasource = datasourceDefault
	}

	datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "prometheus")
	if err != nil {
		return err
	}

	start, end, err := opts.TimeRange.ParseTimeRange(time.Now())
	if err != nil {
		return err
	}

	client, err := prometheus.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	resp, err := client.Series(ctx, datasourceUID, selectors, start, end)
	if err != nil {
		return fmt.Errorf("failed to list series: %w", err)
	}

	if opts.IO.OutputFormat == "table" {
		return prometheus.FormatSeriesTable(cmd.OutOrStdout(), resp)
	}

	return opts.IO.Encode(cmd.OutOrStdout(), resp)
}

func seriesCmd(loader *providers.ConfigLoader) *cobra.Command {
	return newSeriesCmd(loader, "")
}

func newSeriesCmd(loader *providers.ConfigLoader, defaultDS string) *cobra.Command {
	opts := &seriesOpts{}

	cmd := &cobra.Command{
		Use:   "series [SELECTOR]",
		Short: "List time series matching one or more selectors",
		Long: `List time series matching one or more selectors via the Prometheus /api/v1/series endpoint.

A selector can be passed as a positional argument and/or via --match (repeatable).
Time range defaults to unbounded; pass --since, or --from/--to, to scope.`,
		Example: `
  # Match a single metric family
  gcx metrics series -d <datasource-uid> '{__name__="up"}'

  # Match multiple selectors scoped to the last hour
  gcx metrics series -d <datasource-uid> --match '{job="grafana"}' --match '{job="loki"}' --since 1h

  # Output as JSON
  gcx metrics series -d <datasource-uid> '{__name__=~"grafanacloud_.*"}' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSeries(cmd, loader, opts, args, defaultDS)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type seriesTableCodec struct{}

func (c *seriesTableCodec) Format() format.Format {
	return "table"
}

func (c *seriesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for series table codec")
	}

	return prometheus.FormatSeriesTable(w, resp)
}

func (c *seriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("series table codec does not support decoding")
}
