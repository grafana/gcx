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
	// ignored by Tempo unless Filter is also set.
	Filter        string
	KeepHierarchy bool
	MatchDepth    int
	AncestorDepth int

	// Span pruning. PruneGroupBy/PruneMinSpans/PruneMaxParentDepth apply only
	// when Prune enables pruning.
	Prune               bool
	PruneGroupBy        string
	PruneMinSpans       int
	PruneMaxParentDepth int
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

	flags.StringVar(&opts.Filter, "filter", "", "[experimental] TraceQL spanset filter; only matching spans are returned (V2 only)")
	flags.BoolVar(&opts.KeepHierarchy, "keep-hierarchy", false, "[experimental] Include each matched span's ancestor path to the root (ignored without --filter)")
	flags.IntVar(&opts.MatchDepth, "match-depth", 0, "[experimental] Levels of descendants to keep below each matched span: -1 = all, 0 = matched spans only, n = n levels (ignored without --filter)")
	flags.IntVar(&opts.AncestorDepth, "ancestor-depth", -1, "[experimental] Levels of ancestors to keep above each matched span: -1 = all (default), 0 = none, n = n levels (ignored without --filter or --keep-hierarchy)")

	flags.BoolVar(&opts.Prune, "prune", false, "[experimental] Collapse repeated sibling spans (e.g. a fan-out of identical DB calls) into a single aggregated span to shrink large traces. Off unless set")
	flags.StringVar(&opts.PruneGroupBy, "prune-group-by", "", "[experimental] Comma-separated attribute glob patterns siblings must match to be grouped for pruning, e.g. 'db.*,http.method'. Applies only when --prune enables pruning")
	flags.IntVar(&opts.PruneMinSpans, "prune-min-spans", 0, "[experimental] Minimum sibling span count required before a group is pruned; Tempo defaults to 5. Applies only when --prune enables pruning")
	flags.IntVar(&opts.PruneMaxParentDepth, "prune-max-parent-depth", 0, "[experimental] Ancestor levels above pruned leaves that may also be pruned; Tempo defaults to 1. Applies only when --prune enables pruning")

	opts.Share.Setup(flags, "retrieved trace")
	opts.SetupTimeFlags(flags)
}

func (opts *getOpts) Validate(flags *pflag.FlagSet) error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if flags.Changed("filter") && strings.TrimSpace(opts.Filter) == "" {
		return errors.New("--filter must not be empty or whitespace-only")
	}
	if opts.MatchDepth < -1 {
		return errors.New("--match-depth must be -1 or greater")
	}
	if opts.AncestorDepth < -1 {
		return errors.New("--ancestor-depth must be -1 or greater")
	}
	if !flags.Changed("filter") {
		switch {
		case flags.Changed("keep-hierarchy"):
			return errors.New("--keep-hierarchy requires --filter")
		case flags.Changed("match-depth"):
			return errors.New("--match-depth requires --filter")
		case flags.Changed("ancestor-depth"):
			return errors.New("--ancestor-depth requires --filter")
		}
	}
	if flags.Changed("prune-group-by") && strings.TrimSpace(opts.PruneGroupBy) == "" {
		return errors.New("--prune-group-by must not be empty or whitespace-only")
	}
	if opts.PruneMinSpans < 0 {
		return errors.New("--prune-min-spans must be 0 or greater")
	}
	if opts.PruneMaxParentDepth < 0 {
		return errors.New("--prune-max-parent-depth must be 0 or greater")
	}
	return opts.ValidateTimeRange()
}

func (opts *getOpts) buildRequest(flags *pflag.FlagSet, traceID string, start, end time.Time) tempo.GetTraceRequest {
	req := tempo.GetTraceRequest{
		TraceID:   traceID,
		Start:     start,
		End:       end,
		LLMFormat: opts.LLM,
		Query:     opts.Filter,
	}

	if flags.Changed("keep-hierarchy") {
		req.KeepHierarchy = &opts.KeepHierarchy
	}
	if flags.Changed("match-depth") {
		req.MatchDepth = &opts.MatchDepth
	}
	if flags.Changed("ancestor-depth") {
		req.AncestorDepth = &opts.AncestorDepth
	}

	// Unlike the filter fields above, SpanPruning is always sent rather than
	// gated on flags.Changed: false is the correct value whether or not
	// --prune was passed.
	req.SpanPruning = opts.Prune
	if opts.Prune {
		req.SpanPruningGroupBy = opts.PruneGroupBy
	}
	if flags.Changed("prune-min-spans") {
		req.SpanPruningMinSpans = &opts.PruneMinSpans
	}
	if flags.Changed("prune-max-parent-depth") {
		req.SpanPruningMaxParentDepth = &opts.PruneMaxParentDepth
	}
	return req
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

Experimental: for large traces, --filter narrows the response to spans matching
a TraceQL spanset filter (V2 only). --keep-hierarchy, --match-depth, and
--ancestor-depth shape how much context around each match is kept, and are
ignored without --filter.

Experimental: --prune collapses repeated sibling spans (for example, a fan-out
of identical DB calls) into a single aggregated span. Off unless set.
--prune-group-by, --prune-min-spans, and --prune-max-parent-depth tune the
pruning behavior and apply only when --prune enables pruning.

If the trace is too large for -o agents, the response is spilled to a file
with a hint to read it directly or re-run narrower (e.g. with --filter or
--prune).`,
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
  gcx datasources tempo get abc123def456 --filter '{ status = error }' --keep-hierarchy

  # Collapse repeated sibling spans to shrink a huge trace before analysis
  gcx datasources tempo get abc123def456 --prune --llm -o json`,
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

			req := opts.buildRequest(cmd.Flags(), traceID, start, end)

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
		agent.AnnotationLLMHint:   "gcx datasources tempo get -d UID <trace-id> --llm -o json; for a large trace, narrow with --filter '{ status = error }' --keep-hierarchy or shrink fan-outs with --prune",
	}

	opts.setup(cmd.Flags())

	return cmd
}
