package prometheus

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SearchOpts holds the flags common to all three search subcommands.
type SearchOpts struct {
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

func (opts *SearchOpts) SetupCommon(flags *pflag.FlagSet, withMetric bool) {
	opts.SetupTimeFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringArrayVar(&opts.Match, "match", nil, "PromQL series selector(s) restricting candidates; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)")
	if withMetric {
		flags.StringVar(&opts.Metric, "metric", "", "Only results from series of this metric name; mutually exclusive with --metric-regex")
		flags.StringVar(&opts.MetricRegex, "metric-regex", "", `Only results from series whose metric name matches this regex. Used exactly as given. To match all metric names which contain "kube" use ".*kube.*". Mutually exclusive with --metric.`)
	}
	flags.BoolVar(&opts.CaseSensitive, "case-sensitive", false, "Case-sensitive search term matching (case-insensitive by default)")
	flags.StringVar(&opts.FuzzAlg, "fuzz-alg", "jarowinkler", "Fuzzy match algorithm: jarowinkler or subsequence")
	flags.IntVar(&opts.FuzzThreshold, "fuzz-threshold", 70, "Minimum fuzzy match score 0-100 (with jarowinkler, 0 disables fuzzy matching, leaving substring matches only)")
	flags.StringVar(&opts.SortBy, "sort-by", "score", "Sort by: score (requires a search term) or alpha")
	flags.StringVar(&opts.SortDir, "sort-dir", "", "Sort direction for --sort-by alpha: asc (default) or dsc")
	flags.IntVar(&opts.Limit, "limit", 50, "Maximum results to return (0: unlimited on Mimir, subject to server-side caps; Prometheus requires a positive value)")
	flags.BoolVar(&opts.IncludeScore, "include-score", false, "Include each result's relevance score")
}

func (opts *SearchOpts) Validate() error {
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
	// The --sort-dir/--sort-by=score incompatibility is checked in ToOptions,
	// not here: ToOptions may downgrade SortBy to "alpha" when no term is
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

// ToOptions resolves the parsed flags into a prometheus.SearchOptions.
// SortBy falls back to alpha when no search term is issued and the caller
// didn't explicitly pass --sort-by; an explicit --sort-by=score with no
// term is left alone.
func (opts *SearchOpts) ToOptions(terms []string, sortByExplicit bool) (prometheus.SearchOptions, error) {
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
	if len(terms) == 0 && sortBy == "score" && !sortByExplicit {
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
		CaseSensitive:   new(opts.CaseSensitive),
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

func buildSearchResult[T any](results []T, hasMore bool, warnings []string, limit int) (*searchResult[T], *cmdio.ListMeta) {
	meta := cmdio.AttachListMeta(cmdio.PagedListMeta(len(results), limit, hasMore, 0), os.Args)
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
	opts := &SearchOpts{}

	cmd := &cobra.Command{
		Use:   "search-metric-names TERM...",
		Short: "Search metric names (experimental)",
		Long: `Search metric names from a Prometheus/Mimir datasource. At least one TERM is required.

This API allows for metric names to be discovered via a configurable fuzzy search. Multiple TERM values combine as OR.

Search terms can be augmented with matchers for additional filtering of considered series.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also the sibling label-name search and label-value search commands.`,
		Args: cobra.MinimumNArgs(1),
		Example: `
  # Fuzzy search metric names (use datasource UID, not name)
  gcx datasources prometheus search-metric-names http -d UID

  # Refine fuzzy search algorithm
  gcx datasources prometheus search-metric-names http -d UID --fuzz-alg=subsequence --fuzz-threshold=70

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

			searchOptions, err := opts.ToOptions(args, cmd.Flags().Changed("sort-by"))
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

			result, meta := buildSearchResult(resp.Results, resp.HasMore, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-metric-names TERM -d UID -o json",
	}

	opts.IO.RegisterCustomCodec("table", &searchMetricNamesTableCodec{includeScore: &opts.IncludeScore, includeMetadata: &opts.IncludeMetadata})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.SetupCommon(cmd.Flags(), false)
	cmd.Flags().BoolVar(&opts.IncludeMetadata, "include-metadata", false, "Include each result's metric type, help text, and unit (when available)")

	return cmd
}

// SearchLabelNamesCmd returns the `search-label-names` leaf command. See
// SearchMetricNamesCmd for why this is a flat leaf, not a `search` subgroup.
func SearchLabelNamesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &SearchOpts{}

	cmd := &cobra.Command{
		Use:   "search-label-names [TERM...]",
		Short: "Search label names (experimental)",
		Long: `Search label names from a Prometheus/Mimir datasource. Requires TERM (performs a fuzzy search; multiple TERM values combine as OR), or --metric / --metric-regex to scope by metric name instead.

sort_by=score (the default) requires a search term — omitting TERM in favor
of a metric scope falls back to sort_by=alpha unless --sort-by is set
explicitly.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also the sibling metric-name search and label-value search commands.`,
		Args: cobra.ArbitraryArgs,
		Example: `
  # Fuzzy search label names (use datasource UID, not name)
  gcx datasources prometheus search-label-names job -d UID

  # Show all label names available on a given metric
  gcx datasources prometheus search-label-names -d UID --metric http_requests_total

  # Search for label names on a given metric
  gcx datasources prometheus search-label-names namespace -d UID --metric http_requests_total

  # Search for label names across a range of metrics
  gcx datasources prometheus search-label-names namespace -d UID --metric-regex '.*kube.*'

  # Output as JSON
  gcx datasources prometheus search-label-names job -d UID -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if err := rejectExplicitlyEmptyFlags(cmd, "metric", "metric-regex"); err != nil {
				return err
			}

			if len(args) == 0 && opts.Metric == "" && opts.MetricRegex == "" {
				return errors.New("requires TERM, or --metric, or --metric-regex")
			}

			searchOptions, err := opts.ToOptions(args, cmd.Flags().Changed("sort-by"))
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

			result, meta := buildSearchResult(resp.Results, resp.HasMore, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-label-names TERM -d UID -o json",
	}

	opts.IO.RegisterCustomCodec("table", &searchLabelNamesTableCodec{includeScore: &opts.IncludeScore})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.SetupCommon(cmd.Flags(), true)

	return cmd
}

// SearchLabelValuesCmd returns the `search-label-values` leaf command. See
// SearchMetricNamesCmd for why this is a flat leaf, not a `search` subgroup.
func SearchLabelValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &SearchOpts{}

	cmd := &cobra.Command{
		Use:   "search-label-values LABEL [TERM...]",
		Short: "Search the values of a label (experimental)",
		Long: `Search the values of a single label from a Prometheus/Mimir datasource. LABEL is always required; TERM (multiple values combine as OR), --metric, and --metric-regex are all optional and may be combined or omitted — LABEL alone lists every value of that label.

sort_by=score (the default) requires a search term — omitting TERM falls
back to sort_by=alpha unless --sort-by is set explicitly.

--metric-regex is used exactly as given — PromQL anchors =~ at ^...$, so
"kube" matches only a metric literally named "kube", not one containing
it. Write ".*kube.*" for a contains search.

This API is experimental and disabled by default on both self-hosted
Prometheus (requires --enable-feature=search-api) and self-hosted Mimir
(requires -querier.experimental-search-api-enabled).

See also the sibling metric-name search and label-name search commands.`,
		Args: cobra.MinimumNArgs(1),
		Example: `
  # List every value of the "job" label (use datasource UID, not name)
  gcx datasources prometheus search-label-values job -d UID

  # Fuzzy search values of the "job" label
  gcx datasources prometheus search-label-values job pro -d UID

  # List every "job" label value present on a specific metric
  gcx datasources prometheus search-label-values job -d UID --metric http_requests_total

  # List every "job" label value present on a range of metrics
  gcx datasources prometheus search-label-values job -d UID --metric-regex '.*kube.*'

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
			if label == "" {
				return errors.New("invalid LABEL: value is empty (unset shell variable?)")
			}
			terms := args[1:]

			searchOptions, err := opts.ToOptions(terms, cmd.Flags().Changed("sort-by"))
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

			result, meta := buildSearchResult(resp.Results, resp.HasMore, resp.Warnings, opts.Limit)
			if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
				return err
			}
			emitSearchDiagnostics(cmd.ErrOrStderr(), resp.Warnings, meta)
			return nil
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus search-label-values LABEL TERM -d UID -o json",
	}

	opts.IO.RegisterCustomCodec("table", &searchLabelValuesTableCodec{includeScore: &opts.IncludeScore})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())
	opts.SetupCommon(cmd.Flags(), true)

	return cmd
}

type searchMetricNamesTableCodec struct {
	includeScore    *bool
	includeMetadata *bool
}

func (c *searchMetricNamesTableCodec) Format() format.Format { return "table" }

func (c *searchMetricNamesTableCodec) Encode(w io.Writer, data any) error {
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
			row = append(row, strconv.FormatFloat(r.Score, 'f', -1, 64))
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

type searchLabelNamesTableCodec struct {
	includeScore *bool
}

func (c *searchLabelNamesTableCodec) Format() format.Format { return "table" }

func (c *searchLabelNamesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*searchResult[prometheus.LabelNameResult])
	if !ok {
		return errors.New("invalid data type for search label-names table codec")
	}

	headers := []string{"NAME"}
	if *c.includeScore {
		headers = append(headers, "SCORE")
	}

	t := style.NewTable(headers...)
	for _, r := range resp.Results {
		row := []string{r.Name}
		if *c.includeScore {
			row = append(row, strconv.FormatFloat(r.Score, 'f', -1, 64))
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func (c *searchLabelNamesTableCodec) Decode(io.Reader, any) error {
	return errors.New("search label-names table codec does not support decoding")
}

type searchLabelValuesTableCodec struct {
	includeScore *bool
}

func (c *searchLabelValuesTableCodec) Format() format.Format { return "table" }

func (c *searchLabelValuesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*searchResult[prometheus.LabelValueResult])
	if !ok {
		return errors.New("invalid data type for search label-values table codec")
	}

	headers := []string{"VALUE"}
	if *c.includeScore {
		headers = append(headers, "SCORE")
	}

	t := style.NewTable(headers...)
	for _, r := range resp.Results {
		row := []string{r.Value}
		if *c.includeScore {
			row = append(row, strconv.FormatFloat(r.Score, 'f', -1, 64))
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func (c *searchLabelValuesTableCodec) Decode(io.Reader, any) error {
	return errors.New("search label-values table codec does not support decoding")
}
