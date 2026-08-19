package prometheus

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// FormatTable formats a QueryResponse as a compact, human-readable table.
// Labels are collapsed into a single SERIES column by default. Use
// FormatWideTable to explode labels into individual columns.
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

// FormatWideTable formats a QueryResponse as a wide table with one column per
// label. This is useful for inspection but is too verbose for the default
// human-readable view.
func FormatWideTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	switch resp.Data.ResultType {
	case "vector":
		return formatVectorWideTable(w, resp)
	case "matrix":
		return formatMatrixWideTable(w, resp)
	case "scalar":
		return formatScalarTable(w, resp)
	default:
		return fmt.Errorf("unsupported result type: %s", resp.Data.ResultType)
	}
}

// FormatArrow formats a QueryResponse as an Arrow IPC payload, one column per
// label plus TIMESTAMP and VALUE — the same column layout as FormatWideTable,
// but with real types instead of stringified cells.
func FormatArrow(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		return nil
	}

	b, err := buildArrowTable(resp)
	if err != nil {
		return err
	}
	return b.Write(w)
}

func buildArrowTable(resp *QueryResponse) (*arrowtable.Builder, error) {
	switch resp.Data.ResultType {
	case "vector":
		return buildVectorArrow(resp), nil
	case "matrix":
		return buildMatrixArrow(resp), nil
	case "scalar":
		return buildScalarArrow(resp), nil
	default:
		return nil, fmt.Errorf("unsupported result type: %s", resp.Data.ResultType)
	}
}

func labelArrowFields(labelNames []string) []arrowtable.Field {
	fields := make([]arrowtable.Field, 0, len(labelNames)+2)
	for _, name := range labelNames {
		fields = append(fields, arrowtable.Utf8(strings.ToUpper(name)))
	}
	return append(fields, arrowtable.Timestamp("TIMESTAMP"), arrowtable.Float64("VALUE"))
}

func buildVectorArrow(resp *QueryResponse) *arrowtable.Builder {
	labelNames := collectLabelNames(resp.Data.Result)
	b := arrowtable.NewBuilder(labelArrowFields(labelNames))

	for _, sample := range resp.Data.Result {
		row := make([]any, 0, len(labelNames)+2)
		for _, name := range labelNames {
			row = append(row, sample.Metric[name])
		}
		ts, val := arrowTimestampValue(sample.Value)
		b.Row(append(row, ts, val)...)
	}

	return b
}

func buildMatrixArrow(resp *QueryResponse) *arrowtable.Builder {
	labelNames := collectLabelNames(resp.Data.Result)
	b := arrowtable.NewBuilder(labelArrowFields(labelNames))

	for _, sample := range resp.Data.Result {
		for _, point := range sample.Values {
			row := make([]any, 0, len(labelNames)+2)
			for _, name := range labelNames {
				row = append(row, sample.Metric[name])
			}
			ts, val := arrowTimestampValue(point)
			b.Row(append(row, ts, val)...)
		}
	}

	return b
}

func buildScalarArrow(resp *QueryResponse) *arrowtable.Builder {
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Timestamp("TIMESTAMP"),
		arrowtable.Float64("VALUE"),
	})

	for _, sample := range resp.Data.Result {
		ts, val := arrowTimestampValue(sample.Value)
		b.Row(ts, val)
	}

	return b
}

// arrowTimestampValue converts a Prometheus [timestamp, value] pair into a
// (time.Time, float64) row, returning (nil, nil) — appended as nulls in
// their columns — when the pair is missing or malformed rather than
// desyncing the row's column count.
func arrowTimestampValue(point []any) (any, any) {
	if len(point) < 2 {
		return nil, nil
	}
	var ts, val any
	if t, ok := parseTimestampTime(point[0]); ok {
		ts = t
	}
	if f, ok := parseFloatValue(point[1]); ok {
		val = f
	}
	return ts, val
}

func formatVectorTable(w io.Writer, resp *QueryResponse) error {
	t := style.NewTable("VALUE", "TIMESTAMP", "SERIES")

	for _, sample := range resp.Data.Result {
		if len(sample.Value) < 2 {
			continue
		}
		val := parseValue(sample.Value[1])
		ts := parseTimestamp(sample.Value[0])
		t.Row(val, ts, formatSeriesSelector(sample.Metric))
	}

	return t.Render(w)
}

func formatMatrixTable(w io.Writer, resp *QueryResponse) error {
	t := style.NewTable("VALUE", "TIMESTAMP", "SERIES")

	for _, sample := range resp.Data.Result {
		series := formatSeriesSelector(sample.Metric)
		for _, point := range sample.Values {
			if len(point) < 2 {
				continue
			}
			val := parseValue(point[1])
			ts := parseTimestamp(point[0])
			t.Row(val, ts, series)
		}
	}

	return t.Render(w)
}

