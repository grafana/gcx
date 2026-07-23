package metrics

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

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

type listOpts struct {
	IO         cmdio.Options
	Datasource string
	Match      []string
	Prefix     string
	Suffix     string
	Contains   string
}

func (opts *listOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &metricNamesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringArrayVar(&opts.Match, "match", nil, "Series selector(s) to scope results; repeatable")
	flags.StringVar(&opts.Prefix, "prefix", "", "Only include names starting with this string")
	flags.StringVar(&opts.Suffix, "suffix", "", "Only include names ending with this string")
	flags.StringVar(&opts.Contains, "contains", "", "Only include names containing this string")
}

func (opts *listOpts) Validate() error {
	return opts.IO.Validate()
}

// filterMetricNames returns the names passing every configured name filter.
// Filters combine with AND; with no filters configured it returns names as-is.
func filterMetricNames(names []string, prefix, suffix, contains string) []string {
	if prefix == "" && suffix == "" && contains == "" {
		return names
	}

	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		if suffix != "" && !strings.HasSuffix(name, suffix) {
			continue
		}
		if contains != "" && !strings.Contains(name, contains) {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func listCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List metric names.",
		Long: `List metric names from a Prometheus datasource via the label values endpoint for __name__.
Scope the server-side lookup with --match selectors; filter names client-side
with --prefix, --suffix, and --contains, which combine with AND.`,
		Example: `
  # List all metric names (use datasource UID, not name)
  gcx metrics list -d UID

  # Find cart-related metrics
  gcx metrics list -d UID --contains cart

  # Counters only
  gcx metrics list -d UID --suffix _total

  # Metrics present on a job
  gcx metrics list -d UID --match '{job="api"}'

  # Output as JSON
  gcx metrics list -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
			if err != nil {
				return err
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.LabelValues(ctx, datasourceUID, "__name__", opts.Match)
			if err != nil {
				return fmt.Errorf("failed to list metric names: %w", err)
			}

			resp.Data = filterMetricNames(resp.Data, opts.Prefix, opts.Suffix, opts.Contains)

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type metricNamesTableCodec struct{}

func (c *metricNamesTableCodec) Format() format.Format {
	return "table"
}

func (c *metricNamesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.LabelsResponse)
	if !ok {
		return errors.New("invalid data type for metric names table codec")
	}

	return prometheus.FormatMetricNamesTable(w, resp)
}

func (c *metricNamesTableCodec) Decode(io.Reader, any) error {
	return errors.New("metric names table codec does not support decoding")
}
