package prometheus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/host"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// searchAPINotes documents the server behavior all three search commands
// share.
const searchAPINotes = `Without --from/--to or --since, the server searches only the last hour; use
--since (for example --since 7d) to look further back.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).`

// searchOpts holds the flags common to all three search subcommands.
type searchOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	Match      []string

	// Metric is a short-cut to setting a matcher to expressly filter to a specific
	// metric name. When used, it adds a {__name__="<metric>"} match entry.
	// This is available on the label-names and label-values search commands.
	Metric string

	// MetricRegex is similar to Metric but is handled as a raw regex.
	// When used, it adds a {__name__=~"<metric_regex>"} match entry.
	// Note that Metric and MetricRegex are mutually exclusive.
	MetricRegex string

	CaseSensitive   bool
	FuzzAlg         string
	FuzzThreshold   int
	SortBy          string
	SortDir         string
	Limit           int
	IncludeScore    bool
	IncludeMetadata bool
}

func (opts *searchOpts) setup(flags *pflag.FlagSet, withMetric bool) {
	opts.SetupTimeFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringArrayVar(&opts.Match, "match", nil, "PromQL series selector(s) restricting candidates; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)")
	if withMetric {
		flags.StringVar(&opts.Metric, "metric", "", "Only results from series of this metric name; mutually exclusive with --metric-regex")
		flags.StringVar(&opts.MetricRegex, "metric-regex", "", `Only results from series whose metric name matches this regex. Used exactly as given. To match all metric names which contain "kube" use ".*kube.*". Mutually exclusive with --metric.`)
	}
	flags.BoolVar(&opts.CaseSensitive, "case-sensitive", false, "Case-sensitive search term matching (case-insensitive by default)")
	flags.StringVar(&opts.FuzzAlg, "fuzz-alg", "jarowinkler", "Fuzzy match algorithm: jarowinkler or subsequence")
	flags.IntVar(&opts.FuzzThreshold, "fuzz-threshold", 70, "Minimum fuzzy match score as a percentage, 0-100; scores are reported from 0 to 1. With jarowinkler the threshold applies only to fuzzy matches: substring matches are always kept, and 0 turns fuzzy matching off")
	flags.StringVar(&opts.SortBy, "sort-by", "score", "Sort by: score (requires a search term) or alpha")
	flags.StringVar(&opts.SortDir, "sort-dir", "", "Sort direction for --sort-by alpha: asc (default) or dsc")
	flags.IntVar(&opts.Limit, "limit", 50, "Maximum results to return (0: unlimited on Mimir, subject to server-side caps; Prometheus requires a positive value)")
	flags.BoolVar(&opts.IncludeScore, "include-score", false, "Include each result's relevance score (0 to 1; higher is a closer match)")
}

func (opts *searchOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	switch opts.SortBy {
	case "score", "alpha":
	default:
		return fmt.Errorf(`invalid --sort-by %q: must be "score" or "alpha"`, opts.SortBy)
	}
	switch opts.SortDir {
	case "", "asc", "dsc":
	default:
		return fmt.Errorf(`invalid --sort-dir %q: must be "asc" or "dsc"`, opts.SortDir)
	}
	// The --sort-dir/--sort-by=score incompatibility is checked in toOptions,
	// not here: toOptions may downgrade SortBy to "alpha" when no term is
	// given, and that resolved value — not the raw flag default — is what
	// determines compatibility.
	switch opts.FuzzAlg {
	case "jarowinkler", "subsequence":
	default:
		return fmt.Errorf(`invalid --fuzz-alg %q: must be "jarowinkler" or "subsequence"`, opts.FuzzAlg)
	}
	if opts.FuzzThreshold < 0 || opts.FuzzThreshold > 100 {
		return fmt.Errorf("invalid --fuzz-threshold %d: must be between 0 and 100", opts.FuzzThreshold)
	}
	if opts.Limit < 0 {
		return fmt.Errorf("invalid --limit %d: must be >= 0 (0 means unlimited on Mimir)", opts.Limit)
	}

	return opts.ValidateTimeRange()
}

