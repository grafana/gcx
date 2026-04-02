package tempo

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// FormatSearchTable formats a search response as a table.
func FormatSearchTable(w io.Writer, resp *SearchResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TRACE_ID\tSERVICE\tNAME\tDURATION\tSTART")

	for _, t := range resp.Traces {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			t.TraceID,
			t.RootServiceName,
			t.RootTraceName,
			formatDuration(t.DurationMs),
			formatStartTime(t.StartTimeUnixNano),
		)
	}

	return tw.Flush()
}

// FormatTagsTable formats a tags response as a table.
func FormatTagsTable(w io.Writer, resp *TagsResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SCOPE\tTAG")

	for _, scope := range resp.Scopes {
		for _, tag := range scope.Tags {
			fmt.Fprintf(tw, "%s\t%s\n", scope.Name, tag)
		}
	}

	return tw.Flush()
}

// FormatTagValuesTable formats a tag-values response as a table.
func FormatTagValuesTable(w io.Writer, resp *TagValuesResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TYPE\tVALUE")

	for _, tv := range resp.TagValues {
		fmt.Fprintf(tw, "%s\t%s\n", tv.Type, fmt.Sprint(tv.Value))
	}

	return tw.Flush()
}

// FormatMetricsTable formats a metrics response as a table.
func FormatMetricsTable(w io.Writer, resp *MetricsResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LABELS\tTIMESTAMP\tVALUE")

	for _, series := range resp.Series {
		labels := FormatMetricsLabels(series.Labels)

		if len(series.Samples) > 0 {
			for _, sample := range series.Samples {
				fmt.Fprintf(tw, "%s\t%s\t%s\n",
					labels,
					sample.TimestampMs,
					strconv.FormatFloat(sample.Value, 'f', -1, 64),
				)
			}
		} else if series.Value != nil {
			fmt.Fprintf(tw, "%s\t%s\t%s\n",
				labels,
				series.TimestampMs,
				strconv.FormatFloat(*series.Value, 'f', -1, 64),
			)
		}
	}

	return tw.Flush()
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
