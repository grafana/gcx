package tempo_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSearchTable(t *testing.T) {
	resp := &tempo.SearchResponse{
		Traces: []tempo.SearchTrace{
			{
				TraceID:           "abc123",
				RootServiceName:   "frontend",
				RootTraceName:     "GET /api/users",
				StartTimeUnixNano: "1700000000000000000", // 2023-11-14T22:13:20Z
				DurationMs:        42,
			},
			{
				TraceID:           "def456",
				RootServiceName:   "backend",
				RootTraceName:     "POST /api/orders",
				StartTimeUnixNano: "1700003600000000000",
				DurationMs:        1500,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatSearchTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	// Header
	assert.Contains(t, out, "TRACE_ID")
	assert.Contains(t, out, "SERVICE")
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "DURATION")
	assert.Contains(t, out, "START")

	// Row data
	assert.Contains(t, out, "abc123")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "GET /api/users")
	assert.Contains(t, out, "42ms")

	assert.Contains(t, out, "def456")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "1.50s") // 1500ms formatted as seconds

	// Timestamp should be RFC3339 in local timezone
	expectedDate := time.Unix(0, 1700000000000000000).Format(time.RFC3339)[:11] // "YYYY-MM-DDT" prefix
	assert.Contains(t, out, expectedDate)
}

func TestFormatSearchTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := tempo.FormatSearchTable(&buf, &tempo.SearchResponse{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "TRACE_ID")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1)
}

func TestFormatTagsTable(t *testing.T) {
	resp := &tempo.TagsResponse{
		Scopes: []tempo.TagScope{
			{Name: "resource", Tags: []string{"service.name", "host.name"}},
			{Name: "span", Tags: []string{"http.method", "http.status_code"}},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatTagsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "SCOPE")
	assert.Contains(t, out, "TAG")
	assert.Contains(t, out, "resource")
	assert.Contains(t, out, "service.name")
	assert.Contains(t, out, "host.name")
	assert.Contains(t, out, "span")
	assert.Contains(t, out, "http.method")

	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 5) // 1 header + 4 data rows
}

func TestFormatTagValuesTable(t *testing.T) {
	resp := &tempo.TagValuesResponse{
		TagValues: []tempo.TagValue{
			{Type: "string", Value: "frontend"},
			{Type: "string", Value: "backend"},
			{Type: "int", Value: float64(200)},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatTagValuesTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "VALUE")
	assert.Contains(t, out, "string")
	assert.Contains(t, out, "frontend")
	assert.Contains(t, out, "backend")
	assert.Contains(t, out, "int")
	assert.Contains(t, out, "200")
}

func TestFormatMetricsTable_Range(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Instant: false,
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "web"}},
				},
				Samples: []tempo.MetricsSample{
					{TimestampMs: "1700000000000", Value: 42.5},
					{TimestampMs: "1700000060000", Value: 43.0},
				},
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "VALUE")
	assert.Contains(t, out, "42.5")
	assert.Contains(t, out, "43")
	assert.Contains(t, out, "1700000000000")
	assert.Contains(t, out, "1700000060000")
}

func TestFormatMetricsTable_Instant(t *testing.T) {
	val := float64(99)
	resp := &tempo.MetricsResponse{
		Instant: true,
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "api"}},
				},
				TimestampMs: "1700003600000",
				Value:       &val,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, "VALUE")
	assert.NotContains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "99")
	assert.NotContains(t, out, "1700003600000")
}

func TestFormatMetricsTable_InstantWithoutTimestamp(t *testing.T) {
	val := float64(99)
	resp := &tempo.MetricsResponse{
		Instant: true,
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "api"}},
				},
				Value: &val,
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "LABELS")
	assert.Contains(t, out, "VALUE")
	assert.NotContains(t, out, "TIMESTAMP")
	assert.Contains(t, out, "99")
}

func TestFormatMetricsTable_EmptySeries(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Instant: false,
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					{Key: "service", Value: map[string]any{"stringValue": "web"}},
				},
				// No Samples and no Value — row should be skipped
			},
		},
	}

	var buf bytes.Buffer
	err := tempo.FormatMetricsTable(&buf, resp)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1) // Only header
}

// ==============================================================
// Trace tree formatter tests (FormatTraceTable, FormatTraceWide,
// formatDurationNanos). Cover the acceptance criteria from
// docs/specs/feature-traces-get-table/spec.md.
// ==============================================================

