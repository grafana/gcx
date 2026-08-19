package influxdb_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatQueryTable(t *testing.T) {
	tests := []struct {
		name     string
		resp     *influxdb.QueryResponse
		contains []string
		noData   bool
	}{
		{
			name: "with data rows",
			resp: &influxdb.QueryResponse{
				Columns: []string{"Time", "cpu", "host"},
				Rows: [][]any{
					{float64(1000), float64(55.2), "server-a"},
					{float64(2000), float64(63.8), "server-b"},
					{float64(3000), float64(71.4), "server-c"},
				},
			},
			contains: []string{"Time", "cpu", "host", "server-a", "server-b", "server-c"},
		},
		{
			name: "empty rows prints no data message",
			resp: &influxdb.QueryResponse{
				Columns: []string{"Time", "Value"},
				Rows:    nil,
			},
			noData: true,
		},
		{
			name:   "nil columns and nil rows prints no data message",
			resp:   &influxdb.QueryResponse{},
			noData: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := influxdb.FormatQueryTable(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			assert.NotEmpty(t, output)

			if tt.noData {
				assert.Contains(t, strings.ToLower(output), "no data")
			} else {
				for _, s := range tt.contains {
					assert.Contains(t, output, s)
				}
			}
		})
	}
}

func TestFormatArrow(t *testing.T) {
	resp := &influxdb.QueryResponse{
		Columns:     []string{"time", "cpu", "host"},
		TimeColumns: map[int]bool{0: true},
		Rows: [][]any{
			{float64(1700000000000), float64(55.2), "server-a"},
			{float64(1700000060000), float64(63.8), "server-b"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, influxdb.FormatArrow(&buf, resp))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"time", "cpu", "host"}, headers)
	require.Len(t, rows, 2)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), rows[0][0])
	assert.InEpsilon(t, 55.2, rows[0][1], 0.0001)
	assert.Equal(t, "server-a", rows[0][2])
}

func TestFormatArrow_NonNumericColumnFallsBackToUtf8(t *testing.T) {
	resp := &influxdb.QueryResponse{
		Columns: []string{"host", "tags"},
		Rows: [][]any{
			{"server-a", map[string]any{"az": "us-east-1a"}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, influxdb.FormatArrow(&buf, resp))

	_, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "server-a", rows[0][0])
	assert.Contains(t, rows[0][1], "us-east-1a")
}

func TestFormatArrow_NoData(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, influxdb.FormatArrow(&buf, &influxdb.QueryResponse{}))
	assert.Empty(t, buf.String())
}

func TestFormatMeasurementsTable(t *testing.T) {
	tests := []struct {
		name     string
		resp     *influxdb.MeasurementsResponse
		contains []string
		empty    bool
	}{
		{
			name: "with measurements",
			resp: &influxdb.MeasurementsResponse{
				Measurements: []string{"cpu", "disk", "mem", "net"},
			},
			contains: []string{"cpu", "disk", "mem", "net"},
		},
		{
			name:  "empty measurements list",
			resp:  &influxdb.MeasurementsResponse{},
			empty: true,
		},
		{
			name: "nil measurements list",
			resp: &influxdb.MeasurementsResponse{
				Measurements: nil,
			},
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := influxdb.FormatMeasurementsTable(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			assert.NotEmpty(t, output)

			if tt.empty {
				lower := strings.ToLower(output)
				assert.Contains(t, lower, "no measurements found")
			} else {
				for _, s := range tt.contains {
					assert.Contains(t, output, s)
				}
			}
		})
	}
}

func TestFormatFieldKeysTable(t *testing.T) {
	tests := []struct {
		name     string
		resp     *influxdb.FieldKeysResponse
		contains []string
		empty    bool
	}{
		{
			name: "with field keys",
			resp: &influxdb.FieldKeysResponse{
				Fields: []influxdb.FieldKey{
					{FieldKey: "usage_idle", FieldType: "float"},
					{FieldKey: "usage_system", FieldType: "float"},
					{FieldKey: "host", FieldType: "string"},
				},
			},
			contains: []string{"usage_idle", "float", "usage_system", "host", "string"},
		},
		{
			name:  "empty field keys list",
			resp:  &influxdb.FieldKeysResponse{},
			empty: true,
		},
		{
			name: "nil field keys list",
			resp: &influxdb.FieldKeysResponse{
				Fields: nil,
			},
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := influxdb.FormatFieldKeysTable(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			assert.NotEmpty(t, output)

			if tt.empty {
				lower := strings.ToLower(output)
				assert.Contains(t, lower, "no field keys found")
			} else {
				for _, s := range tt.contains {
					assert.Contains(t, output, s)
				}
			}
		})
	}
}

