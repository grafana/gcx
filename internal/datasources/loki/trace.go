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

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/pflag"
)

// Trace-to-logs discovery for `query --trace`.
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
// selector when the caller supplies neither a selector nor --service.
var discoveryLabelPreference = []string{ //nolint:gochecknoglobals // constant-like lookup table
	"service_name", "service", "job", "app", "application", "namespace", "container", "pod", "cluster",
}

// maxDiscoverySelectors bounds how many discovered labels are probed, since each
// adds one query per field variant.
const maxDiscoverySelectors = 2

var hexID = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// TraceOpts holds the flags that switch `query` into trace-to-logs mode.
type TraceOpts struct {
	TraceID string
	SpanID  string
	Service string
}

// Enabled reports whether trace mode is requested.
func (o TraceOpts) Enabled() bool { return o.TraceID != "" }

// Validate checks the trace-related flags. Ids are restricted to hex so they can
// be safely embedded in LogQL string literals.
func (o TraceOpts) Validate(exprProvided bool) error {
	if !o.Enabled() {
		switch {
		case o.SpanID != "":
			return errors.New("--span requires --trace")
		case o.Service != "":
			return errors.New("--service requires --trace")
		}
		return nil
	}
	if !hexID.MatchString(o.TraceID) {
		return fmt.Errorf("invalid --trace %q: expected a hexadecimal trace id", o.TraceID)
	}
	if o.SpanID != "" && !hexID.MatchString(o.SpanID) {
		return fmt.Errorf("invalid --span %q: expected a hexadecimal span id", o.SpanID)
	}
	if o.Service != "" && exprProvided {
		return errors.New("--service cannot be combined with a stream selector expression; use one or the other")
	}
	return nil
}

// traceCandidate is one query to try, with a human-readable description of the
// strategy used in hints.
type traceCandidate struct {
	Expr     string
	Strategy string
}

// ServiceSelectors returns the stream selectors Grafana tries for a service
// name: the OTel-style service_name label and a job label matching
// "namespace/service" style values.
func ServiceSelectors(service string) []string {
	return []string{
		"{service_name=" + strconv.Quote(service) + "}",
		"{job=~" + strconv.Quote("(.*/)?("+regexp.QuoteMeta(service)+")") + "}",
	}
}

// checkSelector reports whether expr is a bare stream selector (no pipeline).
func checkSelector(expr string) error {
	expr = strings.TrimSpace(expr)
	_, filters, ok := loki.ParseStreamSelector(expr)
	if !ok || len(filters) > 0 || !strings.HasSuffix(expr, "}") {
		return fmt.Errorf("with --trace the expression must be a bare stream selector such as '{service_name=\"app\"}', got %q", expr)
	}
	return nil
}

// buildTraceCandidates expands selectors into the ordered list of candidate
// queries. When a span id is set, every span-scoped candidate is followed by the
// same set without the span filter, in case span ids are not logged.
func buildTraceCandidates(opts TraceOpts, selectors []string) []traceCandidate {
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
			if l != "__stream_shard__" && !strings.HasPrefix(l, "__") {
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
// --service, or labels discovered from the datasource), probes the candidate
// queries and reports the outcome as a hint on stderr. selector is the optional
// bare stream selector given on the command line.
func runTraceQuery(ctx context.Context, stderr io.Writer, client *loki.Client, datasourceUID string, opts TraceOpts, selector string, run queryFunc) (*traceMatch, error) {
	var selectors []string
	switch {
	case selector != "":
		selectors = []string{selector}
	case opts.Service != "":
		selectors = ServiceSelectors(opts.Service)
	default:
		labels, err := client.Labels(ctx, datasourceUID, "")
		if err != nil {
			return nil, fmt.Errorf("failed to discover labels for --trace (pass --service or a stream selector instead): %w", err)
		}
		selectors = discoverySelectors(labels.Data)
		if len(selectors) == 0 {
			return nil, errors.New("no stream labels found in the datasource; pass --service or a stream selector with --trace")
		}
	}

	match, err := probeTraceCandidates(ctx, buildTraceCandidates(opts, selectors), run)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if match.Found {
		cmdio.EmitHint(stderr, fmt.Sprintf("matched logs for trace %s using %s (query: %s)", opts.TraceID, match.Strategy, match.Expr), "")
	} else {
		failures := ""
		if match.Failed > 0 {
			failures = fmt.Sprintf(" (%d failed, so the result may be incomplete)", match.Failed)
		}
		cmdio.EmitHint(stderr, fmt.Sprintf("no logs found for trace %s after %d candidate queries%s; widen --since/--from, or pass --service or a stream selector", opts.TraceID, match.Tried, failures), "")
	}
	return match, nil
}

// defaultTraceSince is the look-back window for --trace when no time flags are set.
const defaultTraceSince = "1h"

// prepareQuery validates the shared and trace flags, applies the trace time-range
// default, and resolves the expression. Without --trace the expression is
// required; with --trace it is an optional bare stream selector and the result
// is empty when none was given.
func prepareQuery(shared *dsquery.SharedOpts, trace *TraceOpts, flags *pflag.FlagSet, args []string) (string, error) {
	// Reject an explicitly empty value (unset shell variable) instead of
	// silently dropping trace mode.
	for _, name := range []string{"trace", "span", "service"} {
		if flags.Changed(name) && flags.Lookup(name).Value.String() == "" {
			return "", fmt.Errorf("invalid --%s: value is empty (unset shell variable?)", name)
		}
	}

	exprProvided := shared.Expr != "" || len(args) > 0
	if err := trace.Validate(exprProvided); err != nil {
		return "", err
	}

	if trace.Enabled() && shared.From == "" && shared.To == "" && shared.Since == "" {
		// Instant queries only look back a minute, which is useless for traces.
		shared.Since = defaultTraceSince
	}

	if err := shared.Validate(); err != nil {
		return "", err
	}

	if trace.Enabled() && !exprProvided {
		return "", nil
	}

	expr, err := shared.ResolveExpr(args, 0)
	if err != nil {
		return "", err
	}
	if trace.Enabled() {
		if err := checkSelector(expr); err != nil {
			return "", err
		}
	}
	return expr, nil
}
