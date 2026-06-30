package sql_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseResponse_SingleFrame(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{
								{Name: "id", Type: "number"},
								{Name: "name", Type: "string"},
							},
						},
						Data: dataframe.Data{
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

	resp, err := sql.ParseResponse(body, "clickhouse")
	require.NoError(t, err)

	assert.Equal(t, []sql.Column{{Name: "id", Type: "number"}, {Name: "name", Type: "string"}}, resp.Columns)
	require.Len(t, resp.Rows, 2)
	assert.InDelta(t, 1, resp.Rows[0][0], 0)
	assert.Equal(t, "alice", resp.Rows[0][1])
	assert.InDelta(t, 2, resp.Rows[1][0], 0)
	assert.Equal(t, "bob", resp.Rows[1][1])
}

func TestParseResponse_UsesOnlyFirstFrame(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "v", Type: "number"}},
						},
						Data: dataframe.Data{
							Values: [][]any{{float64(1), float64(2)}},
						},
					},
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "v", Type: "number"}},
						},
						Data: dataframe.Data{
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

	resp, err := sql.ParseResponse(body, "clickhouse")
	require.NoError(t, err)

	require.Len(t, resp.Rows, 2)
	assert.InDelta(t, 1, resp.Rows[0][0], 0)
	assert.InDelta(t, 2, resp.Rows[1][0], 0)
}

func TestParseResponse_MismatchedFrameSkipped(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "a", Type: "string"}},
						},
						Data: dataframe.Data{
							Values: [][]any{{"x"}},
						},
					},
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "b", Type: "number"}},
						},
						Data: dataframe.Data{
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

	resp, err := sql.ParseResponse(body, "clickhouse")
	require.NoError(t, err)

	assert.Equal(t, []sql.Column{{Name: "a", Type: "string"}}, resp.Columns)
	require.Len(t, resp.Rows, 1)
	assert.Equal(t, "x", resp.Rows[0][0])
}

func TestParseResponse_ErrorInResult(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Error:       "Code: 62. DB::Exception: Syntax error",
				ErrorSource: "downstream",
				Status:      400,
			},
		},
	}

	body, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = sql.ParseResponse(body, "clickhouse")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Syntax error")
}

func TestParseResponse_EmptyResult(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "x", Type: "string"}},
						},
						Data: dataframe.Data{
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

	resp, err := sql.ParseResponse(body, "clickhouse")
	require.NoError(t, err)

	assert.Equal(t, []sql.Column{{Name: "x", Type: "string"}}, resp.Columns)
	assert.Empty(t, resp.Rows)
}

func TestParseResponse_MissingRefID(t *testing.T) {
	raw := dataframe.Response{
		Results: map[string]dataframe.Result{
			"B": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "v", Type: "number"}},
						},
						Data: dataframe.Data{
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

	resp, err := sql.ParseResponse(body, "clickhouse")
	require.NoError(t, err)

	assert.Empty(t, resp.Columns)
	assert.Empty(t, resp.Rows)
}
