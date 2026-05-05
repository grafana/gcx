package tempo

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// asyncTailThreshold is how far past the latest attached-subtree end any
// span must end for the header to be flagged with "(async tail detected)".
const asyncTailThreshold = time.Second

// invalidPercentCell is rendered in the % column when the share cannot be
// computed (zero trace duration or non-positive span duration). Rows showing
// this must not participate in color thresholds.
const invalidPercentCell = "—"

// FormatSearchTable formats a search response as a table.
func FormatSearchTable(w io.Writer, resp *SearchResponse) error {
	tbl := style.NewTable("TRACE_ID", "SERVICE", "NAME", "DURATION", "START")

	for _, tr := range resp.Traces {
		tbl.Row(
			tr.TraceID,
			tr.RootServiceName,
			tr.RootTraceName,
			formatDuration(tr.DurationMs),
			formatStartTime(tr.StartTimeUnixNano),
		)
	}

	return tbl.Render(w)
}

// FormatTagsTable formats a tags response as a table.
func FormatTagsTable(w io.Writer, resp *TagsResponse) error {
	t := style.NewTable("SCOPE", "TAG")

	for _, scope := range resp.Scopes {
		for _, tag := range scope.Tags {
			t.Row(scope.Name, tag)
		}
	}

	return t.Render(w)
}

// FormatTagValuesTable formats a tag-values response as a table.
func FormatTagValuesTable(w io.Writer, resp *TagValuesResponse) error {
	t := style.NewTable("TYPE", "VALUE")

	for _, tv := range resp.TagValues {
		t.Row(tv.Type, fmt.Sprint(tv.Value))
	}

	return t.Render(w)
}

// FormatMetricsTable formats a metrics response as a table.
func FormatMetricsTable(w io.Writer, resp *MetricsResponse) error {
	if resp != nil && resp.Instant {
		return formatInstantMetricsTable(w, resp)
	}
	return formatRangeMetricsTable(w, resp)
}

func formatRangeMetricsTable(w io.Writer, resp *MetricsResponse) error {
	t := style.NewTable("LABELS", "TIMESTAMP", "VALUE")

	for _, series := range resp.Series {
		labels := FormatMetricsLabels(series.Labels)

		if len(series.Samples) > 0 {
			for _, sample := range series.Samples {
				t.Row(
					labels,
					sample.TimestampMs,
					strconv.FormatFloat(sample.Value, 'f', -1, 64),
				)
			}
			continue
		}

		if series.Value != nil {
			t.Row(
				labels,
				series.TimestampMs,
				strconv.FormatFloat(*series.Value, 'f', -1, 64),
			)
		}
	}

	return t.Render(w)
}

func formatInstantMetricsTable(w io.Writer, resp *MetricsResponse) error {
	t := style.NewTable("LABELS", "VALUE")

	for _, series := range resp.Series {
		if series.Value == nil {
			continue
		}

		t.Row(
			FormatMetricsLabels(series.Labels),
			strconv.FormatFloat(*series.Value, 'f', -1, 64),
		)
	}

	return t.Render(w)
}

// FormatMetricsLabels formats metrics labels as a {key="val", ...} string.
func FormatMetricsLabels(labels []MetricsLabel) string {
	if len(labels) == 0 {
		return "{}"
	}

	// Sort by key for deterministic output.
	sorted := make([]MetricsLabel, len(labels))
	copy(sorted, labels)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key < sorted[j].Key
	})

	parts := make([]string, 0, len(sorted))
	for _, l := range sorted {
		parts = append(parts, fmt.Sprintf("%s=%q", l.Key, extractLabelValue(l.Value)))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// extractLabelValue extracts a string representation from a MetricsLabel value map.
func extractLabelValue(v map[string]any) string {
	// Tempo encodes typed values as {"stringValue": "..."}, {"intValue": "..."}, etc.
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue"} {
		if val, ok := v[key]; ok {
			return fmt.Sprint(val)
		}
	}
	// Fallback: return first value found.
	for _, val := range v {
		return fmt.Sprint(val)
	}
	return ""
}

func formatDuration(ms int) string {
	switch {
	case ms < 1:
		return "< 1ms"
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60000:
		return fmt.Sprintf("%.2fs", float64(ms)/1000)
	default:
		m := ms / 60000
		s := (ms % 60000) / 1000
		return fmt.Sprintf("%dm%ds", m, s)
	}
}

