package dataframe_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertResponse_EmptyResults(t *testing.T) {
	resp := &dataframe.Response{Results: map[string]dataframe.Result{}}
	out := dataframe.ConvertResponse(resp, "A")
	assert.Empty(t, out.Frames)
}

func TestConvertResponse_NoRefIdA(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"B": {Frames: []dataframe.Frame{}},
		},
	}
	out := dataframe.ConvertResponse(resp, "A")
	assert.Empty(t, out.Frames)
}

func TestConvertResponse_SingleFrame(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{
								{Name: "host", Type: "string"},
								{Name: "status", Type: "number"},
							},
						},
						Data: dataframe.Data{
							Values: [][]any{
								{"server-1", "server-2"},
								{float64(200), float64(500)},
							},
						},
					},
				},
			},
		},
	}

	out := dataframe.ConvertResponse(resp, "A")

	require.Len(t, out.Frames, 1)
	frame := out.Frames[0]

	require.Len(t, frame.Columns, 2)
	assert.Equal(t, "host", frame.Columns[0].Name)
	assert.Equal(t, "string", frame.Columns[0].Type)
	assert.Equal(t, "status", frame.Columns[1].Name)
	assert.Equal(t, "number", frame.Columns[1].Type)

	require.Len(t, frame.Rows, 2)
	assert.Equal(t, []any{"server-1", float64(200)}, frame.Rows[0])
	assert.Equal(t, []any{"server-2", float64(500)}, frame.Rows[1])
}

func TestConvertResponse_MultiFrame(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Name: "users",
							Fields: []dataframe.Field{
								{Name: "name", Type: "string"},
							},
						},
						Data: dataframe.Data{
							Values: [][]any{{"alice", "bob"}},
						},
					},
					{
						Schema: dataframe.Schema{
							Name: "counts",
							Fields: []dataframe.Field{
								{Name: "metric", Type: "string"},
								{Name: "value", Type: "number"},
							},
						},
						Data: dataframe.Data{
							Values: [][]any{
								{"requests", "errors"},
								{float64(100), float64(5)},
							},
						},
					},
				},
			},
		},
	}

	out := dataframe.ConvertResponse(resp, "A")

	require.Len(t, out.Frames, 2)

	// First frame.
	assert.Equal(t, "users", out.Frames[0].Name)
	require.Len(t, out.Frames[0].Columns, 1)
	assert.Equal(t, "name", out.Frames[0].Columns[0].Name)
	require.Len(t, out.Frames[0].Rows, 2)
	assert.Equal(t, []any{"alice"}, out.Frames[0].Rows[0])
	assert.Equal(t, []any{"bob"}, out.Frames[0].Rows[1])

	// Second frame.
	assert.Equal(t, "counts", out.Frames[1].Name)
	require.Len(t, out.Frames[1].Columns, 2)
	require.Len(t, out.Frames[1].Rows, 2)
	assert.Equal(t, []any{"requests", float64(100)}, out.Frames[1].Rows[0])
	assert.Equal(t, []any{"errors", float64(5)}, out.Frames[1].Rows[1])
}

func TestConvertResponse_CustomRefId(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"B": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "val", Type: "number"}},
						},
						Data: dataframe.Data{Values: [][]any{{float64(99)}}},
					},
				},
			},
		},
	}

	// Requesting refId "A" should return empty.
	outA := dataframe.ConvertResponse(resp, "A")
	assert.Empty(t, outA.Frames)

	// Requesting refId "B" should return the data.
	outB := dataframe.ConvertResponse(resp, "B")
	require.Len(t, outB.Frames, 1)
	require.Len(t, outB.Frames[0].Rows, 1)
	assert.Equal(t, []any{float64(99)}, outB.Frames[0].Rows[0])
}

func TestConvertResponse_EmptyData(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{
								{Name: "value", Type: "number"},
							},
						},
						Data: dataframe.Data{Values: [][]any{}},
					},
				},
			},
		},
	}

	out := dataframe.ConvertResponse(resp, "A")
	require.Len(t, out.Frames, 1)
	require.Len(t, out.Frames[0].Columns, 1)
	assert.Equal(t, "value", out.Frames[0].Columns[0].Name)
	assert.Empty(t, out.Frames[0].Rows)
}

func TestConvertResponse_SkipsEmptySchemaFrames(t *testing.T) {
	resp := &dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{Schema: dataframe.Schema{Fields: []dataframe.Field{}}},
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{{Name: "x", Type: "number"}},
						},
						Data: dataframe.Data{Values: [][]any{{float64(1)}}},
					},
				},
			},
		},
	}

	out := dataframe.ConvertResponse(resp, "A")
	require.Len(t, out.Frames, 1)
	assert.Equal(t, "x", out.Frames[0].Columns[0].Name)
}

func TestFormatTable_NoData(t *testing.T) {
	resp := &dataframe.RawQueryResponse{Frames: []dataframe.RawFrame{}}
	var buf bytes.Buffer
	err := dataframe.FormatTable(&buf, resp)
	require.NoError(t, err)
	assert.Equal(t, "No data\n", buf.String())
}

func TestFormatTable_SingleFrame(t *testing.T) {
	resp := &dataframe.RawQueryResponse{
		Frames: []dataframe.RawFrame{
			{
				Columns: []dataframe.RawColumn{
					{Name: "name", Type: "string"},
					{Name: "value", Type: "number"},
				},
				Rows: [][]any{
					{"alice", float64(42)},
					{"bob", nil},
				},
			},
		},
	}

	var buf bytes.Buffer
	err := dataframe.FormatTable(&buf, resp)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "VALUE")
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "42")
	assert.Contains(t, output, "bob")
	// Single frame: no frame header.
	assert.NotContains(t, output, "──")
}

func TestFormatTable_MultiFrame(t *testing.T) {
	resp := &dataframe.RawQueryResponse{
		Frames: []dataframe.RawFrame{
			{
				Name: "users",
				Columns: []dataframe.RawColumn{
					{Name: "name", Type: "string"},
				},
				Rows: [][]any{{"alice"}},
			},
			{
				Name: "counts",
				Columns: []dataframe.RawColumn{
					{Name: "metric", Type: "string"},
					{Name: "value", Type: "number"},
				},
				Rows: [][]any{{"requests", float64(100)}},
			},
		},
	}

	var buf bytes.Buffer
	err := dataframe.FormatTable(&buf, resp)
	require.NoError(t, err)

	output := buf.String()
	// Both frame headers present.
	assert.Contains(t, output, "── users (1 rows) ──")
	assert.Contains(t, output, "── counts (1 rows) ──")
	// Data from both frames.
	assert.Contains(t, output, "alice")
	assert.Contains(t, output, "requests")
	assert.Contains(t, output, "100")
}

func TestFormatTable_MultiFrameUnnamed(t *testing.T) {
	resp := &dataframe.RawQueryResponse{
		Frames: []dataframe.RawFrame{
			{Columns: []dataframe.RawColumn{{Name: "a"}}, Rows: [][]any{{"x"}}},
			{Columns: []dataframe.RawColumn{{Name: "b"}}, Rows: [][]any{{"y"}}},
		},
	}

	var buf bytes.Buffer
	err := dataframe.FormatTable(&buf, resp)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "── Frame 1 (1 rows) ──")
	assert.Contains(t, output, "── Frame 2 (1 rows) ──")
}
