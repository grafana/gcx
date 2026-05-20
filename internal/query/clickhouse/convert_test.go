package clickhouse_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseResponse_SingleFrame(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"A": {
				Frames: []clickhouse.DataFrame{
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{
								{Name: "id", Type: "number"},
								{Name: "name", Type: "string"},
							},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{
								{float64(1), float64(2)},
								{"alice", "bob"},
							},
						},
					},
				},
				Status: 200,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	resp, err := clickhouse.ParseResponse(body)
	require.NoError(t, err)

	assert.Equal(t, []clickhouse.Column{{Name: "id", Type: "number"}, {Name: "name", Type: "string"}}, resp.Columns)
	require.Len(t, resp.Rows, 2)
	assert.InDelta(t, 1, resp.Rows[0][0], 0)
	assert.Equal(t, "alice", resp.Rows[0][1])
	assert.InDelta(t, 2, resp.Rows[1][0], 0)
	assert.Equal(t, "bob", resp.Rows[1][1])
}

func TestParseResponse_MultipleFramesMerged(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"A": {
				Frames: []clickhouse.DataFrame{
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "v", Type: "number"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{float64(1), float64(2)}},
						},
					},
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "v", Type: "number"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{float64(3)}},
						},
					},
				},
				Status: 200,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	resp, err := clickhouse.ParseResponse(body)
	require.NoError(t, err)

	require.Len(t, resp.Rows, 3)
	assert.InDelta(t, 3, resp.Rows[2][0], 0)
}

func TestParseResponse_MismatchedFrameSkipped(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"A": {
				Frames: []clickhouse.DataFrame{
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "a", Type: "string"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{"x"}},
						},
					},
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "b", Type: "number"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{float64(99)}},
						},
					},
				},
				Status: 200,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	resp, err := clickhouse.ParseResponse(body)
	require.NoError(t, err)

	assert.Equal(t, []clickhouse.Column{{Name: "a", Type: "string"}}, resp.Columns)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, "x", resp.Rows[0][0])
}

func TestParseResponse_ErrorInResult(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"A": {
				Error:       "Code: 62. DB::Exception: Syntax error",
				ErrorSource: "downstream",
				Status:      400,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = clickhouse.ParseResponse(body)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Syntax error")
}

func TestParseResponse_EmptyResult(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"A": {
				Frames: []clickhouse.DataFrame{
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "x", Type: "string"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{}},
						},
					},
				},
				Status: 200,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	resp, err := clickhouse.ParseResponse(body)
	require.NoError(t, err)

	assert.Equal(t, []clickhouse.Column{{Name: "x", Type: "string"}}, resp.Columns)
	assert.Empty(t, resp.Rows)
}

func TestParseResponse_MissingRefID(t *testing.T) {
	raw := clickhouse.GrafanaQueryResponse{
		Results: map[string]clickhouse.GrafanaResult{
			"B": {
				Frames: []clickhouse.DataFrame{
					{
						Schema: clickhouse.DataFrameSchema{
							Fields: []clickhouse.DataFrameField{{Name: "v", Type: "number"}},
						},
						Data: clickhouse.DataFrameData{
							Values: [][]any{{float64(1)}},
						},
					},
				},
				Status: 200,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	resp, err := clickhouse.ParseResponse(body)
	require.NoError(t, err)

	assert.Empty(t, resp.Columns)
	assert.Empty(t, resp.Rows)
}