func TestFormatQueryJSON(t *testing.T) {
	// jsonResult is used to unmarshal the output of FormatQueryJSON via
	// JSON round-trip so the test does not depend on the exported type name.
	type jsonResult struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}

	roundTrip := func(t *testing.T, result *influxdb.JSONQueryResponse) jsonResult {
		t.Helper()
		data, err := json.Marshal(result)
		require.NoError(t, err)
		var out jsonResult
		require.NoError(t, json.Unmarshal(data, &out))
		return out
	}

	tests := []struct {
		name   string
		resp   *influxdb.QueryResponse
		assert func(t *testing.T, result jsonResult)
	}{
		{
			name: "float64 time column converted to RFC3339",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "value"},
				Rows:        [][]any{{float64(1719849600000), float64(42.5)}},
				TimeColumns: map[int]bool{0: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				require.Len(t, result.Rows[0], 2)
				assert.Equal(t, "2024-07-01T16:00:00Z", result.Rows[0][0])
			},
		},
		{
			name: "int64 time column converted to RFC3339",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "value"},
				Rows:        [][]any{{int64(1719849600000), float64(42.5)}},
				TimeColumns: map[int]bool{0: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				assert.Equal(t, "2024-07-01T16:00:00Z", result.Rows[0][0])
			},
		},
		{
			name: "non-time columns left unchanged",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "host", "cpu"},
				Rows:        [][]any{{float64(1000), "server-a", float64(55.2)}},
				TimeColumns: map[int]bool{0: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				row := result.Rows[0]
				assert.Equal(t, "server-a", row[1])
				assert.InDelta(t, float64(55.2), row[2], 0.001)
			},
		},
		{
			name: "empty rows returns empty slice",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "value"},
				Rows:        nil,
				TimeColumns: map[int]bool{0: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				assert.Empty(t, result.Rows)
				assert.Equal(t, []string{"time", "value"}, result.Columns)
			},
		},
		{
			name: "nil TimeColumns is safe",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "value"},
				Rows:        [][]any{{float64(1000), float64(42.5)}},
				TimeColumns: nil,
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				// Without TimeColumns, values are passed through as-is.
				assert.InDelta(t, float64(1000), result.Rows[0][0], 0.001)
				assert.InDelta(t, float64(42.5), result.Rows[0][1], 0.001)
			},
		},
		{
			name: "multiple time columns in one row",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"start", "end", "value"},
				Rows:        [][]any{{float64(1719849600000), float64(1719936000000), float64(99)}},
				TimeColumns: map[int]bool{0: true, 1: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				row := result.Rows[0]
				assert.Equal(t, "2024-07-01T16:00:00Z", row[0])
				assert.Equal(t, "2024-07-02T16:00:00Z", row[1])
				assert.InDelta(t, float64(99), row[2], 0.001)
			},
		},
		{
			name: "known timestamp value",
			resp: &influxdb.QueryResponse{
				Columns:     []string{"time", "value"},
				Rows:        [][]any{{float64(1719849600000), float64(1)}},
				TimeColumns: map[int]bool{0: true},
			},
			assert: func(t *testing.T, result jsonResult) {
				t.Helper()
				require.Len(t, result.Rows, 1)
				assert.Equal(t, "2024-07-01T16:00:00Z", result.Rows[0][0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := influxdb.FormatQueryJSON(tt.resp)
			require.NotNil(t, result)
			tt.assert(t, roundTrip(t, result))
		})
	}

	t.Run("original response not mutated", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns:     []string{"time", "value"},
			Rows:        [][]any{{float64(1719849600000), float64(42.5)}},
			TimeColumns: map[int]bool{0: true},
		}

		// Capture original values before calling FormatQueryJSON.
		origTimeVal := resp.Rows[0][0]
		origDataVal := resp.Rows[0][1]

		_ = influxdb.FormatQueryJSON(resp)

		// The original response must not have been modified.
		assert.Equal(t, origTimeVal, resp.Rows[0][0], "time value in original response was mutated")
		assert.Equal(t, origDataVal, resp.Rows[0][1], "data value in original response was mutated")
	})
}
