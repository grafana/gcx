package loki

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/pflag"
)

// Trace-to-logs discovery for `query --trace-id`.
//
// This mirrors the dynamic trace-to-logs discovery in Grafana Explore
// (grafana/grafana#130515): when the exact label/field mapping between a trace
// and its logs is unknown, a set of candidate LogQL queries is generated and
// tried in order until one returns log lines.

// traceFieldVariants are the trace/span id field-name pairs commonly found in
// structured logs (logfmt or JSON). Order matches Grafana Explore.
var traceFieldVariants = []struct{ trace, span string }{ //nolint:gochecknoglobals // constant-like lookup table
	{"traceID", "spanID"},
	{"trace_id", "span_id"},
	{"traceId", "spanId"},
	{"TraceID", "SpanID"},
	{"TraceId", "SpanId"},
	{"otel_trace_id", "otel_span_id"},
}

// discoveryLabelPreference lists stream labels tried, in order, as a catch-all
// selector when the caller supplies neither a selector nor --trace-service.
var discoveryLabelPreference = []string{ //nolint:gochecknoglobals // constant-like lookup table
	"service_name", "service", "job", "app", "application", "namespace", "container", "pod", "cluster",
}

// maxDiscoverySelectors bounds how many discovered labels are probed, since each
// adds one query per field variant.
const maxDiscoverySelectors = 2

var hexID = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// traceOpts holds the flags that switch `query` into trace-to-logs mode.
type traceOpts struct {
	TraceID string
	SpanID  string
	Service string
}

// enabled reports whether trace mode is requested.
func (o traceOpts) enabled() bool { return o.TraceID != "" }

// validate checks the trace-related flags. Ids are restricted to hex so they can
// be safely embedded in LogQL string literals.
func (o traceOpts) validate(exprProvided bool) error {
	if !o.enabled() {
		switch {
		case o.SpanID != "":
			return errors.New("--span-id requires --trace-id")
		case o.Service != "":
			return errors.New("--trace-service requires --trace-id")
		}
		return nil
	}
	if !hexID.MatchString(o.TraceID) {
		return fmt.Errorf("invalid --trace-id %q: expected a hexadecimal trace id", o.TraceID)
	}
	if o.SpanID != "" && !hexID.MatchString(o.SpanID) {
		return fmt.Errorf("invalid --span-id %q: expected a hexadecimal span id", o.SpanID)
	}
	if o.Service != "" && exprProvided {
		return errors.New("--trace-service cannot be combined with a stream selector expression; use one or the other")
	}
	return nil
}

// traceCandidate is one query to try, with a human-readable description of the
// strategy used in hints.
type traceCandidate struct {
	Expr     string
	Strategy string
}

// serviceSelectors returns the stream selectors Grafana tries for a service
// name: the OTel-style service_name label and a job label matching
// "namespace/service" style values.
func serviceSelectors(service string) []string {
	return []string{
		"{service_name=" + strconv.Quote(service) + "}",
		"{job=~" + strconv.Quote("(.*/)?("+regexp.QuoteMeta(service)+")") + "}",
	}
}

// checkSelector reports whether expr is a bare stream selector (no pipeline).
func checkSelector(expr string) error {
	expr = strings.TrimSpace(expr)
	_, filters, ok := loki.ParseStreamSelector(expr)
	if !ok || len(filters) > 0 {
		return fmt.Errorf("with --trace-id the expression must be a bare stream selector such as '{service_name=\"app\"}', got %q", expr)
	}
	return nil
}

// buildTraceCandidates expands selectors into the ordered list of candidate
// queries. When a span id is set, every span-scoped candidate is followed by the
// same set without the span filter, in case span ids are not logged.
func buildTraceCandidates(opts traceOpts, selectors []string) []traceCandidate {
	withSpan := []string{opts.SpanID}
	if opts.SpanID != "" {
		withSpan = append(withSpan, "")
	}

	out := make([]traceCandidate, 0, (len(selectors)*(len(traceFieldVariants)+1))*len(withSpan))
	for _, span := range withSpan {
		scope := "trace"
		if span != "" {
			scope = "trace+span"
		}
		for _, v := range traceFieldVariants {
			filters := []string{v.trace + `="` + opts.TraceID + `"`}
			if span != "" {
				filters = append(filters, v.span+`="`+span+`"`)
			}
			pipeline := "| logfmt | json | drop __error__ | " + strings.Join(filters, " | ")
			for _, sel := range selectors {
				out = append(out, traceCandidate{
					Expr:     sel + " " + pipeline,
					Strategy: fmt.Sprintf("%s id field %q with selector %s", scope, v.trace, sel),
				})
			}
		}

		lineFilters := []string{`|= "` + opts.TraceID + `"`}
		if span != "" {
			lineFilters = append(lineFilters, `|= "`+span+`"`)
		}
		for _, sel := range selectors {
			out = append(out, traceCandidate{
				Expr:     sel + " " + strings.Join(lineFilters, " "),
				Strategy: fmt.Sprintf("%s line filter with selector %s", scope, sel),
			})
		}
	}
	return out
}

