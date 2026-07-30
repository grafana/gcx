package metrics

import (
	"fmt"
	"os"
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
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
	opts.IO.RegisterCustomCodec("table", &prometheus.SingleColumnTableCodec{
		Header: "METRIC",
		Rows: func(data any) ([]string, bool) {
			result, ok := data.(*metricNamesListResult)
			if !ok {
				return nil, false
			}
			return result.Data, true
		},
	})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringArrayVar(&opts.Match, "match", nil, "Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)")
	flags.StringVar(&opts.Prefix, "prefix", "", "Only include names starting with this string")
	flags.StringVar(&opts.Suffix, "suffix", "", "Only include names ending with this string")
	flags.StringVar(&opts.Contains, "contains", "", "Only include names containing this string")
	// Cheaply complete source (the fetch always returns the full name set),
	// so the limit is purely a display trim; the default cap keeps the
	// command's advertised small token cost honest.
	opts.IO.BindListLimit(flags, &opts.Limit, "metric names", 100)
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
		Use:   "list-names",
		Short: "List metric names",
		Long: "List metric names from a Prometheus datasource via the label values endpoint for `__name__`.\n" +
			"Scope the server-side lookup with --match selectors; filter names client-side\n" +
			"with --prefix, --suffix, and --contains, which combine with AND.\n" +
			"Output is capped at 100 names by default; pass --limit 0 for the full list.",
		Args: cobra.NoArgs,
		Example: `
  # List metric names (first 100 by default; use datasource UID, not name)
  gcx metrics list-names -d UID

  # Find cart-related metrics
  gcx metrics list-names -d UID --contains cart

  # Counters only
  gcx metrics list-names -d UID --suffix _total

  # Metrics present on a job
  gcx metrics list-names -d UID --match '{job="api"}'

  # Output as JSON
  gcx metrics list-names -d UID -o json`,
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

			filtered := filterMetricNames(resp.Data, opts.Prefix, opts.Suffix, opts.Contains)

			// Fully-fetched source, so the observed total is exact. Truncation
			// is machine-legible (list_meta in the envelope) and human-legible
			// (stderr hint), per the list truncation contract.
			names, meta := cmdio.TruncateCompleteList(filtered, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)

			if err := opts.IO.Encode(cmd.OutOrStdout(), &metricNamesListResult{Data: names, ListMeta: meta}); err != nil {
				return err
			}
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// metricNamesListResult is the single shape passed to every codec for
// `gcx metrics list-names`. JSON/YAML serialize the envelope; the table codec
// extracts .Data to render rows (Pattern 13: format-agnostic data).
type metricNamesListResult struct {
	Data []string `json:"data" yaml:"data"`
	// ListMeta is attached only when the output is a truncated page, so
	// agents cannot mistake a page for the complete set. Reserved key;
	// see docs/design/output.md § List Truncation Contract.
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}
