package influxdb_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertGrafanaResponse(t *testing.T) {
	tests := []struct {
		name        string
		input       *influxdb.GrafanaQueryResponse
		wantColumns []string
		wantRows    [][]any
	}{
		{
			name: "empty results map",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "result A with no frames",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {Frames: []influxdb.DataFrame{}},
				},
			},
			wantColumns: nil,
			wantRows:    nil,
		},
		{
			name: "result A with error still returns data",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Error:       "partial error",
						ErrorSource: "downstream",
						Status:      400,
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000)},
										{float64(10.5), float64(20.3)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "Value"},
			wantRows: [][]any{
				{float64(1000), float64(10.5)},
				{float64(2000), float64(20.3)},
			},
		},
		{
			name: "single frame with 2 columns and 3 rows",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000), float64(3000)},
										{float64(10.5), float64(20.3), float64(30.1)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "Value"},
			wantRows: [][]any{
				{float64(1000), float64(10.5)},
				{float64(2000), float64(20.3)},
				{float64(3000), float64(30.1)},
			},
		},
		{
			name: "single frame with 3 columns and 2 rows",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "cpu", Type: "number"},
										{Name: "host", Type: "string"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000), float64(2000)},
										{float64(55.2), float64(63.8)},
										{"server-a", "server-b"},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "cpu", "host"},
			wantRows: [][]any{
				{float64(1000), float64(55.2), "server-a"},
				{float64(2000), float64(63.8), "server-b"},
			},
		},
		{
			name: "result A frame with empty values",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "Value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
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
			wantColumns: []string{"Time", "Value"},
			wantRows:    nil,
		},
		{
			name: "multiple frames rows are combined using first frame schema",
			input: &influxdb.GrafanaQueryResponse{
				Results: map[string]influxdb.GrafanaResult{
					"A": {
						Frames: []influxdb.DataFrame{
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(1000)},
										{float64(42.0)},
									},
								},
							},
							{
								Schema: influxdb.DataFrameSchema{
									Fields: []influxdb.Field{
										{Name: "Time", Type: "time"},
										{Name: "_value", Type: "number"},
									},
								},
								Data: influxdb.DataFrameData{
									Values: [][]any{
										{float64(2000)},
										{float64(99.0)},
									},
								},
							},
						},
					},
				},
			},
			wantColumns: []string{"Time", "_value"},
			wantRows: [][]any{
				{float64(1000), float64(42.0)},
				{float64(2000), float64(99.0)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := influxdb.ConvertGrafanaResponse(tt.input)
			require.NotNil(t, got)

			assert.Equal(t, tt.wantColumns, got.Columns)

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
