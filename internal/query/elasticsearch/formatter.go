package elasticsearch

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FormatMetricsTable renders aggregation series as (time, value, series) rows.
func FormatMetricsTable(w io.Writer, resp *MetricsResponse) error {
	if len(resp.Series) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("TIME", "VALUE", "SERIES")
	for _, s := range resp.Series {
		for i, ts := range s.Timestamps {
			if i >= len(s.Values) {
				break
			}
			val := ""
			if s.Values[i] != nil {
				val = strconv.FormatFloat(*s.Values[i], 'f', -1, 64)
			}
			t.Row(ts.Format(time.RFC3339), val, s.Name)
		}
	}
	return t.Render(w)
}

// FormatIndices renders a list of Elasticsearch indices.
func FormatIndices(w io.Writer, indices []IndexInfo) error {
	if len(indices) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("INDEX", "FIELDS")
	for _, idx := range indices {
		t.Row(idx.Name, strconv.Itoa(idx.Fields))
	}
	return t.Render(w)
}

// FormatFields renders a list of mapped fields.
func FormatFields(w io.Writer, fields []FieldInfo) error {
	if len(fields) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("INDEX", "FIELD", "TYPE")
	for _, f := range fields {
		t.Row(f.Index, f.Name, f.Type)
	}
	return t.Render(w)
}
