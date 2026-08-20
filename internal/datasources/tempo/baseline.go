package tempo

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
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

// defaultBaselineWindow pads the candidate search window before and after the
// seed trace's own span range so healthy candidates from just before or after
// the seed are captured. Overridable with --window.
const defaultBaselineWindow = "30m"

// topDownstreamServices is how many of the seed's busiest downstream services
// are pinned into the retrieval query as a topology fingerprint. The design
// recommends ~3-4: enough to keep candidates on the same execution path without
// over-constraining and losing recall.
const topDownstreamServices = 3

type baselineOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	Limit      int
	Window     string
}

func (opts *baselineOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.IntVar(&opts.Limit, "limit", 20, "Maximum number of candidates to return")
	flags.StringVar(&opts.Window, "window", defaultBaselineWindow, "Search window padding applied before and after the seed trace's time range, so candidates from before or after the seed are eligible (e.g., 30m, 6h, 7d). Ignored when --from/--to are set")
	// --from/--to give an absolute window override anchored on explicit times
	// rather than the seed. --since is intentionally omitted: it anchors on now,
	// which is wrong for a seed trace that may be hours or days old.
	flags.StringVar(&opts.From, "from", "", "Absolute start time override (RFC3339, Unix timestamp, or relative like 'now-1h'); requires --to")
	flags.StringVar(&opts.To, "to", "", "Absolute end time override (RFC3339, Unix timestamp, or relative like 'now'); requires --from")
}

func (opts *baselineOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	pad, err := dsquery.ParseDuration(opts.Window)
	if err != nil {
		return fmt.Errorf("invalid --window duration: %w", err)
	}
	if pad < 0 {
		return errors.New("--window must not be negative")
	}
	return opts.ValidateTimeRange()
}

