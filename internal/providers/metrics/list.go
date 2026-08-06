package metrics

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"
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
	flags.StringVar(&opts.Prefix, "prefix", "", "Only include names starting with this string (case-sensitive)")
	flags.StringVar(&opts.Suffix, "suffix", "", "Only include names ending with this string (case-sensitive)")
	flags.StringVar(&opts.Contains, "contains", "", "Only include names containing this string (case-sensitive)")
	// The limit is pushed down via the label values endpoint's limit query
	// param, over-fetched by one so truncation stays detectable; the default
	// cap keeps the command's advertised small token cost honest.
	opts.IO.BindListLimit(flags, &opts.Limit, "metric names", 100)
}

func (opts *listOpts) Validate() error {
	return opts.IO.Validate()
}

// nameFilterMatchers renders the configured name filters as __name__ regex
// matchers. Prometheus regex matchers are fully anchored, and multiple
// matchers on the same label within one selector combine with AND — which a
// single regex could not express here: the filters may overlap in the name
// (--contains total --suffix _total both match the tail of "abc_total"), and
// RE2 has no lookaheads. Filter strings are QuoteMeta-escaped so they keep
// their documented literal, case-sensitive semantics.
func nameFilterMatchers(prefix, suffix, contains string) []*promlabels.Matcher {
	var matchers []*promlabels.Matcher
	if prefix != "" {
		matchers = append(matchers, promlabels.MustNewMatcher(promlabels.MatchRegexp, model.MetricNameLabel, regexp.QuoteMeta(prefix)+".*"))
	}
	if suffix != "" {
		matchers = append(matchers, promlabels.MustNewMatcher(promlabels.MatchRegexp, model.MetricNameLabel, ".*"+regexp.QuoteMeta(suffix)))
	}
	if contains != "" {
		matchers = append(matchers, promlabels.MustNewMatcher(promlabels.MatchRegexp, model.MetricNameLabel, ".*"+regexp.QuoteMeta(contains)+".*"))
	}
	return matchers
}

// nameFiltersAccept reports whether a literal metric name passes every
// configured name filter — the same AND semantics the folded regex matchers
// apply server-side.
func nameFiltersAccept(name, prefix, suffix, contains string) bool {
	if prefix != "" && !strings.HasPrefix(name, prefix) {
		return false
	}
	if suffix != "" && !strings.HasSuffix(name, suffix) {
		return false
	}
	return contains == "" || strings.Contains(name, contains)
}

// renderSelector brace-joins canonical Matcher.String() renderings.
// Brace-joining is safe despite Pattern 14's no-string-formatting rule:
// every part is a canonical rendering, and promql-builder cannot express a
// matcher-only selector (same justification as the labels command).
func renderSelector(matchers []*promlabels.Matcher) string {
	parts := make([]string, 0, len(matchers))
	for _, m := range matchers {
		parts = append(parts, m.String())
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// selectors returns the match[] selectors to send. The name filters are
// folded into every --match selector as __name__ regex matchers, so they
// always narrow: repeated --match selectors remain a union (the Prometheus
// API returns values from series matching any match[] parameter), and the
// filters constrain each branch of that union. Selectors are validated
// client-side either way, so a typo fails here with the selector named
// instead of an opaque proxied 400.
func (opts *listOpts) selectors() ([]string, error) {
	filters := nameFilterMatchers(opts.Prefix, opts.Suffix, opts.Contains)

	if len(opts.Match) == 0 {
		if len(filters) == 0 {
			return nil, nil
		}
		return []string{renderSelector(filters)}, nil
	}

	parser := promparser.NewParser(promparser.Options{})

	if len(filters) == 0 {
		// Valid selectors are sent exactly as written.
		for _, sel := range opts.Match {
			if _, err := parser.ParseMetricSelector(sel); err != nil {
				return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
			}
		}
		return opts.Match, nil
	}

	folded := make([]string, 0, len(opts.Match))
	for _, sel := range opts.Match {
		matchers, err := parser.ParseMetricSelector(sel)
		if err != nil {
			return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
		}

		// A selector may already pin __name__ to a literal (a bare metric
		// name or an explicit equality matcher). A literal the filters
		// reject would silently match nothing, so reject it instead — the
		// same contradiction rule the labels command applies to --metric. A
		// literal the filters accept makes the regex matchers redundant for
		// this selector.
		redundant := false
		for _, m := range matchers {
			if m.Name != model.MetricNameLabel || m.Type != promlabels.MatchEqual {
				continue
			}
			if !nameFiltersAccept(m.Value, opts.Prefix, opts.Suffix, opts.Contains) {
				return nil, fmt.Errorf("name filters (--prefix/--suffix/--contains) contradict the __name__ matcher in --match selector %q: the intersection matches nothing", sel)
			}
			redundant = true
		}
		if redundant {
			folded = append(folded, sel)
			continue
		}

		folded = append(folded, renderSelector(append(matchers, filters...)))
	}
	return folded, nil
}

func listCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}

	cmd := &cobra.Command{
		Use:   "list-names",
		Short: "List metric names",
		Long: "List metric names from a Prometheus datasource via the label values endpoint for `__name__`.\n" +
			"Scope the server-side lookup with --match selectors; narrow names with --prefix,\n" +
			"--suffix, and --contains, which combine with AND and are pushed down to the\n" +
			"server as `__name__` regex matchers in match[].\n" +
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

			// An explicitly empty name filter (typically an unset shell
			// variable, as in --contains "$SVC") would silently read as no
			// filter and answer with unfiltered names; error instead, like
			// the labels command's --metric guard.
			for _, name := range []string{"prefix", "suffix", "contains"} {
				if cmd.Flags().Changed(name) && cmd.Flags().Lookup(name).Value.String() == "" {
					return fmt.Errorf("invalid --%s: value is empty (unset shell variable?)", name)
				}
			}

			selectors, err := opts.selectors()
			if err != nil {
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

			// Push the limit down so the server truncates instead of gcx,
			// over-fetching by one: the spare item proves more names exist
			// without draining the source. Safe alongside the pushed-down
			// filters only — a server-side limit applied before client-side
			// filtering would truncate the wrong set.
			serverLimit := 0
			if opts.Limit > 0 {
				serverLimit = opts.Limit + 1
			}

			resp, err := client.LabelValues(ctx, datasourceUID, "__name__", selectors, serverLimit)
			if err != nil {
				return fmt.Errorf("failed to list metric names: %w", err)
			}

			names := resp.Data
			if names == nil {
				// The envelope's data field must serialize as [], not null.
				names = []string{}
			}

			// Truncation is machine-legible (list_meta in the envelope) and
			// human-legible (stderr hint), per the list truncation contract.
			var meta *cmdio.ListMeta
			switch {
			case opts.Limit <= 0:
				// No limit sent; the response is the complete set.
			case len(names) > opts.Limit+1:
				// The backend ignored the limit param, so the response is
				// the complete set and the observed total is exact.
				names, meta = cmdio.TruncateCompleteList(names, opts.Limit)
			default:
				// Server-side truncation, detected via the over-fetch spare:
				// the source was not drained, so the total stays unknown.
				names, meta = cmdio.TruncatePagedList(names, opts.Limit)
			}
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
