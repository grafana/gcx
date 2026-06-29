package sql

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/queryerror"
)

// ParseResponse parses a Grafana datasource query response (refId "A") into a
// row-oriented QueryResponse. product names the datasource in error messages
// (e.g. "clickhouse", "athena").
//
// SQL datasources backed by grafana/sqlds return exactly one frame per result
// set, so only the first frame is used.
func ParseResponse(respBody []byte, product string) (*QueryResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result, ok := raw.Results["A"]
	if !ok {
		return &QueryResponse{}, nil
	}
	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return nil, queryerror.New(product, "query", status, result.Error, result.ErrorSource)
	}

	resp := &QueryResponse{}
	if len(result.Frames) == 0 {
		return resp, nil
	}
	frame := result.Frames[0]

	for _, f := range frame.Schema.Fields {
		resp.Columns = append(resp.Columns, Column{Name: f.Name, Type: f.Type})
	}

	if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
		return resp, nil
	}
	numRows := len(frame.Data.Values[0])
	for rowIdx := range numRows {
		row := make([]any, len(frame.Data.Values))
		for colIdx := range frame.Data.Values {
			if rowIdx < len(frame.Data.Values[colIdx]) {
				row[colIdx] = frame.Data.Values[colIdx][rowIdx]
			}
		}
		resp.Rows = append(resp.Rows, row)
	}
	return resp, nil
}
