package metrics

import (
	"errors"
	"fmt"
	"io"
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
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
	Limit      int
}

func (opts *listOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &metricNamesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringArrayVar(&opts.Match, "match", nil, "Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)")
	flags.StringVar(&opts.Prefix, "prefix", "", "Only include names starting with this string")
	flags.StringVar(&opts.Suffix, "suffix", "", "Only include names ending with this string")
	flags.StringVar(&opts.Contains, "contains", "", "Only include names containing this string")
	flags.IntVar(&opts.Limit, "limit", 100, "Maximum number of names to return after filtering (0 for all)")
}

func (opts *listOpts) Validate() error {
	if opts.Limit < 0 {
		return errors.New("--limit must be zero or positive")
	}
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
		Short: "List metric names",
		Long: "List metric names from a Prometheus datasource via the label values endpoint for `__name__`.\n" +
			"Scope the server-side lookup with --match selectors; filter names client-side\n" +
			"with --prefix, --suffix, and --contains, which combine with AND.\n" +
			"Output is capped at 100 names by default; pass --limit 0 for the full list.",
		Args: cobra.NoArgs,
		Example: `
  # List metric names (first 100 by default; use datasource UID, not name)
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

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
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

			if total := len(resp.Data); opts.Limit > 0 && total > opts.Limit {
				resp.Data = resp.Data[:opts.Limit]
				cmdio.EmitHint(cmd.ErrOrStderr(), fmt.Sprintf("showing first %d of %d metric names; use --limit for more (0 for all)", opts.Limit, total), "")
			}

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
