package tempo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Accepted --prune values. Bare --prune resolves to pruneValueTrue via the
// flag's NoOptDefVal.
const (
	pruneValueTrue  = "true"
	pruneValueFalse = "false"
	pruneValueAuto  = "auto"
)

// pruneMode is the resolved form of --prune. pruneUnset means the flag was not
// passed, which leaves the decision to the datasource's tenant default.
type pruneMode int

const (
	pruneUnset pruneMode = iota
	pruneOff
	pruneOn
	pruneAuto
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

	// Span pruning. PruneGroupBy/PruneMinSpans/PruneMaxParentDepth apply
	// whenever pruning is enabled — explicitly via --prune, or by the
	// datasource's tenant default.
	Prune               string
	PruneGroupBy        string
	PruneMinSpans       int
	PruneMaxParentDepth int

	pruneMode pruneMode
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

	flags.StringVar(&opts.Prune, "prune", "", "[experimental] Collapse repeated sibling spans (e.g. a fan-out of identical DB calls) into a single aggregated span to shrink large traces: 'true', 'false', or 'auto' to prune only when the unpruned trace exceeds the agent output budget. Bare --prune means true. Overrides the datasource's tenant default; omit to use that default")
	flags.Lookup("prune").NoOptDefVal = pruneValueTrue
	flags.StringVar(&opts.PruneGroupBy, "prune-group-by", "", "[experimental] Comma-separated attribute glob patterns siblings must match to be grouped for pruning, e.g. 'db.*,http.method'. Applies whenever pruning is enabled, including by the datasource's tenant default")
	flags.IntVar(&opts.PruneMinSpans, "prune-min-spans", 0, "[experimental] Minimum sibling span count required before a group is pruned; Tempo defaults to 5. Applies whenever pruning is enabled, including by the datasource's tenant default")
	flags.IntVar(&opts.PruneMaxParentDepth, "prune-max-parent-depth", 0, "[experimental] Ancestor levels above pruned leaves that may also be pruned; Tempo defaults to 1. Applies whenever pruning is enabled, including by the datasource's tenant default")

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
	if err := opts.resolvePruneMode(flags); err != nil {
		return err
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

func (opts *getOpts) resolvePruneMode(flags *pflag.FlagSet) error {
	if !flags.Changed("prune") {
		opts.pruneMode = pruneUnset
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(opts.Prune)) {
	case pruneValueTrue:
		opts.pruneMode = pruneOn
	case pruneValueFalse:
		opts.pruneMode = pruneOff
	case pruneValueAuto:
		opts.pruneMode = pruneAuto
	default:
		return fmt.Errorf("--prune must be %q, %q, or %q", pruneValueTrue, pruneValueFalse, pruneValueAuto)
	}
	return nil
}

func (opts *getOpts) buildRequest(flags *pflag.FlagSet, traceID string, start, end time.Time) tempo.GetTraceRequest {
	req := tempo.GetTraceRequest{
		TraceID:            traceID,
		Start:              start,
		End:                end,
		LLMFormat:          opts.LLM,
		Query:              opts.Filter,
		KeepHierarchy:      opts.KeepHierarchy,
		MatchDepth:         opts.MatchDepth,
		AncestorDepth:      opts.AncestorDepth,
		SpanPruningGroupBy: opts.PruneGroupBy,
	}

	// pruneAuto deliberately leaves SpanPruning unset on the first fetch so the
	// tenant default still applies; it only forces pruning on the retry.
	switch opts.pruneMode {
	case pruneOn:
		req.SpanPruning = new(true)
	case pruneOff:
		req.SpanPruning = new(false)
	case pruneUnset, pruneAuto:
	}

	if flags.Changed("prune-min-spans") {
		req.SpanPruningMinSpans = &opts.PruneMinSpans
	}
	if flags.Changed("prune-max-parent-depth") {
		req.SpanPruningMaxParentDepth = &opts.PruneMaxParentDepth
	}
	return req
}

// fetchTrace retrieves the trace, and under --prune=auto re-requests it with
// span pruning forced on when the unpruned response does not fit the agent
// output budget. Auto is best-effort: a failed or unhelpful retry falls back to
// the response already in hand rather than failing the command.
func fetchTrace(ctx context.Context, client *tempo.Client, errOut io.Writer, datasourceUID string, req tempo.GetTraceRequest, auto bool) (*tempo.GetTraceResponse, error) {
	resp, err := client.GetTrace(ctx, datasourceUID, req)
	if err != nil {
		return nil, fmt.Errorf("get trace failed: %w", err)
	}
	if !auto {
		return resp, nil
	}

	budget := cmdio.SpillThreshold()
	size, err := encodedSize(resp)
	if err != nil {
		return nil, fmt.Errorf("measure trace response: %w", err)
	}
	if size <= budget {
		return resp, nil
	}

	pruned := req
	pruned.SpanPruning = new(true)
	prunedResp, err := client.GetTrace(ctx, datasourceUID, pruned)
	if err != nil {
		cmdio.EmitHint(errOut, fmt.Sprintf(
			"--prune=auto: trace is %d bytes (budget %d) but the pruned retry failed (%v); returning the unpruned trace",
			size, budget, err), "")
		return resp, nil
	}

	prunedSize, err := encodedSize(prunedResp)
	if err != nil {
		return nil, fmt.Errorf("measure pruned trace response: %w", err)
	}
	if prunedSize >= size {
		return resp, nil
	}

	cmdio.EmitHint(errOut, fmt.Sprintf(
		"--prune=auto: unpruned trace was %d bytes (budget %d); returning a span-pruned trace of %d bytes",
		size, budget, prunedSize), "")
	return prunedResp, nil
}

// encodedSize reports the JSON-encoded size of v, matching how the agents
// codec measures a payload against the spill threshold.
func encodedSize(v any) (int, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return 0, err
	}
	return buf.Len(), nil
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
of identical DB calls) into a single aggregated span. It takes 'true', 'false',
or 'auto'; bare --prune means true, and omitting it uses the datasource's tenant
default. With --prune=auto the trace is fetched unpruned first and re-requested
with pruning only if it exceeds the agent output budget (100 KiB, overridable
via GCX_AGENT_SPILL_BYTES), which pairs with -o agents for large traces.
--prune-group-by, --prune-min-spans, and --prune-max-parent-depth tune the
pruning behavior and apply whenever pruning is enabled, including by the tenant
default.`,
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
  gcx datasources tempo get abc123def456 --prune --llm -o json

  # Prune only if the trace does not fit the agent output budget
  gcx datasources tempo get abc123def456 --prune=auto --llm -o agents`,
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

			resp, err := fetchTrace(ctx, client, cmd.ErrOrStderr(), datasourceUID, req, opts.pruneMode == pruneAuto)
			if err != nil {
				return err
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
		agent.AnnotationLLMHint:   "gcx datasources tempo get -d UID <trace-id> --llm -o json; for a large trace, narrow with --filter '{ status = error }' --keep-hierarchy or shrink fan-outs with --prune (or --prune=auto to prune only when oversized)",
	}

	opts.setup(cmd.Flags())

	return cmd
}
