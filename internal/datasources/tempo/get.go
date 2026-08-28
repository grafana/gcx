package tempo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Share      dsquery.ExploreLinkOpts
	Datasource string
	LLM        bool

	// V2 spanset filter. KeepHierarchy, MatchDepth, and AncestorDepth are
	// ignored by Tempo unless Query is also set.
	Query         string
	KeepHierarchy bool
	MatchDepth    int
	AncestorDepth int

	// Span pruning. GroupBy/MinSpans/MaxParentDepth are ignored by Tempo
	// unless pruning ends up enabled (explicitly, or via the datasource's
	// tenant default).
	SpanPruning               bool
	SpanPruningGroupBy        string
	SpanPruningMinSpans       int
	SpanPruningMaxParentDepth int
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	// Default is the human-readable tree table for all non-agent sessions.
	// Piped output renders the same table without ANSI styling (via IsStylingEnabled).
	// Agent mode is the only path that overrides the default to JSON.
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.BoolVar(&opts.LLM, "llm", false, "[experimental] Request LLM-friendly trace format by sending the 'Accept: application/vnd.grafana.llm' header. Falls back to default JSON")

	flags.StringVar(&opts.Query, "q", "", "[experimental] TraceQL spanset filter; only matching spans are returned (V2 only)")
	flags.BoolVar(&opts.KeepHierarchy, "keep-hierarchy", false, "[experimental] Include each matched span's ancestor path to the root (ignored without --q)")
	flags.IntVar(&opts.MatchDepth, "match-depth", 0, "[experimental] Levels of descendants to keep below each matched span: -1 = all, 0 = matched spans only, n = n levels (ignored without --q)")
	flags.IntVar(&opts.AncestorDepth, "ancestor-depth", -1, "[experimental] Levels of ancestors to keep above each matched span: -1 = all (default), 0 = none, n = n levels (ignored without --q or --keep-hierarchy)")

	flags.BoolVar(&opts.SpanPruning, "span-pruning", false, "[experimental] Collapse repeated sibling spans (e.g. a fan-out of identical DB calls) into a single aggregated span to shrink large traces. Overrides the datasource's tenant default; omit to use that default")
	flags.StringVar(&opts.SpanPruningGroupBy, "span-pruning-group-by", "", "[experimental] Comma-separated attribute glob patterns siblings must match to be grouped for pruning, e.g. 'db.*,http.method' (ignored without --span-pruning)")
	flags.IntVar(&opts.SpanPruningMinSpans, "span-pruning-min-spans", 0, "[experimental] Minimum sibling span count required before a group is pruned; Tempo defaults to 5 (ignored without --span-pruning)")
	flags.IntVar(&opts.SpanPruningMaxParentDepth, "span-pruning-max-parent-depth", 0, "[experimental] Ancestor levels above pruned leaves that may also be pruned; Tempo defaults to 1 (ignored without --span-pruning)")

	opts.Share.Setup(flags, "retrieved trace")
	opts.SetupTimeFlags(flags)
}

func (opts *getOpts) Validate(flags *pflag.FlagSet) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if flags.Changed("q") && strings.TrimSpace(opts.Query) == "" {
		return errors.New("--q must not be empty or whitespace-only")
	}
	if opts.MatchDepth < -1 {
		return errors.New("--match-depth must be -1 or greater")
	}
	if opts.AncestorDepth < -1 {
		return errors.New("--ancestor-depth must be -1 or greater")
	}
	return opts.ValidateTimeRange()
}

func GetCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get TRACE_ID",
		Short: "Retrieve a trace by ID",
		Long: `Retrieve a single trace by its trace ID from a Tempo datasource.

TRACE_ID is the hex-encoded trace identifier to retrieve.
Datasource is resolved from -d flag or datasources.tempo in your context.
Use --share-link to print a Grafana Explore URL for the trace, or --open to
open it in your browser after retrieval succeeds. Share links require an
explicit time range via --since or --from/--to.

Experimental: --llm requests the trace in a new LLM-friendly JSON format by
sending the "Accept: application/vnd.grafana.llm" header. Datasources that do
not support this format return the standard response.

Experimental: for large traces, --q narrows the response to spans matching a
TraceQL spanset filter (V2 only). --keep-hierarchy, --match-depth, and
--ancestor-depth shape how much context around each match is kept, and are
ignored without --q. --span-pruning collapses repeated sibling spans (for
example, a fan-out of identical DB calls) into a single aggregated span;
--span-pruning-group-by, --span-pruning-min-spans, and
--span-pruning-max-parent-depth tune that behavior and are ignored without
--span-pruning.`,
		Example: `
  # Get LLM-friendly output for agent analysis
  gcx datasources tempo get abc123def456 --llm -o json

  # Get LLM-friendly output with explicit datasource UID
  gcx datasources tempo get -d tempo-001 abc123def456 --llm -o json

  # Print a Grafana Explore share link for the trace
  gcx datasources tempo get abc123def456 --share-link

  # Get a human-readable trace table
  gcx datasources tempo get abc123def456

  # Get LLM-friendly output within a time range
  gcx datasources tempo get abc123def456 --since 1h --llm -o json

  # Narrow a large trace to error spans and their ancestor path
  gcx datasources tempo get abc123def456 --q '{ status = error }' --keep-hierarchy

  # Collapse repeated sibling spans to shrink a huge trace before analysis
  gcx datasources tempo get abc123def456 --span-pruning --llm -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd.Flags()); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			traceID := args[0]

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := tempo.GetTraceRequest{
				TraceID:            traceID,
				Start:              start,
				End:                end,
				LLMFormat:          opts.LLM,
				Query:              opts.Query,
				KeepHierarchy:      opts.KeepHierarchy,
				MatchDepth:         opts.MatchDepth,
				AncestorDepth:      opts.AncestorDepth,
				SpanPruningGroupBy: opts.SpanPruningGroupBy,
			}
			if cmd.Flags().Changed("span-pruning") {
				req.SpanPruning = &opts.SpanPruning
			}
			if cmd.Flags().Changed("span-pruning-min-spans") {
				req.SpanPruningMinSpans = &opts.SpanPruningMinSpans
			}
			if cmd.Flags().Changed("span-pruning-max-parent-depth") {
				req.SpanPruningMaxParentDepth = &opts.SpanPruningMaxParentDepth
			}

			resp, err := client.GetTrace(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("get trace failed: %w", err)
			}

			exploreURL := ""
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("trace retrieval")
			if opts.IsRange() {
				exploreURL = TraceExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
					DatasourceUID:  datasourceUID,
					DatasourceType: "tempo",
					From:           opts.From,
					To:             opts.To,
					OrgID:          dsquery.OrgID(cfgCtx),
				}, traceID)
			} else if opts.Share.Enabled() {
				unavailableMsg = "trace retrieval succeeded, but Grafana Explore links require --since or --from/--to for Tempo trace retrieval"
			}

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}, opts.Share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx datasources tempo get -d UID <trace-id> --llm -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