// discoverySelectors builds catch-all selectors from labels present in the
// datasource, preferring well-known service-identifying labels.
func discoverySelectors(labels []string) []string {
	present := make(map[string]bool, len(labels))
	for _, l := range labels {
		present[l] = true
	}

	var chosen []string
	for _, l := range discoveryLabelPreference {
		if present[l] {
			chosen = append(chosen, l)
		}
		if len(chosen) == maxDiscoverySelectors {
			break
		}
	}
	if len(chosen) == 0 {
		sorted := make([]string, 0, len(labels))
		for _, l := range labels {
			if !strings.HasPrefix(l, "__") {
				sorted = append(sorted, l)
			}
		}
		sort.Strings(sorted)
		if len(sorted) > 0 {
			chosen = sorted[:1]
		}
	}

	selectors := make([]string, 0, len(chosen))
	for _, l := range chosen {
		selectors = append(selectors, "{"+l+`=~".+"}`)
	}
	return selectors
}

// traceMatch is the outcome of probing candidate queries.
type traceMatch struct {
	Expr     string
	Strategy string
	Resp     *loki.QueryResponse
	Found    bool
	Tried    int
	Failed   int
}

// queryFunc runs one LogQL expression.
type queryFunc func(ctx context.Context, expr string) (*loki.QueryResponse, error)

// probeTraceCandidates runs candidates in order and returns the first that yields
// log lines. If none do, it returns the last successful (empty) response with
// Found=false; if every candidate errored, it returns the first error.
func probeTraceCandidates(ctx context.Context, candidates []traceCandidate, run queryFunc) (*traceMatch, error) {
	var (
		firstErr error
		last     *traceMatch
	)
	tried, failed := 0, 0
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tried++
		resp, err := run(ctx, c.Expr)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		m := &traceMatch{Expr: c.Expr, Strategy: c.Strategy, Resp: resp, Tried: tried, Failed: failed}
		if countEntries(resp) > 0 {
			m.Found = true
			return m, nil
		}
		last = m
	}
	if last != nil {
		last.Tried, last.Failed = tried, failed
		return last, nil
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, errors.New("no candidate queries were generated")
}

// runTraceQuery resolves the selectors for trace mode (explicit selector,
// --trace-service, or labels discovered from the datasource), probes the candidate
// queries and reports the outcome as a hint on stderr. selector is the optional
// bare stream selector given on the command line. Labels are discovered over the
// same start/end the candidate queries run over.
func runTraceQuery(ctx context.Context, stderr io.Writer, client *loki.Client, datasourceUID string, opts traceOpts, selector string, start, end time.Time, run queryFunc) (*traceMatch, error) {
	var selectors []string
	switch {
	case selector != "":
		selectors = []string{selector}
	case opts.Service != "":
		selectors = serviceSelectors(opts.Service)
	default:
		labels, err := client.LabelsInRange(ctx, datasourceUID, "", start, end)
		if err != nil {
			return nil, fmt.Errorf("failed to discover labels for --trace-id (pass --trace-service or a stream selector instead): %w", err)
		}
		selectors = discoverySelectors(labels.Data)
		if len(selectors) == 0 {
			return nil, errors.New("no stream labels found in the datasource for the queried time range; widen --since/--from or pass --trace-service or a stream selector with --trace-id")
		}
	}

	candidates := buildTraceCandidates(opts, selectors)
	// Probing is sequential and can take a while, so say up front how much work
	// may follow rather than staying silent until the first match.
	cmdio.EmitHint(stderr, fmt.Sprintf("trying up to %d candidate queries for trace %s (stops at the first that returns logs)", len(candidates), opts.TraceID), "")

	match, err := probeTraceCandidates(ctx, candidates, run)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	failures := ""
	if match.Failed > 0 {
		failures = fmt.Sprintf(" (%d candidate quer%s failed)", match.Failed, plural(match.Failed))
	}
	if match.Found {
		cmdio.EmitHint(stderr, fmt.Sprintf("matched logs for trace %s using %s (query: %s)%s", opts.TraceID, match.Strategy, match.Expr, failures), "")
	} else {
		cmdio.EmitHint(stderr, fmt.Sprintf("no logs found for trace %s after %d candidate queries%s; results may be incomplete if any failed; widen --since/--from, or pass --trace-service or a stream selector", opts.TraceID, match.Tried, failures), "")
	}
	return match, nil
}

// defaultTraceSince is the look-back window for --trace-id when no time flags are set.
const defaultTraceSince = "1h"

// prepareQuery validates the shared and trace flags, applies the trace time-range
// default, and resolves the expression. Without --trace-id the expression is
// required; with --trace-id it is an optional bare stream selector and the result
// is empty when none was given.
func prepareQuery(shared *dsquery.SharedOpts, trace *traceOpts, flags *pflag.FlagSet, args []string) (string, error) {
	// Reject an explicitly empty value (unset shell variable) instead of
	// silently dropping trace mode.
	for _, name := range []string{"trace-id", "span-id", "trace-service"} {
		if flags.Changed(name) && flags.Lookup(name).Value.String() == "" {
			return "", fmt.Errorf("invalid --%s: value is empty (unset shell variable?)", name)
		}
	}

	exprProvided := shared.Expr != "" || len(args) > 0
	if err := trace.validate(exprProvided); err != nil {
		return "", err
	}

	if trace.enabled() && shared.From == "" && shared.To == "" && shared.Since == "" {
		// Instant queries only look back a minute, which is useless for traces.
		shared.Since = defaultTraceSince
	}

	if err := shared.Validate(); err != nil {
		return "", err
	}

	if trace.enabled() && !exprProvided {
		return "", nil
	}

	expr, err := shared.ResolveExpr(args, 0)
	if err != nil {
		return "", err
	}
	if trace.enabled() {
		if err := checkSelector(expr); err != nil {
			return "", err
		}
	}
	return expr, nil
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
