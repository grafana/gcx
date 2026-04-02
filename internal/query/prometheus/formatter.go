package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// FormatTable formats a QueryResponse as a table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	switch resp.Data.ResultType {
	case "vector":
		return formatVectorTable(tw, resp)
	case "matrix":
		return formatMatrixTable(tw, resp)
	case "scalar":
		return formatScalarTable(tw, resp)
	default:
		return fmt.Errorf("unsupported result type: %s", resp.Data.ResultType)
	}
}

func formatVectorTable(tw *tabwriter.Writer, resp *QueryResponse) error {
	labelNames := collectLabelNames(resp.Data.Result)

	// Print header
	header := make([]string, 0, len(labelNames)+2)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "TIMESTAMP", "VALUE")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	// Print rows
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
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}

	return tw.Flush()
}

func formatMatrixTable(tw *tabwriter.Writer, resp *QueryResponse) error {
	labelNames := collectLabelNames(resp.Data.Result)

	// Print header
	header := make([]string, 0, len(labelNames)+2)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "TIMESTAMP", "VALUE")
	fmt.Fprintln(tw, strings.Join(header, "\t"))

	// Print rows
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
			fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
	}

	return tw.Flush()
}

func formatScalarTable(tw *tabwriter.Writer, resp *QueryResponse) error {
	fmt.Fprintln(tw, "TIMESTAMP\tVALUE")

	for _, sample := range resp.Data.Result {
		if len(sample.Value) >= 2 {
			ts := parseTimestamp(sample.Value[0])
			val := parseValue(sample.Value[1])
			fmt.Fprintf(tw, "%s\t%s\n", ts, val)
		}
	}

	return tw.Flush()
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
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL")
	for _, label := range resp.Data {
		fmt.Fprintln(tw, label)
	}
	return tw.Flush()
}

// FormatMetadataTable formats a MetadataResponse as a table.
func FormatMetadataTable(w io.Writer, resp *MetadataResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "METRIC\tTYPE\tHELP")

	metrics := make([]string, 0, len(resp.Data))
	for metric := range resp.Data {
		metrics = append(metrics, metric)
	}
	sort.Strings(metrics)

	for _, metric := range metrics {
		entries := resp.Data[metric]
		for _, entry := range entries {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", metric, entry.Type, entry.Help)
		}
	}

	return tw.Flush()
}
