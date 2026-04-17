package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FormatTable formats a QueryResponse as a table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	switch resp.Data.ResultType {
	case "vector":
		return formatVectorTable(w, resp)
	case "matrix":
		return formatMatrixTable(w, resp)
	case "scalar":
		return formatScalarTable(w, resp)
	default:
		return fmt.Errorf("unsupported result type: %s", resp.Data.ResultType)
	}
}

func formatVectorTable(w io.Writer, resp *QueryResponse) error {
	labelNames := collectLabelNames(resp.Data.Result)

	header := make([]string, 0, len(labelNames)+2)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "TIMESTAMP", "VALUE")
	t := style.NewTable(header...)

	for _, sample := range resp.Data.Result {
		row := make([]string, 0, len(labelNames)+2)
		for _, name := range labelNames {
			row = append(row, sample.Metric[name])
		}

		if len(sample.Value) >= 2 {
			ts := parseTimestamp(sample.Value[0])
			val := parseValue(sample.Value[1])
			row = append(row, ts, val)
		}
		t.Row(row...)
	}

	return t.Render(w)
}

func formatMatrixTable(w io.Writer, resp *QueryResponse) error {
	labelNames := collectLabelNames(resp.Data.Result)

	header := make([]string, 0, len(labelNames)+2)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "TIMESTAMP", "VALUE")
	t := style.NewTable(header...)

	for _, sample := range resp.Data.Result {
		for _, point := range sample.Values {
			row := make([]string, 0, len(labelNames)+2)
			for _, name := range labelNames {
				row = append(row, sample.Metric[name])
			}

			if len(point) >= 2 {
				ts := parseTimestamp(point[0])
				val := parseValue(point[1])
				row = append(row, ts, val)
			}
			t.Row(row...)
		}
	}

	return t.Render(w)
}

func formatScalarTable(w io.Writer, resp *QueryResponse) error {
	t := style.NewTable("TIMESTAMP", "VALUE")

	for _, sample := range resp.Data.Result {
		if len(sample.Value) >= 2 {
			ts := parseTimestamp(sample.Value[0])
			val := parseValue(sample.Value[1])
			t.Row(ts, val)
		}
	}

	return t.Render(w)
}

func collectLabelNames(samples []Sample) []string {
	nameSet := make(map[string]struct{})
	for _, sample := range samples {
		for name := range sample.Metric {
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func parseTimestamp(v any) string {
	switch ts := v.(type) {
	case float64:
		t := time.Unix(int64(ts), int64((ts-float64(int64(ts)))*1e9))
		return t.Format(time.RFC3339)
	case string:
		f, err := strconv.ParseFloat(ts, 64)
		if err != nil {
			return ts
		}
		t := time.Unix(int64(f), int64((f-float64(int64(f)))*1e9))
		return t.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// FormatLabelsTable formats a LabelsResponse as a table.
func FormatLabelsTable(w io.Writer, resp *LabelsResponse) error {
	t := style.NewTable("LABEL")
	for _, label := range resp.Data {
		t.Row(label)
	}
	return t.Render(w)
}

// FormatSeriesTable formats a SeriesResponse as a table. Each row is a single
// series rendered in Prometheus selector syntax ({k="v",k2="v2"}) with labels
// sorted for stability.
func FormatSeriesTable(w io.Writer, resp *SeriesResponse) error {
	t := style.NewTable("SERIES")
	for _, series := range resp.Data {
		t.Row(formatSeriesSelector(series))
	}
	return t.Render(w)
}

func formatSeriesSelector(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(labels[k]))
	}
	b.WriteByte('}')
	return b.String()
}

// FormatMetadataTable formats a MetadataResponse as a table.
func FormatMetadataTable(w io.Writer, resp *MetadataResponse) error {
	t := style.NewTable("METRIC", "TYPE", "HELP")

	metrics := make([]string, 0, len(resp.Data))
	for metric := range resp.Data {
		metrics = append(metrics, metric)
	}
	sort.Strings(metrics)

	for _, metric := range metrics {
		entries := resp.Data[metric]
		for _, entry := range entries {
			t.Row(metric, entry.Type, entry.Help)
		}
	}

	return t.Render(w)
}
