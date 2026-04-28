package infinity

import (
	"fmt"
	"strconv"
	"time"
)

// QueryRequest represents an Infinity datasource query request.
type QueryRequest struct {
	Type         string            // json, csv, tsv, xml, graphql, html
	Source       string            // "url" or "inline"
	URL          string            // target URL (when Source == "url")
	Data         string            // inline data (when Source == "inline")
	RootSelector string            // JSONPath/XPath root selector
	Method       string            // HTTP method: GET or POST
	Headers      map[string]string // custom request headers
	GraphQL      string            // GraphQL query string (when Type == "graphql")
	Start        time.Time
	End          time.Time
}

// IsRange returns true if both Start and End are set.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse holds the query result as generic tabular data.
type QueryResponse struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// Column describes a single column in the query result.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// GrafanaQueryResponse is the raw response from Grafana's datasource query API.
type GrafanaQueryResponse struct {
	Results map[string]GrafanaResult `json:"results"`
}

// GrafanaResult is a single result entry.
type GrafanaResult struct {
	Frames      []DataFrame `json:"frames,omitempty"`
	Error       string      `json:"error,omitempty"`
	ErrorSource string      `json:"errorSource,omitempty"`
	Status      int         `json:"status,omitempty"`
}

// DataFrame is a Grafana data frame.
type DataFrame struct {
	Schema DataFrameSchema `json:"schema"`
	Data   DataFrameData   `json:"data"`
}

// DataFrameSchema describes the frame structure.
type DataFrameSchema struct {
	Name   string  `json:"name,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Field describes one field/column in a data frame.
type Field struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// DataFrameData holds column-oriented data values.
type DataFrameData struct {
	Values [][]any `json:"values,omitempty"`
}

// ConvertGrafanaResponse converts a Grafana data frame response into a flat
// QueryResponse with columns and rows suitable for table rendering.
func ConvertGrafanaResponse(grafanaResp *GrafanaQueryResponse) *QueryResponse {
	result := &QueryResponse{
		Columns: []Column{},
		Rows:    [][]any{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok {
		return result
	}

	for _, frame := range grafanaResult.Frames {
		if len(frame.Schema.Fields) == 0 {
			continue
		}

		for _, field := range frame.Schema.Fields {
			result.Columns = append(result.Columns, Column(field))
		}

		if len(frame.Data.Values) == 0 {
			break
		}

		numFields := len(frame.Schema.Fields)
		numRows := len(frame.Data.Values[0])
		for i := range numRows {
			row := make([]any, numFields)
			for colIdx, colValues := range frame.Data.Values {
				if colIdx < numFields && i < len(colValues) {
					row[colIdx] = colValues[i]
				}
			}
			result.Rows = append(result.Rows, row)
		}
		break // only process the first frame
	}

	return result
}

// ToString converts a value to its string representation for table rendering.
func ToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}
