package tempo

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

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
