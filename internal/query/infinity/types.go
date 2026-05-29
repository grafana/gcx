package infinity

import (
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
)

// QueryRequest represents an Infinity datasource query request.
type QueryRequest struct {
	Expr  string // optional root selector (JSONPath for JSON, XPath for XML/HTML)
	Start time.Time
	End   time.Time
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

// GrafanaQueryResponse is the top-level Grafana datasource query response.
type GrafanaQueryResponse = dataframe.Response

// GrafanaResult represents a single Grafana datasource query result.
type GrafanaResult = dataframe.Result

// DataFrame represents a Grafana data frame.
type DataFrame = dataframe.Frame

// DataFrameSchema describes the structure of a Grafana data frame.
type DataFrameSchema = dataframe.Schema

// Field describes a field in a Grafana data frame.
type Field = dataframe.Field

// DataFrameData contains column-oriented Grafana data frame values.
type DataFrameData = dataframe.Data

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
			result.Columns = append(result.Columns, Column{Name: field.Name, Type: field.Type})
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