func formatStartTime(startTimeUnixNano string) string {
	nanos, err := strconv.ParseInt(startTimeUnixNano, 10, 64)
	if err != nil {
		return startTimeUnixNano
	}
	return time.Unix(0, nanos).Format(time.RFC3339)
}

type traceSpan struct {
	traceID    string
	spanID     string
	parentID   string
	name       string
	service    string
	kind       string
	statusCode string
	start      int64
	end        int64
}

// FormatTraceTable formats a get-trace response as a tree table.
func FormatTraceTable(w io.Writer, resp *GetTraceResponse) error {
	return formatTrace(w, resp, false)
}

// FormatTraceWide formats a get-trace response as a wide tree table (adds KIND, START).
func FormatTraceWide(w io.Writer, resp *GetTraceResponse) error {
	return formatTrace(w, resp, true)
}

// traceTotals captures the aggregates needed for the header line and the
// per-row % cell: trace start/end (and therefore total duration), the
// chosen trace ID, and the unique service count.
type traceTotals struct {
	traceID  string
	start    int64
	end      int64
	dur      int64
	services int
}

func aggregateTotals(spans []traceSpan) traceTotals {
	t := traceTotals{
		start: spans[0].start,
		end:   spans[0].end,
	}
	services := make(map[string]struct{})
	for _, s := range spans {
		if s.start < t.start {
			t.start = s.start
		}
		if s.end > t.end {
			t.end = s.end
		}
		if t.traceID == "" && s.traceID != "" {
			t.traceID = s.traceID
		}
		services[s.service] = struct{}{}
	}
	t.dur = t.end - t.start
	t.services = len(services)
	return t
}

// traceTree groups spans by parent and separates roots from orphans.
type traceTree struct {
	children map[string][]*traceSpan
	attached []*traceSpan
	orphans  []*traceSpan
}

func buildTraceTree(spans []traceSpan) traceTree {
	byID := make(map[string]*traceSpan, len(spans))
	for i := range spans {
		byID[spans[i].spanID] = &spans[i]
	}
	tree := traceTree{children: make(map[string][]*traceSpan)}
	for i := range spans {
		s := &spans[i]
		switch {
		case s.parentID == "":
			tree.attached = append(tree.attached, s)
		case byID[s.parentID] != nil:
			tree.children[s.parentID] = append(tree.children[s.parentID], s)
		default:
			tree.orphans = append(tree.orphans, s)
		}
	}
	for k := range tree.children {
		sort.Slice(tree.children[k], func(i, j int) bool { return tree.children[k][i].start < tree.children[k][j].start })
	}
	sort.Slice(tree.attached, func(i, j int) bool { return tree.attached[i].start < tree.attached[j].start })
	sort.Slice(tree.orphans, func(i, j int) bool { return tree.orphans[i].start < tree.orphans[j].start })
	return tree
}

// detectAsyncTail returns true when the latest span end exceeds the latest
// end of attached subtrees by more than asyncTailThreshold.
func detectAsyncTail(tree traceTree, totals traceTotals) bool {
	if len(tree.attached) == 0 {
		return false
	}
	var attachedMaxEnd int64
	var collect func(*traceSpan)
	collect = func(s *traceSpan) {
		if s.end > attachedMaxEnd {
			attachedMaxEnd = s.end
		}
		for _, c := range tree.children[s.spanID] {
			collect(c)
		}
	}
	for _, r := range tree.attached {
		collect(r)
	}
	return totals.end-attachedMaxEnd > int64(asyncTailThreshold)
}

func writeTraceHeader(w io.Writer, totals traceTotals, spanCount int, asyncTail bool) {
	suffix := ""
	if asyncTail {
		suffix = " (async tail detected)"
	}
	fmt.Fprintf(w, "Trace %s  duration: %s  spans: %d  services: %d%s\n\n",
		totals.traceID, formatDurationNanos(totals.dur), spanCount, totals.services, suffix)
}