// b64ID encodes a hex string as the base64 wire representation Tempo emits
// for span/trace IDs. Round-trips back to the original hex via decodeIDField.
func b64ID(t *testing.T, hexID string) string {
	t.Helper()
	raw, err := hex.DecodeString(hexID)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

// span builds a minimal OTLP span map. parentHexID may be empty for roots.
// kind and statusCode may be empty to omit those fields entirely.
func mkSpan(t *testing.T, name, spanHexID, parentHexID, kind, statusCode string, startNs, endNs int64) map[string]any {
	t.Helper()
	s := map[string]any{
		"spanId":            b64ID(t, spanHexID),
		"name":              name,
		"startTimeUnixNano": strconv.FormatInt(startNs, 10),
		"endTimeUnixNano":   strconv.FormatInt(endNs, 10),
	}
	if parentHexID != "" {
		s["parentSpanId"] = b64ID(t, parentHexID)
	}
	if kind != "" {
		s["kind"] = kind
	}
	if statusCode != "" {
		s["status"] = map[string]any{"code": statusCode}
	}
	return s
}

// mkResourceSpans wraps spans for a single service. All spans get traceID32
// as their traceId — every test in this file uses the same canonical ID.
func mkResourceSpans(serviceName string, spans ...map[string]any) map[string]any {
	rawTrace, _ := hex.DecodeString(traceID32)
	encTrace := base64.StdEncoding.EncodeToString(rawTrace)
	for _, s := range spans {
		s["traceId"] = encTrace
	}
	return map[string]any{
		"resource": map[string]any{
			"attributes": []any{
				map[string]any{
					"key": "service.name",
					"value": map[string]any{
						"stringValue": serviceName,
					},
				},
			},
		},
		"scopeSpans": []any{
			map[string]any{
				"spans": toAnySlice(spans),
			},
		},
	}
}

func toAnySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// mkTrace assembles a *GetTraceResponse containing the given resourceSpans.
func mkTrace(rss ...map[string]any) *tempo.GetTraceResponse {
	return &tempo.GetTraceResponse{
		Trace: map[string]any{
			"resourceSpans": toAnySlice(rss),
		},
	}
}

const traceID32 = "abcdef0123456789abcdef0123456789"

// TestFormatDurationNanos covers representative durations and the non-positive sentinels.
func TestFormatDurationNanos(t *testing.T) {
	// formatDurationNanos is package-private; we exercise it indirectly via
	// FormatTraceWide's START column, which delegates to formatDurationNanos
	// (or formatStartOffset wrapping it).
	cases := []struct {
		name    string
		spanNs  int64
		wantSub string
	}{
		{"500ns precision", 500, "500ns"},
		{"37µs precision", 37_000, "37µs"},
		{"504ms precision", 504_000_000, "504ms"},
		{"28.65s precision", 28_650_000_000, "28.65s"},
		{"1m32s precision", 92_000_000_000, "1m32s"},
		{"zero renders question mark", 0, "?"},
		{"negative renders question mark", -1, "?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Positive durations: span runs from t=0 to t=spanNs.
			// Non-positive durations: shift start forward so end <= start
			// triggers the formatDurationNanos "?" sentinel.
			start, end := int64(0), tc.spanNs
			if tc.spanNs <= 0 {
				start, end = 1000, 1000+tc.spanNs
			}
			resp := mkTrace(mkResourceSpans("svc",
				mkSpan(t, "root", "0000000000000001", "", "", "", start, end),
			))
			var buf bytes.Buffer
			require.NoError(t, tempo.FormatTraceTable(&buf, resp))
			assert.Contains(t, buf.String(), tc.wantSub)
		})
	}
}

