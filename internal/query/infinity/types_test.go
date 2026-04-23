package infinity_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertGrafanaResponse(t *testing.T) {
	tests := []struct {
		name        string
		input       *infinity.GrafanaQueryResponse
		wantColumns []infinity.Column
		wantRows    [][]any
	}{
		{
			name: "empty response with no result key A",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "result with no frames",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{},
					},
				},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "single frame with 2 columns and 3 rows",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "name", Type: "string"},
										{Name: "value", Type: "number"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{"Alice", "Bob", "Charlie"},
										{float64(10), float64(20), float64(30)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []infinity.Column{
				{Name: "name", Type: "string"},
				{Name: "value", Type: "number"},
			},
			wantRows: [][]any{
				{"Alice", float64(10)},
				{"Bob", float64(20)},
				{"Charlie", float64(30)},
			},
		},
		{
			name: "multi-column frame with 4 columns",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "id", Type: "number"},
										{Name: "host", Type: "string"},
										{Name: "status", Type: "number"},
										{Name: "message", Type: "string"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{float64(1), float64(2)},
										{"host-a", "host-b"},
										{float64(200), float64(500)},
										{"ok", "error"},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []infinity.Column{
				{Name: "id", Type: "number"},
				{Name: "host", Type: "string"},
				{Name: "status", Type: "number"},
				{Name: "message", Type: "string"},
			},
			wantRows: [][]any{
				{float64(1), "host-a", float64(200), "ok"},
				{float64(2), "host-b", float64(500), "error"},
			},
		},
		{
			name: "frame where data has fewer values than fields does not panic",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "a", Type: "string"},
										{Name: "b", Type: "string"},
										{Name: "c", Type: "string"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{"x"},
									},
								},
							},
						},
					},
				},
			},
			// Should handle gracefully: we only have 1 column of data for 3 fields.
			// The function should not panic. The exact output depends on implementation,
			// but we verify it does not crash and returns a valid structure.
			wantColumns: []infinity.Column{
				{Name: "a", Type: "string"},
				{Name: "b", Type: "string"},
				{Name: "c", Type: "string"},
			},
			wantRows: [][]any{
				{"x", nil, nil},
			},
		},
		{
			name: "frame where values arrays are empty produces no rows",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "col1", Type: "string"},
										{Name: "col2", Type: "number"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{},
										{},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []infinity.Column{
				{Name: "col1", Type: "string"},
				{Name: "col2", Type: "number"},
			},
			wantRows: nil,
		},
		{
			name: "first frame of multiple frames is used",
			input: &infinity.GrafanaQueryResponse{
				Results: map[string]infinity.GrafanaResult{
					"A": {
						Frames: []infinity.DataFrame{
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "first", Type: "string"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{"from-first-frame"},
									},
								},
							},
							{
								Schema: infinity.DataFrameSchema{
									Fields: []infinity.Field{
										{Name: "second", Type: "string"},
									},
								},
								Data: infinity.DataFrameData{
									Values: [][]any{
										{"from-second-frame"},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []infinity.Column{
				{Name: "first", Type: "string"},
			},
			wantRows: [][]any{
				{"from-first-frame"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := infinity.ConvertGrafanaResponse(tt.input)
			require.NotNil(t, got, "convertGrafanaResponse must never return nil")

			if tt.wantColumns == nil {
				assert.Empty(t, got.Columns)
			} else {
				assert.Equal(t, tt.wantColumns, got.Columns)
			}

			if tt.wantRows == nil {
				assert.Empty(t, got.Rows)
			} else {
				require.Len(t, got.Rows, len(tt.wantRows))
				for i, wantRow := range tt.wantRows {
					assert.Equal(t, wantRow, got.Rows[i], "row %d mismatch", i)
				}
			}
		})
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{
			name: "string passthrough",
			in:   "hello",
			want: "hello",
		},
		{
			name: "float64 formatted",
			in:   float64(3.14),
			want: "3.14",
		},
		{
			name: "float64 integer value",
			in:   float64(42),
			want: "42",
		},
		{
			name: "float64 large number",
			in:   float64(1000000),
			want: "1000000",
		},
		{
			name: "bool true",
			in:   true,
			want: "true",
		},
		{
			name: "bool false",
			in:   false,
			want: "false",
		},
		{
			name: "nil returns empty string",
			in:   nil,
			want: "",
		},
		{
			name: "unknown type uses fmt.Sprintf fallback",
			in:   []int{1, 2, 3},
			want: fmt.Sprintf("%v", []int{1, 2, 3}),
		},
		{
			name: "int uses fmt.Sprintf fallback",
			in:   42,
			want: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := infinity.ToString(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}
