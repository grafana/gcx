package influxdb_test

import (
	"bytes"
	"strings"
	"testing"

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