// rowCells renders a single span row's cells, with all coloring applied.
func rowCells(s *traceSpan, label string, totals traceTotals, wide bool) []string {
	isErr := s.statusCode == "STATUS_CODE_ERROR"
	dur := s.end - s.start
	hasPct := totals.dur > 0 && dur > 0

	pctStr := invalidPercentCell
	var pct float64
	if hasPct {
		pct = float64(dur) / float64(totals.dur) * 100
		pctStr = fmt.Sprintf("%5.1f%%", pct)
	}
	// Dim only applies when pct is computable and below threshold.
	dim := hasPct && pct < 1.0

	cells := []string{label, dashIfEmpty(s.service), s.spanID, formatDurationNanos(dur), pctStr}
	if wide {
		cells = []string{label, dashIfEmpty(s.service), dashIfEmpty(s.kind), s.spanID, formatStartOffset(s.start - totals.start), formatDurationNanos(dur), pctStr}
	}
	pctIdx := len(cells) - 1
	for i := range cells {
		switch {
		case i == pctIdx && !hasPct:
			// Em-dash cell: no color thresholds.
		case i == pctIdx:
			cells[i] = style.ColorPercent(cells[i], pct, dim, isErr)
		default:
			// SPAN cell (i==0) gets error tint; other cells only get dim.
			cells[i] = style.ColorCell(cells[i], dim, isErr && i == 0)
		}
	}
	return cells
}

// spanLabel renders the SPAN cell text (prefix + connector + name + ⚠).
func spanLabel(s *traceSpan, prefix, connector string) string {
	name := s.name
	if name == "" {
		name = "-"
	}
	if s.statusCode == "STATUS_CODE_ERROR" {
		name = "⚠ " + name
	}
	return prefix + connector + name
}

func formatTrace(w io.Writer, resp *GetTraceResponse, wide bool) error {
	var spans []traceSpan
	if resp != nil && resp.Trace != nil {
		spans = extractTraceSpans(resp.Trace)
	}
	if len(spans) == 0 {
		// Empty/nil trace renders the header line only — no body, no panic.
		fmt.Fprintf(w, "Trace -  duration: %s  spans: 0  services: 0\n", formatDurationNanos(0))
		return nil
	}

	totals := aggregateTotals(spans)
	tree := buildTraceTree(spans)
	asyncTail := detectAsyncTail(tree, totals)
	writeTraceHeader(w, totals, len(spans), asyncTail)

	headers := []string{"SPAN", "SERVICE", "SPAN_ID", "DURATION", "%"}
	// Fixed widths for columns with predictable max sizes: prevents lipgloss from
	// shrinking them when the terminal is narrow, so only SPAN/SERVICE compress.
	//   normal: SPAN_ID=18 (16 hex+2pad), DURATION=12, %=9
	//   wide adds: KIND=14 (max "unspecified"+2pad), START=12, same DURATION/%%
	colWidths := []int{0, 0, 18, 12, 9}
	if wide {
		headers = []string{"SPAN", "SERVICE", "KIND", "SPAN_ID", "START", "DURATION", "%"}
		colWidths = []int{0, 0, 14, 18, 12, 12, 9}
	}

	newTable := func() *style.TableBuilder {
		return style.NewTable(headers...).ColumnWidths(colWidths)
	}

	walk := func(tbl *style.TableBuilder, root *traceSpan) {
		var rec func(s *traceSpan, prefix string, isLast, isRoot bool)
		rec = func(s *traceSpan, prefix string, isLast, isRoot bool) {
			connector, nextPrefix := treeConnector(prefix, isLast, isRoot)
			label := spanLabel(s, prefix, connector)
			tbl.Row(rowCells(s, label, totals, wide)...)
			kids := tree.children[s.spanID]
			for i, c := range kids {
				rec(c, nextPrefix, i == len(kids)-1, false)
			}
		}
		rec(root, "", true, true)
	}

	if len(tree.attached) > 0 {
		tbl := newTable()
		for _, root := range tree.attached {
			walk(tbl, root)
		}
		if err := tbl.Render(w); err != nil {
			return err
		}
	}

	if len(tree.orphans) > 0 {
		divider := fmt.Sprintf("── Detached subtrees (%d) — parent span not in trace ──", len(tree.orphans))
		fmt.Fprintf(w, "\n%s\n", style.ColorMutedText(divider))
		tbl := newTable()
		for _, root := range tree.orphans {
			walk(tbl, root)
		}
		if err := tbl.Render(w); err != nil {
			return err
		}
	}

	return nil
}