// TestFormatTraceTable_TreeConnectors covers a 3-span trace root → child →
// grandchild: three rows in order with `└ ` for the only child and
// `  └ ` for the grandchild.
func TestFormatTraceTable_TreeConnectors(t *testing.T) {
	resp := mkTrace(mkResourceSpans("frontend",
		mkSpan(t, "root", "0000000000000001", "", "SPAN_KIND_SERVER", "STATUS_CODE_OK", 0, 100_000_000),
		mkSpan(t, "child", "0000000000000002", "0000000000000001", "SPAN_KIND_INTERNAL", "STATUS_CODE_OK", 10_000_000, 80_000_000),
		mkSpan(t, "grandchild", "0000000000000003", "0000000000000002", "SPAN_KIND_INTERNAL", "STATUS_CODE_OK", 20_000_000, 60_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	// Header.
	assert.Contains(t, out, "Trace "+traceID32)
	assert.Contains(t, out, "spans: 3")

	// Rows in order root → child → grandchild.
	rootIdx := strings.Index(out, "root")
	childIdx := strings.Index(out, "child")
	grandIdx := strings.Index(out, "grandchild")
	require.GreaterOrEqual(t, rootIdx, 0, "root should appear")
	require.Less(t, rootIdx, childIdx, "root must precede child: %s", out)
	require.Less(t, childIdx, grandIdx, "child must precede grandchild: %s", out)

	// Tree connectors.
	assert.Contains(t, out, "└ child", "child should be marked as last sibling")
	assert.Contains(t, out, "  └ grandchild", "grandchild should be a continuation under last ancestor")
}

// TestFormatTraceTable_DetachedSubtrees covers: orphan span renders below
// the divider and the divider count matches.
func TestFormatTraceTable_DetachedSubtrees(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "attached-root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000),
		// parent is a span that's NOT in the response → orphan.
		mkSpan(t, "orphan", "0000000000000002", "00000000000000ff", "", "STATUS_CODE_OK", 50_000_000, 70_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	attachedIdx := strings.Index(out, "attached-root")
	dividerIdx := strings.Index(out, "── Detached subtrees (1) — parent span not in trace ──")
	orphanIdx := strings.Index(out, "orphan")

	require.GreaterOrEqual(t, attachedIdx, 0, "attached-root should appear")
	require.GreaterOrEqual(t, dividerIdx, 0, "divider should appear")
	require.GreaterOrEqual(t, orphanIdx, 0, "orphan should appear")
	assert.Less(t, attachedIdx, dividerIdx, "attached should render before divider")
	assert.Less(t, dividerIdx, orphanIdx, "divider should render before orphan")
}

// TestFormatTraceTable_NoOrphansNoDivider covers the negative constraint:
// no divider is rendered when orphan count is zero.
func TestFormatTraceTable_NoOrphansNoDivider(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000),
	))
	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	assert.NotContains(t, buf.String(), "Detached subtrees")
}

// TestFormatTraceTable_AsyncTail covers: header is suffixed when the latest
// span end is >1s after the latest end of attached subtrees.
func TestFormatTraceTable_AsyncTail(t *testing.T) {
	// Attached subtree ends at 100ms. Orphan extends to 100ms + 2s — beyond
	// the 1s threshold relative to the attached max end.
	const oneMs = int64(time.Millisecond)
	const twoSec = int64(2 * time.Second)
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100*oneMs),
		mkSpan(t, "trailing", "0000000000000002", "00000000000000ff", "", "STATUS_CODE_OK", 100*oneMs, 100*oneMs+twoSec),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	assert.Contains(t, buf.String(), "(async tail detected)")
}

// TestFormatTraceTable_NoAsyncTailWhenAttachedExtends covers the negative:
// when no orphan or trailing span exceeds the attached max end by >1s, the
// header has no async-tail suffix.
func TestFormatTraceTable_NoAsyncTailWhenAttachedExtends(t *testing.T) {
	const oneMs = int64(time.Millisecond)
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 200*oneMs),
		mkSpan(t, "child", "0000000000000002", "0000000000000001", "", "STATUS_CODE_OK", 50*oneMs, 150*oneMs),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	assert.NotContains(t, buf.String(), "async tail")
}

// TestFormatTraceTable_ErrorSpanPrefix covers: error span gets ⚠ prefix.
// Color rendering is exercised in style_test (env has no TTY).
func TestFormatTraceTable_ErrorSpanPrefix(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "ok-root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000),
		mkSpan(t, "boom", "0000000000000002", "0000000000000001", "", "STATUS_CODE_ERROR", 10_000_000, 50_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	assert.Contains(t, out, "⚠ boom", "error span name should be prefixed with ⚠ ")
	assert.NotContains(t, out, "⚠ ok-root", "non-error spans should not be prefixed")
}

// TestFormatTraceTable_NilTrace covers: nil Trace renders only the header line, no body, no panic.
func TestFormatTraceTable_NilTrace(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, tempo.FormatTraceTable(&buf, nil))
		out := buf.String()
		assert.Contains(t, out, "spans: 0")
		assert.Contains(t, out, "services: 0")
		assert.NotContains(t, out, "SPAN_ID", "no body should be rendered")
	})

	t.Run("empty Trace map", func(t *testing.T) {
		resp := &tempo.GetTraceResponse{}
		var buf bytes.Buffer
		require.NoError(t, tempo.FormatTraceTable(&buf, resp))
		out := buf.String()
		assert.Contains(t, out, "spans: 0")
		assert.Contains(t, out, "services: 0")
		assert.NotContains(t, out, "SPAN_ID")
	})

	t.Run("Trace with no resourceSpans", func(t *testing.T) {
		resp := &tempo.GetTraceResponse{Trace: map[string]any{}}
		var buf bytes.Buffer
		require.NoError(t, tempo.FormatTraceTable(&buf, resp))
		assert.Contains(t, buf.String(), "spans: 0")
	})
}