// BaselineCmd returns the `baseline` subcommand: given a seed trace, it finds
// healthy same-operation candidate traces to diff against.
func BaselineCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &baselineOpts{}

	cmd := &cobra.Command{
		Use:   "baseline TRACE_ID",
		Short: "[experimental] Find healthy baseline candidates for a trace",
		Long: `[experimental] Find healthy, same-operation candidate traces to compare against a seed trace.

TRACE_ID is the seed trace (typically a faulty one). The command fetches it,
reads its root service/operation and its busiest downstream services, then
searches for traces with the same root identity whose operation succeeded
(status != error), pinned to the seed's top downstream services so candidates
stay on the same execution path.

Downstream errors are deliberately NOT filtered out: surfacing them is the job
of 'gcx traces diff <seed> <candidate>', which is the real similarity and
root-cause step. This command only retrieves candidate trace IDs (in the order
search returns them); it does not rank them.

By default candidates are searched within the seed trace's own time range,
padded by --window (30m) on each side, so candidates from before or after the
seed are eligible. Widen with --window, or set an absolute window with
--from/--to.

This is a heuristic retrieval built on TraceQL search; its query and output may
change.`,
		Example: `
  # Find baseline candidates for a trace, then diff against one
  gcx traces baseline abc123
  gcx traces diff abc123 <candidate>

  # Widen the window to 6h before and after the seed, output JSON
  gcx traces baseline abc123 --window 6h -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			seedID := args[0]

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Fetch the seed trace (no time range → full lookup) and read its
			// root identity and span time range.
			seed, err := client.GetTrace(ctx, datasourceUID, tempo.GetTraceRequest{TraceID: seedID})
			if err != nil {
				return fmt.Errorf("failed to fetch seed trace: %w", err)
			}

			// Single pass over the seed trace: root identity, span time range,
			// and per-service span counts.
			profile := parseSeedTrace(seed.Trace)
			if profile.RootService == "" || profile.RootOperation == "" {
				return fmt.Errorf("could not determine root service/operation from trace %q", seedID)
			}

			start, end, err := opts.resolveWindow(profile, time.Now())
			if err != nil {
				return err
			}

			// The seed can match its own retrieval query, so fetch one extra and
			// drop it, keeping up to --limit candidates.
			fetchLimit := opts.Limit
			if fetchLimit > 0 {
				fetchLimit++
			}
			downstream := downstreamServices(profile.ServiceSpans, profile.RootService, topDownstreamServices)
			req := tempo.SearchRequest{
				Query: buildBaselineQuery(profile.RootService, profile.RootOperation, downstream),
				Start: start,
				End:   end,
				Limit: fetchLimit,
			}

			resp, err := client.Search(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("baseline candidate search failed: %w", err)
			}

			resp = excludeTrace(resp, seedID)
			resp = limitTraces(resp, opts.Limit)

			result := buildBaselineResult(seedID, profile.ServiceSpans, resp, req.Query)

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx traces baseline -d UID <trace-id> -o json",
		agent.AnnotationStability: agent.StabilityExperimental,
	}

	opts.setup(cmd.Flags())

	return cmd
}

// resolveWindow returns the candidate search window: an explicit --from/--to
// range when provided, otherwise the seed trace's own span range padded by
// --window on each side (before and after the seed).
func (opts *baselineOpts) resolveWindow(profile seedProfile, now time.Time) (time.Time, time.Time, error) {
	if opts.IsRange() {
		return opts.ParseTimeRange(now)
	}
	if !profile.HasTimeRange {
		return time.Time{}, time.Time{}, errors.New("seed trace has no span timestamps; specify --from/--to")
	}
	pad, err := dsquery.ParseDuration(opts.Window)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --window duration: %w", err)
	}
	start := unixNanoToTime(profile.MinStartNanos).Add(-pad)
	end := unixNanoToTime(profile.MaxEndNanos).Add(pad)
	return start, end, nil
}

// buildBaselineQuery constructs the TraceQL retrieval query per the design:
// same root identity, the operation span constrained to a successful
// (non-error) status, and a topology fingerprint pinning the seed's top
// downstream services so candidates stay on the same execution path. Whole-trace
// health is not filtered here — 'gcx traces diff' surfaces downstream errors.
func buildBaselineQuery(service, operation string, downstream []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "{ trace:rootService = %s && trace:rootName = %s } && { name = %s && span:status != error && nestedSetParent = -1 }",
		strconv.Quote(service), strconv.Quote(operation), strconv.Quote(operation))
	for _, svc := range downstream {
		fmt.Fprintf(&b, " && { resource.service.name = %s }", strconv.Quote(svc))
	}
	return b.String()
}

// downstreamServices returns up to n of the seed's busiest non-root services,
// ordered by span count desc with a name tie-break for a deterministic query.
// These form the topology fingerprint in buildBaselineQuery.
func downstreamServices(serviceSpans map[string]int, rootService string, n int) []string {
	names := make([]string, 0, len(serviceSpans))
	for name := range serviceSpans {
		if name == rootService || name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		ci, cj := serviceSpans[names[i]], serviceSpans[names[j]]
		if ci != cj {
			return ci > cj
		}
		return names[i] < names[j]
	})
	if len(names) > n {
		names = names[:n]
	}
	return names
}

// buildBaselineResult enriches the candidates with the structural context
// (per-candidate span/service counts) and attaches the seed profile. Candidate
// order is preserved as returned by search.
func buildBaselineResult(seedID string, seedCounts map[string]int, resp *tempo.SearchResponse, query string) *tempo.BaselineResult {
	seedSpans := 0
	for _, n := range seedCounts {
		seedSpans += n
	}
	result := &tempo.BaselineResult{
		SeedTraceID:      seedID,
		SeedSpanCount:    seedSpans,
		SeedServiceCount: len(seedCounts),
		Query:            query,
	}
	if resp == nil {
		return result
	}
	for _, t := range resp.Traces {
		spans := 0
		for _, s := range t.ServiceStats {
			spans += s.SpanCount
		}
		result.Candidates = append(result.Candidates, tempo.BaselineCandidate{
			TraceID:           t.TraceID,
			RootServiceName:   t.RootServiceName,
			RootTraceName:     t.RootTraceName,
			StartTimeUnixNano: t.StartTimeUnixNano,
			DurationMs:        t.DurationMs,
			SpanCount:         spans,
			ServiceCount:      len(t.ServiceStats),
		})
	}
	return result
}

// limitTraces trims resp to at most n traces, preserving order. n <= 0 means no
// cap. It returns a new response so the caller's slice is not aliased.
func limitTraces(resp *tempo.SearchResponse, n int) *tempo.SearchResponse {
	if resp == nil || n <= 0 || len(resp.Traces) <= n {
		return resp
	}
	return &tempo.SearchResponse{Traces: resp.Traces[:n]}
}

// excludeTrace returns a copy of resp without the seed trace.
func excludeTrace(resp *tempo.SearchResponse, seedID string) *tempo.SearchResponse {
	if resp == nil {
		return resp
	}
	filtered := make([]tempo.SearchTrace, 0, len(resp.Traces))
	for _, t := range resp.Traces {
		if t.TraceID == seedID {
			continue
		}
		filtered = append(filtered, t)
	}
	return &tempo.SearchResponse{Traces: filtered}
}

// ─── seed trace parsing (OTLP-shaped resourceSpans) ─────────────────────────

// seedProfile summarizes a seed trace: its root identity, span time range, and
// per-service span counts. It is produced by a single traversal of the trace.
type seedProfile struct {
	RootService   string
	RootOperation string
	MinStartNanos uint64
	MaxEndNanos   uint64
	HasTimeRange  bool
	ServiceSpans  map[string]int
}

// parseSeedTrace walks every span in the OTLP-shaped trace once and derives what
// the command needs: per-service span counts, the overall span time range, and
// the root identity (earliest parentless span and its resource service.name).
func parseSeedTrace(trace map[string]any) seedProfile {
	profile := seedProfile{ServiceSpans: make(map[string]int)}
	var rootStart uint64
	rootFound := false

	forEachSpan(trace, func(service string, span map[string]any) {
		if service != "" {
			profile.ServiceSpans[service]++
		}

		start := parseUnixNano(span["startTimeUnixNano"])
		profile.foldTimeRange(start, parseUnixNano(span["endTimeUnixNano"]))

		// Root = the earliest-starting parentless span.
		if parent, _ := span["parentSpanId"].(string); strings.TrimSpace(parent) == "" {
			if !rootFound || (start != 0 && (rootStart == 0 || start < rootStart)) {
				rootFound, rootStart = true, start
				profile.RootService = service
				profile.RootOperation, _ = span["name"].(string)
			}
		}
	})
	return profile
}

// foldTimeRange widens the profile's [MinStartNanos, MaxEndNanos] by a span's
// start/end, ignoring zero (absent) timestamps so a missing start can't drag the
// window back to the epoch. start <= end always holds, so the min/max over all
// real timestamps is exactly [earliest start, latest end].
func (p *seedProfile) foldTimeRange(start, end uint64) {
	for _, ts := range [2]uint64{start, end} {
		if ts == 0 {
			continue
		}
		if !p.HasTimeRange || ts < p.MinStartNanos {
			p.MinStartNanos = ts
		}
		if !p.HasTimeRange || ts > p.MaxEndNanos {
			p.MaxEndNanos = ts
		}
		p.HasTimeRange = true
	}
}

// forEachSpan calls fn for every span in the OTLP-shaped trace, passing the
// resource's service.name ("" when absent). It isolates the resourceSpans ->
// scopeSpans -> spans descent so callers stay flat.
func forEachSpan(trace map[string]any, fn func(service string, span map[string]any)) {
	for _, rs := range asAnySlice(trace["resourceSpans"]) {
		rsm, _ := rs.(map[string]any)
		if rsm == nil {
			continue
		}
		service := resourceServiceName(rsm)
		for _, ss := range asAnySlice(rsm["scopeSpans"]) {
			ssm, _ := ss.(map[string]any)
			for _, sp := range asAnySlice(ssm["spans"]) {
				if span, ok := sp.(map[string]any); ok {
					fn(service, span)
				}
			}
		}
	}
}

func resourceServiceName(rs map[string]any) string {
	res, _ := rs["resource"].(map[string]any)
	for _, a := range asAnySlice(res["attributes"]) {
		am, _ := a.(map[string]any)
		if am == nil {
			continue
		}
		if key, _ := am["key"].(string); key != "service.name" {
			continue
		}
		val, _ := am["value"].(map[string]any)
		s, _ := val["stringValue"].(string)
		return s
	}
	return ""
}

func asAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// unixNanoToTime converts unix nanoseconds to time.Time, bounding the value to
// avoid a uint64->int64 overflow (timestamps fit in int64 until year 2262).
func unixNanoToTime(n uint64) time.Time {
	if n > math.MaxInt64 {
		n = math.MaxInt64
	}
	return time.Unix(0, int64(n))
}

func parseUnixNano(v any) uint64 {
	switch t := v.(type) {
	case string:
		n, _ := strconv.ParseUint(t, 10, 64)
		return n
	case float64:
		return uint64(t)
	}
	return 0
}