// treeConnector returns (connector, nextPrefix) for a span row given whether
// it's the last sibling and whether it's a root.
func treeConnector(prefix string, isLast, isRoot bool) (string, string) {
	if isRoot {
		return "", ""
	}
	if isLast {
		return "└ ", prefix + "  "
	}
	return "├ ", prefix + "│ "
}

func extractTraceSpans(trace map[string]any) []traceSpan {
	var result []traceSpan
	rss, _ := trace["resourceSpans"].([]any)
	for _, rsAny := range rss {
		rs, ok := rsAny.(map[string]any)
		if !ok {
			continue
		}
		service := extractServiceName(rs)
		scopeSpans, _ := rs["scopeSpans"].([]any)
		for _, ssAny := range scopeSpans {
			ss, ok := ssAny.(map[string]any)
			if !ok {
				continue
			}
			spans, _ := ss["spans"].([]any)
			for _, spanAny := range spans {
				span, ok := spanAny.(map[string]any)
				if !ok {
					continue
				}
				ex := traceSpan{service: service}
				if n, ok := span["name"].(string); ok {
					ex.name = n
				}
				ex.traceID = decodeIDField(span["traceId"], traceIDBytes)
				ex.spanID = decodeIDField(span["spanId"], spanIDBytes)
				ex.parentID = decodeIDField(span["parentSpanId"], spanIDBytes)
				if v, ok := span["startTimeUnixNano"].(string); ok {
					ex.start, _ = strconv.ParseInt(v, 10, 64)
				}
				if v, ok := span["endTimeUnixNano"].(string); ok {
					ex.end, _ = strconv.ParseInt(v, 10, 64)
				}
				ex.kind = "-"
				if k, ok := span["kind"].(string); ok && k != "" {
					ex.kind = strings.ToLower(strings.TrimPrefix(k, "SPAN_KIND_"))
				}
				if status, ok := span["status"].(map[string]any); ok {
					if code, ok := status["code"].(string); ok {
						ex.statusCode = code
					}
				}
				result = append(result, ex)
			}
		}
	}
	return result
}

func extractServiceName(rs map[string]any) string {
	resource, _ := rs["resource"].(map[string]any)
	attrs, _ := resource["attributes"].([]any)
	for _, attrAny := range attrs {
		attr, ok := attrAny.(map[string]any)
		if !ok {
			continue
		}
		if attr["key"] == "service.name" {
			if val, ok := attr["value"].(map[string]any); ok {
				if s, ok := val["stringValue"].(string); ok {
					return s
				}
			}
		}
	}
	return "-"
}

// OTLP/JSON trace and span ID byte lengths.
const (
	traceIDBytes = 16
	spanIDBytes  = 8
)

// decodeIDField returns the lowercase-hex form of an OTLP/JSON trace or span
// ID, accepting either of the two encodings emitters use in practice.
//
// The OTel JSON spec mandates lowercase hex strings for byte fields, but the
// proto3 canonical JSON mapping (which Tempo's OTLP/JSON output historically
// followed) emits base64. We accept both. The disambiguation is unambiguous
// because hex is a strict subset of the base64 alphabet but uses a different
// fixed length: a 32-char hex trace ID is also valid base64, but base64-decoding
// it produces 24 bytes — not 16 — so the length check below sorts it out.
//
// expectedBytes is the wire-form decoded length (16 for trace IDs, 8 for span IDs).
// Inputs that match neither encoding are returned unchanged.
func decodeIDField(v any, expectedBytes int) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if len(s) == expectedBytes*2 {
		if _, err := hex.DecodeString(s); err == nil {
			return strings.ToLower(s)
		}
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == expectedBytes {
		return hex.EncodeToString(b)
	}
	return s
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatDurationNanos(ns int64) string {
	if ns <= 0 {
		return "?"
	}
	switch {
	case ns < 1_000:
		return fmt.Sprintf("%dns", ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%dµs", ns/1_000)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%dms", ns/1_000_000)
	case ns < 60_000_000_000:
		return fmt.Sprintf("%.2fs", float64(ns)/1e9)
	default:
		m := ns / 60_000_000_000
		s := (ns % 60_000_000_000) / 1_000_000_000
		return fmt.Sprintf("%dm%ds", m, s)
	}
}

func formatStartOffset(ns int64) string {
	if ns <= 0 {
		return "+0"
	}
	return "+" + formatDurationNanos(ns)
}
