package elasticsearch

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
)

// SearchRequest represents a Lucene document search (raw_data or logs).
type SearchRequest struct {
	Query     string // Lucene query string; empty matches all documents
	Size      int    // max documents to return
	TimeField string
	Start     time.Time
	End       time.Time
}

// AggsRequest represents a metric aggregation over a date histogram,
// optionally grouped by a terms bucket.
type AggsRequest struct {
	Query     string // Lucene query string scoping the aggregation
	Agg       string // count, avg, sum, min, max, cardinality
	Field     string // aggregated field (required unless Agg is count)
	GroupBy   string // optional terms field to split series by
	GroupSize int    // max number of terms groups
	TimeField string
	Start     time.Time
	End       time.Time
	StepMs    int64
}

// ValidateAgg checks the aggregation type and its field requirement.
func ValidateAgg(agg, field string) error {
	switch agg {
	case "count", "avg", "sum", "min", "max", "cardinality":
	default:
		return fmt.Errorf("invalid --agg %q (supported: avg, cardinality, count, max, min, sum)", agg)
	}
	if agg != "count" && field == "" {
		return fmt.Errorf("--field is required for --agg %s", agg)
	}
	return nil
}

// isLogsInternalField reports whether a column is Elasticsearch plugin
// bookkeeping dropped from logs output; these carry raw document/session
// internals, not log content.
func isLogsInternalField(name string) bool {
	switch name {
	case "_source", "sort", "highlight", "_type":
		return true
	}
	return false
}

// trimLogsColumns removes plugin-internal columns from a logs table response.
func trimLogsColumns(resp *querysql.QueryResponse) *querysql.QueryResponse {
	keep := make([]int, 0, len(resp.Columns))
	for i, c := range resp.Columns {
		if !isLogsInternalField(c.Name) {
			keep = append(keep, i)
		}
	}
	if len(keep) == len(resp.Columns) {
		return resp
	}

	out := &querysql.QueryResponse{}
	for _, i := range keep {
		out.Columns = append(out.Columns, resp.Columns[i])
	}
	for _, row := range resp.Rows {
		nr := make([]any, 0, len(keep))
		for _, i := range keep {
			if i < len(row) {
				nr = append(nr, row[i])
			} else {
				nr = append(nr, nil)
			}
		}
		out.Rows = append(out.Rows, nr)
	}
	return out
}

// convertTimeColumns rewrites epoch-millisecond values in time-typed columns
// to RFC3339 strings so table and JSON output are human-readable.
func convertTimeColumns(resp *querysql.QueryResponse) *querysql.QueryResponse {
	timeCols := map[int]bool{}
	for i, c := range resp.Columns {
		if c.Type == "time" {
			timeCols[i] = true
		}
	}
	if len(timeCols) == 0 {
		return resp
	}
	for _, row := range resp.Rows {
		for i := range row {
			if !timeCols[i] || row[i] == nil {
				continue
			}
			if ms, ok := row[i].(float64); ok {
				row[i] = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
			}
		}
	}
	return resp
}

// MetricSeries is one aggregation series: a terms group (or the whole result
// set when ungrouped) over the date histogram.
type MetricSeries struct {
	Name       string      `json:"name"`
	Timestamps []time.Time `json:"timestamps"`
	Values     []*float64  `json:"values"`
}

// MetricsResponse holds the parsed aggregation query result.
type MetricsResponse struct {
	Series []MetricSeries `json:"series"`
}

// IndexInfo describes an Elasticsearch index from the _mapping response.
type IndexInfo struct {
	Name   string `json:"name"`
	Fields int    `json:"fields"`
}

// FieldInfo describes a field within an index mapping.
type FieldInfo struct {
	Index string `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

type mappingProperty struct {
	Type       string                     `json:"type"`
	Properties map[string]mappingProperty `json:"properties"`
}

type indexMapping struct {
	Mappings struct {
		Properties map[string]mappingProperty `json:"properties"`
	} `json:"mappings"`
}

// ParseMapping parses a _mapping response into index and field listings,
// flattening nested object properties with dotted names.
func ParseMapping(body []byte) ([]IndexInfo, []FieldInfo, error) {
	var raw map[string]indexMapping
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("failed to parse mapping response: %w", err)
	}

	var indices []IndexInfo
	var fields []FieldInfo
	for index, m := range raw {
		var idxFields []FieldInfo
		flattenProperties(index, "", m.Mappings.Properties, &idxFields)
		sort.Slice(idxFields, func(i, j int) bool { return idxFields[i].Name < idxFields[j].Name })
		indices = append(indices, IndexInfo{Name: index, Fields: len(idxFields)})
		fields = append(fields, idxFields...)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i].Name < indices[j].Name })
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Index != fields[j].Index {
			return fields[i].Index < fields[j].Index
		}
		return fields[i].Name < fields[j].Name
	})
	return indices, fields, nil
}

func flattenProperties(index, prefix string, props map[string]mappingProperty, out *[]FieldInfo) {
	for name, p := range props {
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		if len(p.Properties) > 0 {
			flattenProperties(index, full, p.Properties, out)
			continue
		}
		*out = append(*out, FieldInfo{Index: index, Name: full, Type: p.Type})
	}
}