// TestFormatTraceTable_SingleSpan covers: one span renders one row with no tree connectors.
func TestFormatTraceTable_SingleSpan(t *testing.T) {
	resp := mkTrace(mkResourceSpans("solo",
		mkSpan(t, "only-root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 50_000_000),
	))
	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	assert.Contains(t, out, "only-root")
	// No tree connectors should be present in a single-row tree.
	assert.NotContains(t, out, "└ only-root")
	assert.NotContains(t, out, "├ only-root")
	assert.Contains(t, out, "spans: 1")
}

// TestFormatTraceTable_AllOrphans covers: when every span is an orphan,
// only the detached-subtrees section renders (no attached table).
func TestFormatTraceTable_AllOrphans(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		// Every span has a parent that's not in the response.
		mkSpan(t, "lost-1", "0000000000000001", "00000000000000aa", "", "STATUS_CODE_OK", 0, 50_000_000),
		mkSpan(t, "lost-2", "0000000000000002", "00000000000000bb", "", "STATUS_CODE_OK", 10_000_000, 60_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	assert.Contains(t, out, "Detached subtrees (2)", "divider should reflect 2 orphans")
	assert.Contains(t, out, "lost-1")
	assert.Contains(t, out, "lost-2")
	assert.Contains(t, out, "spans: 2")
}

// TestFormatTraceWide_Columns covers: wide column order is SPAN, SERVICE,
// KIND, SPAN_ID, START, DURATION, %; KIND prefix stripped; root START is +0.
func TestFormatTraceWide_Columns(t *testing.T) {
	resp := mkTrace(mkResourceSpans("api",
		mkSpan(t, "root", "0000000000000001", "", "SPAN_KIND_SERVER", "STATUS_CODE_OK", 1_000_000_000, 1_500_000_000),
		mkSpan(t, "child", "0000000000000002", "0000000000000001", "SPAN_KIND_CLIENT", "STATUS_CODE_OK", 1_100_000_000, 1_400_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceWide(&buf, resp))
	out := buf.String()

	// All wide-only headers present.
	for _, h := range []string{"SPAN", "SERVICE", "KIND", "SPAN_ID", "START", "DURATION", "%"} {
		assert.Contains(t, out, h)
	}

	// Column ordering.
	spanIdx := strings.Index(out, "SPAN")
	serviceIdx := strings.Index(out, "SERVICE")
	kindIdx := strings.Index(out, "KIND")
	spanIDIdx := strings.Index(out, "SPAN_ID")
	startIdx := strings.Index(out, "START")
	durationIdx := strings.Index(out, "DURATION")
	require.True(t, spanIdx < serviceIdx && serviceIdx < kindIdx && kindIdx < spanIDIdx && spanIDIdx < startIdx && startIdx < durationIdx,
		"unexpected column order: %s", out)

	// KIND prefix stripped. Spike lowercases — accept either case.
	assert.Regexp(t, "(?i)server", out)
	assert.Regexp(t, "(?i)client", out)
	assert.NotContains(t, out, "SPAN_KIND_", "SPAN_KIND_ prefix MUST be stripped")

	// Root START is +0.
	assert.Contains(t, out, "+0", "root START offset must be +0")
}

// TestFormatTraceTable_InvalidDurationCells covers: span end <= start →
// DURATION renders ?, % renders —.
func TestFormatTraceTable_InvalidDurationCells(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		// Valid root sets up traceDur > 0.
		mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000),
		// Child has end <= start: duration <= 0.
		mkSpan(t, "broken", "0000000000000002", "0000000000000001", "", "STATUS_CODE_OK", 50_000_000, 50_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	// The broken row's DURATION is "?" and % is "—".
	// We assert by line containing "broken".
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "broken") {
			assert.Contains(t, line, "?", "broken span DURATION must be ?")
			assert.Contains(t, line, "—", "broken span %% must be em dash")
			return
		}
	}
	t.Fatalf("did not find broken row in output: %s", out)
}

// TestFormatTraceTable_ZeroTraceDuration covers AC: when total duration is 0
// (all spans degenerate), every row's % is —.
func TestFormatTraceTable_ZeroTraceDuration(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		// All spans have end == start so traceDur = 0.
		mkSpan(t, "deg-1", "0000000000000001", "", "", "STATUS_CODE_OK", 1000, 1000),
		mkSpan(t, "deg-2", "0000000000000002", "0000000000000001", "", "STATUS_CODE_OK", 1000, 1000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	// Every data row must show "—" in the % cell.
	rows := 0
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "deg-") {
			rows++
			assert.Contains(t, line, "—", "all rows must render — when trace duration is 0")
		}
	}
	assert.Equal(t, 2, rows, "expected 2 data rows")
}

// TestFormatTraceTable_ChildOrderingByStart covers: three children sort ascending
// by start time — t3 is marked last (└), t1 and t2 are non-last (├).
func TestFormatTraceTable_ChildOrderingByStart(t *testing.T) {
	// Build with start times intentionally out of insertion order to exercise sort.
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000),
		mkSpan(t, "t3", "0000000000000004", "0000000000000001", "", "STATUS_CODE_OK", 30_000_000, 60_000_000),
		mkSpan(t, "t1", "0000000000000002", "0000000000000001", "", "STATUS_CODE_OK", 10_000_000, 20_000_000),
		mkSpan(t, "t2", "0000000000000003", "0000000000000001", "", "STATUS_CODE_OK", 20_000_000, 30_000_000),
	))

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()

	t1Idx := strings.Index(out, "t1")
	t2Idx := strings.Index(out, "t2")
	t3Idx := strings.Index(out, "t3")
	require.True(t, t1Idx < t2Idx && t2Idx < t3Idx, "children must be ordered by ascending start")

	// Last child gets └, others get ├.
	assert.Contains(t, out, "├ t1")
	assert.Contains(t, out, "├ t2")
	assert.Contains(t, out, "└ t3")
}