// toOptions resolves the parsed flags and search terms into a
// prometheus.SearchOptions, rejecting before any I/O the inputs both servers
// reject. SortBy falls back to alpha when no term is given and --sort-by was
// not passed explicitly.
func (opts *searchOpts) toOptions(terms []string, sortByExplicit bool) (prometheus.SearchOptions, error) {
	for _, term := range terms {
		// An empty term, typically an unset shell variable, makes Prometheus
		// drop the filter and return every name instead of failing.
		if strings.TrimSpace(term) == "" {
			return prometheus.SearchOptions{}, fmt.Errorf("invalid TERM %q: value is empty or blank (unset shell variable?)", term)
		}
	}

	start, end, err := opts.ParseTimeRange(time.Now())
	if err != nil {
		return prometheus.SearchOptions{}, err
	}

	nameMatcher, flagName, err := resolveNameMatcher(opts.Metric, opts.MetricRegex)
	if err != nil {
		return prometheus.SearchOptions{}, err
	}

	match, err := foldNameMatcherSelector(flagName, nameMatcher, opts.Match)
	if err != nil {
		return prometheus.SearchOptions{}, err
	}

	sortBy := opts.SortBy
	if len(terms) == 0 && sortBy == "score" {
		if sortByExplicit {
			return prometheus.SearchOptions{}, errors.New("--sort-by=score requires a search TERM; pass a TERM or use --sort-by=alpha")
		}
		sortBy = "alpha"
	}
	if opts.SortDir != "" && sortBy == "score" {
		return prometheus.SearchOptions{}, errors.New("--sort-dir is incompatible with --sort-by=score; sort direction only applies to --sort-by=alpha")
	}

	return prometheus.SearchOptions{
		Search:          terms,
		Match:           match,
		Start:           start,
		End:             end,
		CaseSensitive:   opts.CaseSensitive,
		FuzzAlg:         opts.FuzzAlg,
		FuzzThreshold:   opts.FuzzThreshold,
		SortBy:          sortBy,
		SortDir:         opts.SortDir,
		Limit:           opts.Limit,
		IncludeScore:    opts.IncludeScore,
		IncludeMetadata: opts.IncludeMetadata,
	}, nil
}

// resolveClient loads the config/context, resolves the datasource UID, and
// builds a query client. Shared by all three search subcommands.
func resolveClient(cmd *cobra.Command, loader *providers.ConfigLoader, datasource string) (string, *prometheus.Client, error) {
	ctx := cmd.Context()

	cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
	if err != nil {
		return "", nil, err
	}

	datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "prometheus")
	if err != nil {
		return "", nil, err
	}

	client, err := prometheus.NewClient(cfg)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create client: %w", err)
	}

	return datasourceUID, client, nil
}

type searchResult[T any] struct {
	Results  []T             `json:"results" yaml:"results"`
	Warnings []string        `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	ListMeta *cmdio.ListMeta `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func buildSearchResult[T any](ctx context.Context, results []T, hasMore bool, incomplete bool, warnings []string, limit int) (*searchResult[T], *cmdio.ListMeta) {
	var safetyCap int
	if limit <= 0 || incomplete {
		// Either --limit 0 asked for everything, or the stream itself was
		// cut short (decodeSearchStream's own size cap, or an interrupted
		// connection) rather than stopping because a real trailer reported
		// has_more=true. Either way this is a bound gcx did not request and
		// a larger --limit cannot fix — including when the user's --limit
		// was already positive: a stream cut after 2 of a requested 5 must
		// not suggest "--limit 4", which is both smaller than what was
		// already asked for and contradicts the incomplete-stream warning's
		// own "request a smaller limit" advice. So this is the cap case, not
		// the "ask for more" case: no continuation, refine filters instead
		// (see PagedListMeta).
		safetyCap = len(results)
	}
	meta := cmdio.AttachListMeta(cmdio.PagedListMeta(len(results), limit, hasMore, safetyCap), host.Args(ctx))
	return &searchResult[T]{Results: results, Warnings: warnings, ListMeta: meta}, meta
}

// emitSearchDiagnostics writes each server warning and the standard
// truncation hint to stderr, after the main payload has been written to
// stdout. No-op for warnings when empty.
func emitSearchDiagnostics(w io.Writer, warnings []string, meta *cmdio.ListMeta) {
	for _, warning := range warnings {
		cmdio.EmitWarn(w, warning)
	}
	cmdio.EmitListTruncationHint(w, meta)
}

// SearchMetricNamesCmd returns the `search-metric-names` leaf command.
//
// This is one of three sibling leaf commands (see also SearchLabelNamesCmd
// and SearchLabelValuesCmd). Each is a flat `<operation>-<subject>` leaf
// rather than children of a `search` subgroup, per the compound-verb rule in
// docs/design/command-naming.md — a `search` group with `metric-names`/
// `label-names`/`label-values` children would not pass the canonical-verb
// naming gate.
func SearchMetricNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:   "search-metric-names TERM...",
		Short: "[experimental] Search metric names",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Search metric names from a Prometheus/Mimir datasource. At least one TERM is required.

This API allows for metric names to be discovered via a configurable fuzzy search. Multiple TERM values combine as OR.

Search terms can be augmented with matchers for additional filtering of considered series.

` + searchAPINotes + `

See also the sibling label-name search and label-value search commands.`,
		Args: cobra.MinimumNArgs(1),
		Example: `
  # Fuzzy search metric names (use datasource UID, not name)
  gcx datasources prometheus search-metric-names http -d UID

  # Search metric names seen in the last 7 days (default: the last hour)
  gcx datasources prometheus search-metric-names http -d UID --since 7d

  # Refine fuzzy search algorithm
  gcx datasources prometheus search-metric-names http -d UID --fuzz-alg=subsequence --fuzz-threshold=85

  # Limit result sets and control ordering
  gcx datasources prometheus search-metric-names http -d UID --limit=10 --sort-by=alpha

  # Include relevance score and metric metadata
  gcx datasources prometheus search-metric-names http -d UID --include-score --include-metadata

  # Output as JSON
  gcx datasources prometheus search-metric-names http -d UID -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			searchOptions, err := opts.toOptions(args, cmd.Flags().Changed("sort-by"))
			if err != nil {
				return err
			}

			datasourceUID, client, err := resolveClient(cmd, loader, opts.Datasource)
			if err != nil {
				return err
			}

			resp, err := client.SearchMetricNames(cmd.Context(), datasourceUID, searchOptions)
			if err != nil {
				return fmt.Errorf("failed to search metric names: %w", err)
			}

			result, meta := buildSearchResult(cmd.Context(), resp.Results, resp.HasMore, resp.Incomplete, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.Context(), cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-metric-names TERM -d UID -o json",
		agent.AnnotationStability: agent.StabilityExperimental,
	}

	opts.IO.RegisterCustomCodec("table", &searchMetricNamesTableCodec{includeScore: &opts.IncludeScore, includeMetadata: &opts.IncludeMetadata})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags(), false)
	cmd.Flags().BoolVar(&opts.IncludeMetadata, "include-metadata", false, "Include each result's metric type, help text, and unit (when available)")

	return cmd
}