func formatVectorWideTable(w io.Writer, resp *QueryResponse) error {
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

func formatMatrixWideTable(w io.Writer, resp *QueryResponse) error {
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
		t := time.Unix(int64(ts), int64((ts-float64(int64(ts)))*1e9)).UTC()
		return t.Format(time.RFC3339)
	case string:
		f, err := strconv.ParseFloat(ts, 64)
		if err != nil {
			return ts
		}
		t := time.Unix(int64(f), int64((f-float64(int64(f)))*1e9)).UTC()
		return t.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseTimestampTime is parseTimestamp's typed counterpart, for Arrow's
// Timestamp columns — returns ok=false for a value that can't be parsed as a
// Unix-epoch-seconds float rather than falling back to a string.
func parseTimestampTime(v any) (time.Time, bool) {
	switch ts := v.(type) {
	case float64:
		return time.Unix(int64(ts), int64((ts-float64(int64(ts)))*1e9)).UTC(), true
	case string:
		f, err := strconv.ParseFloat(ts, 64)
		if err != nil {
			return time.Time{}, false
		}
		return time.Unix(int64(f), int64((f-float64(int64(f)))*1e9)).UTC(), true
	default:
		return time.Time{}, false
	}
}

// parseFloatValue is parseValue's typed counterpart, for Arrow's Float64
// columns — unlike parseValue (which passes an already-string wire value
// through unchanged for display), this parses it as a float so special
// values like "NaN"/"+Inf"/"-Inf" become real float64s, not text.
func parseFloatValue(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
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

func formatSingleColumnTable(w io.Writer, header string, values []string) error {
	t := style.NewTable(header)
	for _, v := range values {
		t.Row(v)
	}
	return t.Render(w)
}

// SingleColumnTableCodec is a table codec for any payload rendering as a
// single-column list. Header is the column title; Rows extracts the row
// values from the payload, returning false for an unexpected payload type.
// Commands with single-column table output register an instance instead of
// writing their own codec.
type SingleColumnTableCodec struct {
	Header string
	Rows   func(data any) ([]string, bool)
}

func (c *SingleColumnTableCodec) Format() format.Format {
	return "table"
}

func (c *SingleColumnTableCodec) Encode(w io.Writer, data any) error {
	rows, ok := c.Rows(data)
	if !ok {
		return fmt.Errorf("invalid data type for %s table codec", strings.ToLower(c.Header))
	}
	return formatSingleColumnTable(w, c.Header, rows)
}

func (c *SingleColumnTableCodec) Decode(io.Reader, any) error {
	return fmt.Errorf("%s table codec does not support decoding", strings.ToLower(c.Header))
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

// FormatCardinalityLabelNamesTable formats a label names cardinality response as
// two tables: a summary of the response-level totals followed by the per-label-name
// distinct value counts.
func FormatCardinalityLabelNamesTable(w io.Writer, resp *CardinalityLabelNamesResponse) error {
	if resp.LabelNamesCount == 0 && resp.LabelValuesCountTotal == 0 && len(resp.Cardinality) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	fmt.Fprintln(w, "Summary:")
	summary := style.NewTable("UNIQUE LABEL NAMES", "UNIQUE LABEL VALUES")
	summary.Row(strconv.Itoa(resp.LabelNamesCount), strconv.Itoa(resp.LabelValuesCountTotal))
	if err := summary.Render(w); err != nil {
		return err
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Label names:")
	if len(resp.Cardinality) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("LABEL NAME", "UNIQUE LABEL VALUES")
	for _, entry := range resp.Cardinality {
		t.Row(entry.LabelName, strconv.Itoa(entry.LabelValuesCount))
	}
	return t.Render(w)
}

// FormatCardinalityLabelValuesTable formats a label values cardinality response
// as two tables: a per-label summary (distinct value and series counts) followed
// by the per-value series-count breakdown.
func FormatCardinalityLabelValuesTable(w io.Writer, resp *CardinalityLabelValuesResponse) error {
	if len(resp.Labels) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	fmt.Fprintln(w, "Summary:")
	summary := style.NewTable("LABEL NAME", "UNIQUE LABEL VALUES", "SERIES")
	for _, label := range resp.Labels {
		summary.Row(label.LabelName, strconv.Itoa(label.LabelValuesCount), strconv.Itoa(label.SeriesCount))
	}
	if err := summary.Render(w); err != nil {
		return err
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Label values:")
	values := style.NewTable("LABEL NAME", "LABEL VALUE", "SERIES")
	for _, label := range resp.Labels {
		for _, value := range label.Cardinality {
			values.Row(label.LabelName, value.LabelValue, strconv.Itoa(value.SeriesCount))
		}
	}
	return values.Render(w)
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