// TestFormatTraceTable_SpanIDIsLowercaseHex covers: SPAN_ID renders
// as 16 lowercase hex chars after base64 decode.
func TestFormatTraceTable_SpanIDIsLowercaseHex(t *testing.T) {
	resp := mkTrace(mkResourceSpans("svc",
		mkSpan(t, "root", "abcdef0123456789", "", "", "STATUS_CODE_OK", 0, 100_000_000),
	))
	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()
	assert.Contains(t, out, "abcdef0123456789", "SPAN_ID must round-trip to 16 lowercase hex chars")
	assert.NotContains(t, out, "ABCDEF", "SPAN_ID must be lowercase")
}

// TestFormatTraceTable_MissingServiceRendersDash covers: missing
// service.name resource attribute renders as "-".
func TestFormatTraceTable_MissingServiceRendersDash(t *testing.T) {
	// Build a resourceSpans entry with no service.name attribute.
	// Build a span and stamp the canonical trace ID directly — no mkResourceSpans
	// because we deliberately want to exclude the service.name attribute.
	span := mkSpan(t, "root", "0000000000000001", "", "", "STATUS_CODE_OK", 0, 100_000_000)
	rawTrace, err := hex.DecodeString(traceID32)
	require.NoError(t, err)
	span["traceId"] = base64.StdEncoding.EncodeToString(rawTrace)

	resp := &tempo.GetTraceResponse{
		Trace: map[string]any{
			"resourceSpans": []any{
				map[string]any{
					"resource":   map[string]any{"attributes": []any{}},
					"scopeSpans": []any{map[string]any{"spans": []any{span}}},
				},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, tempo.FormatTraceTable(&buf, resp))
	out := buf.String()
	// Service column should fall back to "-". We can't assert exact placement
	// without parsing, but the literal "-" must appear somewhere on the row.
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "root") {
			// Service falls back to "-". The dash should appear before the SPAN_ID
			// column (which is lowercase hex 0000...0001).
			assert.Contains(t, line, "-", "missing service must render as -")
			return
		}
	}
	t.Fatalf("did not find root row")
}

func TestFormatMetricsLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels []tempo.MetricsLabel
		want   string
	}{
		{
			name:   "empty labels",
			labels: nil,
			want:   "{}",
		},
		{
			name: "single label",
			labels: []tempo.MetricsLabel{
				{Key: "service", Value: map[string]any{"stringValue": "web"}},
			},
			want: `{service="web"}`,
		},
		{
			name: "multiple labels sorted by key",
			labels: []tempo.MetricsLabel{
				{Key: "zone", Value: map[string]any{"stringValue": "us-east"}},
				{Key: "service", Value: map[string]any{"stringValue": "api"}},
			},
			want: `{service="api", zone="us-east"}`,
		},
		{
			name: "intValue extraction",
			labels: []tempo.MetricsLabel{
				{Key: "status", Value: map[string]any{"intValue": "200"}},
			},
			want: `{status="200"}`,
		},
		{
			name: "boolValue extraction",
			labels: []tempo.MetricsLabel{
				{Key: "error", Value: map[string]any{"boolValue": true}},
			},
			want: `{error="true"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tempo.FormatMetricsLabels(tc.labels)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestFormatTraceTable_IDEncodings covers both OTLP/JSON ID encodings:
// proto3 canonical base64 (Tempo's historical wire form) and the OTel JSON
// spec's lowercase hex. Hex chars are a subset of the base64 alphabet — a
// 32-char hex trace ID is also valid base64 — so the formatter must
// disambiguate by length, not by trying base64 first.
func TestFormatTraceTable_IDEncodings(t *testing.T) {
	const (
		traceHex  = "abcdef0123456789abcdef0123456789"
		spanHex   = "1111111111111111"
		parentHex = "2222222222222222"
	)
	rawTrace, err := hex.DecodeString(traceHex)
	require.NoError(t, err)
	traceB64 := base64.StdEncoding.EncodeToString(rawTrace)
	rawSpan, err := hex.DecodeString(spanHex)
	require.NoError(t, err)
	spanB64 := base64.StdEncoding.EncodeToString(rawSpan)

	tests := []struct {
		name      string
		traceID   string
		spanID    string
		parentID  string
		wantTrace string // expected substring in the header
		wantSpan  string // expected substring in the SPAN_ID column
	}{
		{
			name:      "base64 inputs (legacy proto3 mapping)",
			traceID:   traceB64,
			spanID:    spanB64,
			wantTrace: traceHex,
			wantSpan:  spanHex,
		},
		{
			name:      "hex inputs (OTel JSON spec)",
			traceID:   traceHex,
			spanID:    spanHex,
			wantTrace: traceHex,
			wantSpan:  spanHex,
		},
		{
			name:      "uppercase hex normalized to lowercase",
			traceID:   strings.ToUpper(traceHex),
			spanID:    strings.ToUpper(spanHex),
			wantTrace: traceHex,
			wantSpan:  spanHex,
		},
		{
			name:      "mixed: hex trace ID, base64 span ID",
			traceID:   traceHex,
			spanID:    spanB64,
			wantTrace: traceHex,
			wantSpan:  spanHex,
		},
		{
			name:      "hex parent span ID is preserved",
			traceID:   traceHex,
			spanID:    spanHex,
			parentID:  parentHex,
			wantTrace: traceHex,
			wantSpan:  spanHex,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			span := map[string]any{
				"traceId":           tc.traceID,
				"spanId":            tc.spanID,
				"name":              "root",
				"startTimeUnixNano": "0",
				"endTimeUnixNano":   "100000000",
				"status":            map[string]any{"code": "STATUS_CODE_OK"},
			}
			if tc.parentID != "" {
				span["parentSpanId"] = tc.parentID
			}
			rs := map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{
							"key":   "service.name",
							"value": map[string]any{"stringValue": "svc"},
						},
					},
				},
				"scopeSpans": []any{
					map[string]any{"spans": []any{span}},
				},
			}

			resp := &tempo.GetTraceResponse{
				Trace: map[string]any{"resourceSpans": []any{rs}},
			}

			var buf bytes.Buffer
			require.NoError(t, tempo.FormatTraceTable(&buf, resp))
			out := buf.String()

			assert.Contains(t, out, "Trace "+tc.wantTrace, "trace ID header must show lowercase hex")
			assert.Contains(t, out, tc.wantSpan, "SPAN_ID column must show lowercase hex")
			// The 24-char base64-decoded-from-hex string would be 48 hex chars.
			// If the formatter ever falls back to that path, the header would
			// contain a 48-char string. Guard against regression.
			assert.NotRegexp(t, `Trace [0-9a-f]{48}`, out, "must not double-decode hex IDs as base64")
		})
	}
}