// SearchLabelNamesCmd returns the `search-label-names` leaf command. See
// SearchMetricNamesCmd for why this is a flat leaf, not a `search` subgroup.
func SearchLabelNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:   "search-label-names [TERM...]",
		Short: "[experimental] Search label names",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Search label names from a Prometheus/Mimir datasource. Requires TERM (performs a fuzzy search; multiple TERM values combine as OR), or a scope instead: --metric, --metric-regex or --match.

sort_by=score (the default) requires a search term — omitting TERM in favor
of a scope falls back to sort_by=alpha, and an explicit --sort-by=score
without TERM is rejected.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

` + searchAPINotes + `

See also the sibling metric-name search and label-value search commands.`,
		Args: cobra.ArbitraryArgs,
		Example: `
  # Fuzzy search label names (use datasource UID, not name)
  gcx datasources prometheus search-label-names job -d UID

  # Show label names available on a given metric
  gcx datasources prometheus search-label-names -d UID --metric http_requests_total

  # Show label names on series matching a selector
  gcx datasources prometheus search-label-names -d UID --match '{job="api"}'

  # Search for label names on a given metric
  gcx datasources prometheus search-label-names namespace -d UID --metric http_requests_total

  # Search for label names across a range of metrics
  gcx datasources prometheus search-label-names namespace -d UID --metric-regex '.*kube.*'

  # Search label names seen in the last 7 days (default: the last hour)
  gcx datasources prometheus search-label-names job -d UID --since 7d

  # Output as JSON
  gcx datasources prometheus search-label-names job -d UID -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if err := rejectExplicitlyEmptyFlags(cmd, "metric", "metric-regex"); err != nil {
				return err
			}

			if len(args) == 0 && opts.Metric == "" && opts.MetricRegex == "" && len(opts.Match) == 0 {
				return errors.New("requires TERM, or a scope: --metric, --metric-regex or --match")
			}

			searchOptions, err := opts.toOptions(args, cmd.Flags().Changed("sort-by"))
			if err != nil {
				return err
			}

			datasourceUID, client, err := resolveClient(cmd, loader, opts.Datasource)
			if err != nil {
				return err
			}

			resp, err := client.SearchLabelNames(cmd.Context(), datasourceUID, searchOptions)
			if err != nil {
				return fmt.Errorf("failed to search label names: %w", err)
			}

			result, meta := buildSearchResult(cmd.Context(), resp.Results, resp.HasMore, resp.Incomplete, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.Context(), cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-label-names TERM -d UID -o json",
		agent.AnnotationStability: agent.StabilityExperimental,
	}

	opts.IO.RegisterCustomCodec("table", &searchValueTableCodec[prometheus.LabelNameResult]{
		header:       "NAME",
		row:          func(r prometheus.LabelNameResult) (string, float64) { return r.Name, r.Score },
		includeScore: &opts.IncludeScore,
	})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags(), true)

	return cmd
}

// SearchLabelValuesCmd returns the `search-label-values` leaf command. See
// SearchMetricNamesCmd for why this is a flat leaf, not a `search` subgroup.
func SearchLabelValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}

	cmd := &cobra.Command{
		Use:   "search-label-values LABEL [TERM...]",
		Short: "[experimental] Search the values of a label",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Search the values of a single label from a Prometheus/Mimir datasource. LABEL is always required; TERM (multiple values combine as OR), --metric, --metric-regex and --match are all optional and may be combined or omitted — LABEL alone lists that label's values.

sort_by=score (the default) requires a search term — omitting TERM falls
back to sort_by=alpha, and an explicit --sort-by=score without TERM is
rejected.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

` + searchAPINotes + `

See also the sibling metric-name search and label-name search commands.`,
		Args: cobra.MinimumNArgs(1),
		Example: `
  # List values of the "job" label (use datasource UID, not name)
  gcx datasources prometheus search-label-values job -d UID

  # Fuzzy search values of the "job" label
  gcx datasources prometheus search-label-values job pro -d UID

  # List "job" label values present on a specific metric
  gcx datasources prometheus search-label-values job -d UID --metric http_requests_total

  # List "job" label values present on a range of metrics
  gcx datasources prometheus search-label-values job -d UID --metric-regex '.*kube.*'

  # List "job" label values seen in the last 7 days (default: the last hour)
  gcx datasources prometheus search-label-values job -d UID --since 7d

  # Output as JSON
  gcx datasources prometheus search-label-values job pro -d UID -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if err := rejectExplicitlyEmptyFlags(cmd, "metric", "metric-regex"); err != nil {
				return err
			}

			label := args[0]
			if strings.TrimSpace(label) == "" {
				return errors.New("invalid LABEL: value is empty or blank (unset shell variable?)")
			}
			terms := args[1:]

			searchOptions, err := opts.toOptions(terms, cmd.Flags().Changed("sort-by"))
			if err != nil {
				return err
			}

			datasourceUID, client, err := resolveClient(cmd, loader, opts.Datasource)
			if err != nil {
				return err
			}

			resp, err := client.SearchLabelValues(cmd.Context(), datasourceUID, label, searchOptions)
			if err != nil {
				return fmt.Errorf("failed to search label values: %w", err)
			}

			result, meta := buildSearchResult(cmd.Context(), resp.Results, resp.HasMore, resp.Incomplete, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.Context(), cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-label-values LABEL TERM -d UID -o json",
		agent.AnnotationStability: agent.StabilityExperimental,
	}

	opts.IO.RegisterCustomCodec("table", &searchValueTableCodec[prometheus.LabelValueResult]{
		header:       "VALUE",
		row:          func(r prometheus.LabelValueResult) (string, float64) { return r.Value, r.Score },
		includeScore: &opts.IncludeScore,
	})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.setup(cmd.Flags(), true)

	return cmd
}

type searchMetricNamesTableCodec struct {
	includeScore    *bool
	includeMetadata *bool
}

func (c *searchMetricNamesTableCodec) Format() format.Format { return "table" }

func (c *searchMetricNamesTableCodec) Encode(ctx context.Context, w io.Writer, data any) error {
	resp, ok := data.(*searchResult[prometheus.MetricNameResult])
	if !ok {
		return errors.New("invalid data type for search metric-names table codec")
	}

	headers := []string{"NAME"}
	if *c.includeScore {
		headers = append(headers, "SCORE")
	}
	if *c.includeMetadata {
		headers = append(headers, "TYPE", "HELP", "UNIT")
	}

	t := style.NewTable(headers...)
	for _, r := range resp.Results {
		row := []string{r.Name}
		if *c.includeScore {
			row = append(row, strconv.FormatFloat(r.Score, 'f', 4, 64))
		}
		if *c.includeMetadata {
			row = append(row, r.Type, r.Help, r.Unit)
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func (c *searchMetricNamesTableCodec) Decode(io.Reader, any) error {
	return errors.New("search metric-names table codec does not support decoding")
}

// searchValueTableCodec renders a label-name or label-value search result as
// a single-column table, plus a SCORE column when scores were requested.
type searchValueTableCodec[T any] struct {
	header       string
	row          func(T) (value string, score float64)
	includeScore *bool
}

func (c *searchValueTableCodec[T]) Format() format.Format { return "table" }

func (c *searchValueTableCodec[T]) Encode(ctx context.Context, w io.Writer, data any) error {
	resp, ok := data.(*searchResult[T])
	if !ok {
		return errors.New("invalid data type for search table codec")
	}

	headers := []string{c.header}
	if *c.includeScore {
		headers = append(headers, "SCORE")
	}

	t := style.NewTable(headers...)
	for _, r := range resp.Results {
		value, score := c.row(r)
		row := []string{value}
		if *c.includeScore {
			row = append(row, strconv.FormatFloat(score, 'f', 4, 64))
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func (c *searchValueTableCodec[T]) Decode(io.Reader, any) error {
	return errors.New("search table codec does not support decoding")
}
